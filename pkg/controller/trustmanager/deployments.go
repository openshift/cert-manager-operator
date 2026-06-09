package trustmanager

import (
	"fmt"
	"os"
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

const (
	defaultCAPackageVolumeMountPath = "/var/run/configmaps/default-package"
	defaultCAPackageConfigMapName   = "trust-manager-default-package"
	defaultCAPackageKeyName         = "ca-certificates.crt"
)

func (r *Reconciler) createOrApplyDeployments(tm *v1alpha1.TrustManager, trustNamespace string, resourceLabels map[string]string, trustManagerCreateRecon bool) error {
	desired, err := r.getDeploymentObject(tm, trustNamespace, resourceLabels)
	if err != nil {
		return fmt.Errorf("failed to generate deployment resource for creation in %s: %w", trustNamespace, err)
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

func (r *Reconciler) getDeploymentObject(tm *v1alpha1.TrustManager, trustNamespace string, resourceLabels map[string]string) (*appsv1.Deployment, error) {
	deployment := decodeDeploymentObjBytes(assets.MustAsset(deploymentAssetName))

	updateNamespace(deployment, trustNamespace)
	updateResourceLabels(deployment, resourceLabels)
	updatePodTemplateLabels(deployment, resourceLabels)

	updateArgList(deployment, tm)

	if err := updateResourceRequirement(deployment, tm); err != nil {
		return nil, fmt.Errorf("failed to update resource requirements: %w", err)
	}
	if err := updatePodTolerations(deployment, tm); err != nil {
		return nil, fmt.Errorf("failed to update pod tolerations: %w", err)
	}
	if err := updateNodeSelector(deployment, tm); err != nil {
		return nil, fmt.Errorf("failed to update node selector: %w", err)
	}
	if tm.Spec.TrustManagerConfig.Affinity != nil {
		deployment.Spec.Template.Spec.Affinity = tm.Spec.TrustManagerConfig.Affinity
	}
	if err := r.updateImage(deployment); err != nil {
		return nil, NewIrrecoverableError(err, "failed to update image for %s", tm.GetName())
	}

	// Handle defaultCAPackage volume mount if enabled
	if tm.Spec.TrustManagerConfig.DefaultCAPackage.Policy == v1alpha1.DefaultCAPackagePolicyEnabled {
		updateVolumeWithDefaultCAPackage(deployment)
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
	tmConfig := tm.Spec.TrustManagerConfig

	trustNamespace := tmConfig.TrustNamespace
	if trustNamespace == "" {
		trustNamespace = trustManagerDefaultNamespace
	}

	args := []string{
		fmt.Sprintf("--log-level=%d", tmConfig.LogLevel),
		fmt.Sprintf("--log-format=%s", tmConfig.LogFormat),
		fmt.Sprintf("--trust-namespace=%s", trustNamespace),
		"--metrics-port=9402",
		"--readiness-probe-port=6060",
		"--readiness-probe-path=/readyz",
		fmt.Sprintf("--filter-expired-certs=%t", tmConfig.FilterExpiredCertificates == v1alpha1.FilterExpiredCertificatesPolicyEnabled),
	}

	// Add secret targets configuration
	if tmConfig.SecretTargets.Policy == v1alpha1.SecretTargetsPolicyCustom {
		args = append(args, "--secret-targets-enabled=true")
	}

	// Add default CA package location if enabled
	if tmConfig.DefaultCAPackage.Policy == v1alpha1.DefaultCAPackagePolicyEnabled {
		args = append(args, fmt.Sprintf("--default-package-location=%s/%s", defaultCAPackageVolumeMountPath, defaultCAPackageKeyName))
	}

	for i, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name == trustManagerContainerName {
			deployment.Spec.Template.Spec.Containers[i].Args = args
		}
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

// updateVolumeWithDefaultCAPackage adds the default CA package ConfigMap volume and mount
// to the trust-manager deployment. The ConfigMap is injected with the OpenShift trusted CA
// bundle via the config.openshift.io/inject-trusted-cabundle annotation.
func updateVolumeWithDefaultCAPackage(deployment *appsv1.Deployment) {
	const (
		caVolumeName = "default-package"
	)
	var (
		defaultMode = int32(420)
	)

	desiredVolumeMount := corev1.VolumeMount{
		Name:      caVolumeName,
		MountPath: defaultCAPackageVolumeMountPath,
		ReadOnly:  true,
	}

	desiredVolume := corev1.Volume{
		Name: caVolumeName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: defaultCAPackageConfigMapName,
				},
				DefaultMode: &defaultMode,
			},
		},
	}

	// Update or append volume mount in the trust-manager container
	for i, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name == trustManagerContainerName {
			volumeMountExists := false
			for j, vm := range container.VolumeMounts {
				if vm.Name == caVolumeName {
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

	// Update or append volume in the deployment
	volumeExists := false
	for i, vol := range deployment.Spec.Template.Spec.Volumes {
		if vol.Name == caVolumeName {
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

// createOrApplyDefaultCAPackageConfigMap creates or updates the ConfigMap used for the default
// CA package with the OpenShift trusted CA bundle injection annotation.
func (r *Reconciler) createOrApplyDefaultCAPackageConfigMap(tm *v1alpha1.TrustManager, trustNamespace string, resourceLabels map[string]string) error {
	configmapKey := client.ObjectKey{
		Name:      defaultCAPackageConfigMapName,
		Namespace: trustNamespace,
	}

	desired := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configmapKey.Name,
			Namespace: configmapKey.Namespace,
			Labels:    resourceLabels,
			Annotations: map[string]string{
				// This annotation triggers the Cluster Network Operator (CNO)
				// to inject the trusted CA bundle into this ConfigMap.
				"config.openshift.io/inject-trusted-cabundle": "true",
			},
		},
	}

	fetched := &corev1.ConfigMap{}
	exist, err := r.Exists(r.ctx, configmapKey, fetched)
	if err != nil {
		return FromClientError(err, "failed to check if default CA package configmap exists")
	}

	if exist && hasObjectChanged(desired, fetched) {
		r.log.V(1).Info("default CA package configmap needs update", "name", configmapKey)
		if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to update %s configmap resource", configmapKey)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "configmap resource %s reconciled back to desired state", configmapKey)
	} else {
		r.log.V(4).Info("default CA package configmap already exists and is in expected state", "name", configmapKey)
	}

	if !exist {
		if err := r.Create(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to create %s configmap resource", configmapKey)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "configmap resource %s created", configmapKey)
	}

	return nil
}
