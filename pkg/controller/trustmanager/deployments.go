package trustmanager

import (
	"fmt"
	"os"
	"reflect"
	"unsafe"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1validation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/kubernetes/pkg/apis/core"
	corevalidation "k8s.io/kubernetes/pkg/apis/core/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func (r *Reconciler) createOrApplyDeployments(tm *v1alpha1.TrustManager, resourceLabels map[string]string, trustManagerCreateRecon bool) error {
	desired, err := r.getDeploymentObject(tm, resourceLabels)
	if err != nil {
		return fmt.Errorf("failed to generate deployment resource for creation: %w", err)
	}

	deploymentName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(4).Info("reconciling deployment resource", "name", deploymentName)
	fetched := &appsv1.Deployment{}
	exist, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), fetched)
	if err != nil {
		return FromClientError(err, "failed to check %s deployment resource already exists", deploymentName)
	}

	if exist && trustManagerCreateRecon {
		r.eventRecorder.Eventf(tm, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s deployment resource already exists, maybe from previous installation", deploymentName)
	}
	if exist && hasObjectChanged(desired, fetched) {
		r.log.V(1).Info("deployment has been modified, updating to desired state", "name", deploymentName)
		if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to update %s deployment resource", deploymentName)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "deployment resource %s reconciled back to desired state", deploymentName)
	} else {
		r.log.V(4).Info("deployment resource already exists and is in expected state", "name", deploymentName)
	}
	if !exist {
		if err := r.Create(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to create %s deployment resource", deploymentName)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "deployment resource %s created", deploymentName)
	}

	if err := r.updateImageInStatus(tm, desired); err != nil {
		return FromClientError(err, "failed to update %s trustmanager status with image info", tm.GetName())
	}
	return nil
}

func (r *Reconciler) getDeploymentObject(tm *v1alpha1.TrustManager, resourceLabels map[string]string) (*appsv1.Deployment, error) {
	deployment := decodeDeploymentObjBytes(assets.MustAsset(deploymentAssetName))

	trustNamespace := tm.Spec.TrustManagerConfig.TrustNamespace
	if trustNamespace == "" {
		trustNamespace = defaultTrustNamespace
	}

	updateNamespace(deployment, trustNamespace)
	updateResourceLabels(deployment, resourceLabels)
	updatePodTemplateLabels(deployment, resourceLabels)

	updateArgList(deployment, tm)

	if err := updateResourceRequirement(deployment, tm); err != nil {
		return nil, fmt.Errorf("failed to update resource requirements: %w", err)
	}
	if err := updateAffinityRules(deployment, tm); err != nil {
		return nil, fmt.Errorf("failed to update affinity rules: %w", err)
	}
	if err := updatePodTolerations(deployment, tm); err != nil {
		return nil, fmt.Errorf("failed to update pod tolerations: %w", err)
	}
	if err := updateNodeSelector(deployment, tm); err != nil {
		return nil, fmt.Errorf("failed to update node selector: %w", err)
	}
	if err := r.updateImage(deployment); err != nil {
		return nil, NewIrrecoverableError(err, "failed to update image for %s", tm.GetName())
	}

	// Add default CA package volume if enabled
	if tm.Spec.TrustManagerConfig.DefaultCAPackage.Policy == v1alpha1.DefaultCAPackagePolicyEnabled {
		updateDefaultCAPackageVolume(deployment)
	}

	return deployment, nil
}

func (r *Reconciler) updateImage(deployment *appsv1.Deployment) error {
	image := os.Getenv(trustManagerImageNameEnvVarName)
	if image == "" {
		return fmt.Errorf("%s environment variable with trust-manager image not set", trustManagerImageNameEnvVarName)
	}
	for i, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name == trustManagerContainerName {
			deployment.Spec.Template.Spec.Containers[i].Image = image
		}
	}
	return nil
}

func (r *Reconciler) updateImageInStatus(tm *v1alpha1.TrustManager, deployment *appsv1.Deployment) error {
	for _, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name == trustManagerContainerName {
			if tm.Status.TrustManagerImage == container.Image {
				return nil
			}
			tm.Status.TrustManagerImage = container.Image
		}
	}
	return r.updateStatus(r.ctx, tm)
}

func updatePodTemplateLabels(deployment *appsv1.Deployment, resourceLabels map[string]string) {
	deployment.Spec.Template.Labels = resourceLabels
}

func updateArgList(deployment *appsv1.Deployment, tm *v1alpha1.TrustManager) {
	config := tm.Spec.TrustManagerConfig
	trustNamespace := config.TrustNamespace
	if trustNamespace == "" {
		trustNamespace = defaultTrustNamespace
	}

	args := []string{
		fmt.Sprintf("--log-level=%d", config.LogLevel),
		fmt.Sprintf("--log-format=%s", config.LogFormat),
		"--metrics-port=9402",
		"--readiness-probe-port=6060",
		"--readiness-probe-path=/readyz",
		fmt.Sprintf("--trust-namespace=%s", trustNamespace),
		"--webhook-host=0.0.0.0",
		"--webhook-port=6443",
		"--webhook-certificate-dir=/tls",
	}

	// Add filter-expired-certificates flag if enabled
	if config.FilterExpiredCertificates == v1alpha1.FilterExpiredCertificatesPolicyEnabled {
		args = append(args, "--filter-expired-certificates=true")
	}

	// Add secret targets args if enabled
	if config.SecretTargets.Policy == v1alpha1.SecretTargetsPolicyCustom {
		args = append(args, "--secret-targets-enabled=true")
	}

	// Add default package location if enabled
	if config.DefaultCAPackage.Policy == v1alpha1.DefaultCAPackagePolicyEnabled {
		args = append(args, fmt.Sprintf("--default-package-location=/var/run/configmaps/default-ca-package/%s", defaultCAPackageKey))
	}

	for i, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name == trustManagerContainerName {
			deployment.Spec.Template.Spec.Containers[i].Args = args
		}
	}
}

func updateDefaultCAPackageVolume(deployment *appsv1.Deployment) {
	const volumeName = "default-ca-package"
	var defaultMode = int32(420)

	desiredVolumeMount := corev1.VolumeMount{
		Name:      volumeName,
		MountPath: "/var/run/configmaps/default-ca-package",
		ReadOnly:  true,
	}

	desiredVolume := corev1.Volume{
		Name: volumeName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: defaultCAPackageConfigMapName,
				},
				DefaultMode: &defaultMode,
			},
		},
	}

	// Add volume mount to the trust-manager container
	for i, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name == trustManagerContainerName {
			volumeMountExists := false
			for j, vm := range container.VolumeMounts {
				if vm.Name == volumeName {
					deployment.Spec.Template.Spec.Containers[i].VolumeMounts[j] = desiredVolumeMount
					volumeMountExists = true
					break
				}
			}
			if !volumeMountExists {
				deployment.Spec.Template.Spec.Containers[i].VolumeMounts = append(
					deployment.Spec.Template.Spec.Containers[i].VolumeMounts,
					desiredVolumeMount,
				)
			}
			break
		}
	}

	// Add volume to the deployment
	volumeExists := false
	for i, vol := range deployment.Spec.Template.Spec.Volumes {
		if vol.Name == volumeName {
			deployment.Spec.Template.Spec.Volumes[i] = desiredVolume
			volumeExists = true
			break
		}
	}
	if !volumeExists {
		deployment.Spec.Template.Spec.Volumes = append(
			deployment.Spec.Template.Spec.Volumes,
			desiredVolume,
		)
	}
}

func updateResourceRequirement(deployment *appsv1.Deployment, tm *v1alpha1.TrustManager) error {
	if reflect.ValueOf(tm.Spec.TrustManagerConfig.Resources).IsZero() {
		return nil
	}
	if err := validateResourceRequirements(tm.Spec.TrustManagerConfig.Resources,
		field.NewPath("spec", "trustManagerConfig")); err != nil {
		return err
	}
	for i := range deployment.Spec.Template.Spec.Containers {
		deployment.Spec.Template.Spec.Containers[i].Resources = tm.Spec.TrustManagerConfig.Resources
	}
	return nil
}

func updateAffinityRules(deployment *appsv1.Deployment, tm *v1alpha1.TrustManager) error {
	if tm.Spec.TrustManagerConfig.Affinity == nil {
		return nil
	}
	if err := validateAffinityRules(tm.Spec.TrustManagerConfig.Affinity,
		field.NewPath("spec", "trustManagerConfig")); err != nil {
		return err
	}
	deployment.Spec.Template.Spec.Affinity = tm.Spec.TrustManagerConfig.Affinity
	return nil
}

func updatePodTolerations(deployment *appsv1.Deployment, tm *v1alpha1.TrustManager) error {
	if tm.Spec.TrustManagerConfig.Tolerations == nil {
		return nil
	}
	if err := validateTolerationsConfig(tm.Spec.TrustManagerConfig.Tolerations,
		field.NewPath("spec", "trustManagerConfig")); err != nil {
		return err
	}
	deployment.Spec.Template.Spec.Tolerations = tm.Spec.TrustManagerConfig.Tolerations
	return nil
}

func updateNodeSelector(deployment *appsv1.Deployment, tm *v1alpha1.TrustManager) error {
	if tm.Spec.TrustManagerConfig.NodeSelector == nil {
		return nil
	}
	if err := validateNodeSelectorConfig(tm.Spec.TrustManagerConfig.NodeSelector,
		field.NewPath("spec", "trustManagerConfig")); err != nil {
		return err
	}
	deployment.Spec.Template.Spec.NodeSelector = tm.Spec.TrustManagerConfig.NodeSelector
	return nil
}

// validateNodeSelectorConfig validates the NodeSelector configuration.
func validateNodeSelectorConfig(nodeSelector map[string]string, fldPath *field.Path) error {
	return metav1validation.ValidateLabels(nodeSelector, fldPath.Child("nodeSelector")).ToAggregate()
}

func validateTolerationsConfig(tolerations []corev1.Toleration, fldPath *field.Path) error {
	// convert corev1.Tolerations to core.Tolerations, required for validation.
	convTolerations := *(*[]core.Toleration)(unsafe.Pointer(&tolerations))
	return corevalidation.ValidateTolerations(convTolerations, fldPath.Child("tolerations")).ToAggregate()
}

func validateResourceRequirements(requirements corev1.ResourceRequirements, fldPath *field.Path) error {
	// convert corev1.ResourceRequirements to core.ResourceRequirements, required for validation.
	convRequirements := *(*core.ResourceRequirements)(unsafe.Pointer(&requirements))
	return corevalidation.ValidateContainerResourceRequirements(&convRequirements, nil, fldPath.Child("resources"), corevalidation.PodValidationOptions{}).ToAggregate()
}

func validateAffinityRules(affinity *corev1.Affinity, fldPath *field.Path) error {
	// convert corev1.Affinity to core.Affinity, required for validation.
	convAffinity := (*core.Affinity)(unsafe.Pointer(affinity))
	return validateAffinity(convAffinity, corevalidation.PodValidationOptions{}, fldPath.Child("affinity")).ToAggregate()
}
