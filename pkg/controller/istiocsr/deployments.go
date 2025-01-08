package istiocsr

import (
	"fmt"
	"os"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

const (
	caVolumeMountPath = "/var/run/configmaps/istio-csr"
)

var invalidIssuerRefConfigError = fmt.Errorf("invalid issuerRef config")

func (r *Reconciler) createOrApplyDeployments(istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string, istioCSRCreateRecon bool) error {
	desired, err := r.getDeploymentObject(istiocsr, resourceLabels)
	if err != nil {
		return fmt.Errorf("failed to generate deployment resource for creation in %s: %w", istiocsr.GetNamespace(), err)
	}

	deploymentName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(1).Info("reconciling deployment resource", "name", deploymentName)
	fetched := &appsv1.Deployment{}
	key := types.NamespacedName{
		Name:      desired.GetName(),
		Namespace: desired.GetNamespace(),
	}
	exist, err := r.Exists(r.ctx, key, fetched)
	if err != nil {
		return FromClientError(err, "failed to check %s deployment resource already exists", deploymentName)
	}

	if exist && istioCSRCreateRecon {
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s deployment resource already exists, maybe from previous installation", deploymentName)
	}
	if exist && hasObjectChanged(desired, fetched) {
		r.log.V(1).Info("deployment has been modified, updating to desired state", "name", deploymentName)
		if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to update %s deployment resource", deploymentName)
		}
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeNormal, "Reconciled", "deployment resource %s reconciled back to desired state", deploymentName)
	} else {
		r.log.V(1).Info("deployment resource already exists and is in expected state", "name", deploymentName)
	}
	if !exist {
		if err := r.Create(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to create %s deployment resource", deploymentName)
		}
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeNormal, "Reconciled", "deployment resource %s created", deploymentName)
	}

	if err := r.updateImageInStatus(istiocsr, desired); err != nil {
		return FromClientError(err, "failed to update %s/%s istiocsr status with image info", istiocsr.GetNamespace(), istiocsr.GetName())
	}
	return nil
}

func (r *Reconciler) getDeploymentObject(istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string) (*appsv1.Deployment, error) {
	if istiocsr.Spec.IstioCSRConfig == nil {
		return nil, NewIrrecoverableError(fmt.Errorf("%s/%s spec.IstioCSRConfig is empty", istiocsr.GetNamespace(), istiocsr.GetName()), "not creating deployment resource")
	}

	if err := r.assertIssuerRefExists(istiocsr); err != nil {
		return nil, fmt.Errorf("failed to verify issuer in %s/%s: %w", istiocsr.GetNamespace(), istiocsr.GetName(), err)
	}

	deployment := decodeDeploymentObjBytes(assets.MustAsset(deploymentAssetName))

	updateNamespace(deployment, istiocsr.GetNamespace())
	updateResourceLabels(deployment, resourceLabels)
	updatePodTemplateLabels(deployment, resourceLabels)
	updateResourceRequirement(deployment, istiocsr)
	updateAffinityRules(deployment, istiocsr)
	updatePodTolerations(deployment, istiocsr)
	updateNodeSelector(deployment, istiocsr)
	updateArgList(deployment, istiocsr)

	if err := r.updateImage(deployment, istiocsr); err != nil {
		return nil, NewIrrecoverableError(err, "failed to update image %s/%s", istiocsr.GetNamespace(), istiocsr.GetName())
	}
	if err := r.updateVolumes(deployment, istiocsr); err != nil {
		return nil, fmt.Errorf("failed to update volume %s/%s: %w", istiocsr.GetNamespace(), istiocsr.GetName(), err)
	}

	return deployment, nil
}

func (r *Reconciler) updateImage(deployment *appsv1.Deployment, istiocsr *v1alpha1.IstioCSR) error {
	image := os.Getenv(istiocsrImageNameEnvVarName)
	if image == "" {
		return fmt.Errorf("%s environment variable with istiocsr image not set", istiocsrImageNameEnvVarName)
	}
	for i, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name == istiocsrContainerName {
			deployment.Spec.Template.Spec.Containers[i].Image = image
		}
	}
	return nil
}

func (r *Reconciler) updateImageInStatus(istiocsr *v1alpha1.IstioCSR, deployment *appsv1.Deployment) error {
	for _, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name == istiocsrContainerName {
			if istiocsr.Status.IstioCSRImage == container.Image {
				return nil
			}
			istiocsr.Status.IstioCSRImage = container.Image
		}
	}
	return r.updateStatus(r.ctx, istiocsr)
}

func updatePodTemplateLabels(deployment *appsv1.Deployment, resourceLabels map[string]string) {
	deployment.Spec.Template.ObjectMeta.Labels = resourceLabels
}

func updateArgList(deployment *appsv1.Deployment, istiocsr *v1alpha1.IstioCSR) {
	istiocsrConfigs := istiocsr.Spec.IstioCSRConfig
	args := []string{
		fmt.Sprintf("--log-level=%d", istiocsrConfigs.LogLevel),
		fmt.Sprintf("--log-format=%s", istiocsrConfigs.LogFormat),
		"--metrics-port=9402", "--readiness-probe-port=6060", "--readiness-probe-path=/readyz",
		fmt.Sprintf("--certificate-namespace=%s", istiocsrConfigs.Istio.Namespace),
		"--issuer-enabled=true", "--preserve-certificate-requests=false",
		fmt.Sprintf("--issuer-name=%s", istiocsrConfigs.CertManager.IssuerRef.Name),
		fmt.Sprintf("--issuer-kind=%s", istiocsrConfigs.CertManager.IssuerRef.Kind),
		fmt.Sprintf("--issuer-group=%s", istiocsrConfigs.CertManager.IssuerRef.Group),
		fmt.Sprintf("--root-ca-file=%s/%s", caVolumeMountPath, istiocsrCAKeyName),
		fmt.Sprintf("--serving-certificate-dns-names=cert-manager-istio-csr.%s.svc", istiocsr.GetNamespace()),
		fmt.Sprintf("--serving-certificate-duration=%.0fm", istiocsrConfigs.IstiodTLSConfig.CertificateDuration.Minutes()),
		fmt.Sprintf("--trust-domain=%s", istiocsrConfigs.IstiodTLSConfig.TrustDomain),
		"--cluster-id=Kubernetes",
		fmt.Sprintf("--max-client-certificate-duration=%.0fm", istiocsrConfigs.IstiodTLSConfig.MaxCertificateDuration.Minutes()),
		"--serving-address=0.0.0.0:6443",
		fmt.Sprintf("--serving-certificate-key-size=%d", istiocsrConfigs.IstiodTLSConfig.PrivateKeySize),
		fmt.Sprintf("--serving-signature-algorithm=%s", istiocsrConfigs.IstiodTLSConfig.SignatureAlgorithm),
		"--enable-client-cert-authenticator=false",
		fmt.Sprintf("--leader-election-namespace=%s", istiocsrConfigs.Istio.Namespace),
		"--disable-kubernetes-client-rate-limiter=false", "--istiod-cert-enabled=false",
		fmt.Sprintf("--istiod-cert-namespace=%s", istiocsrConfigs.Istio.Namespace),
		fmt.Sprintf("--istiod-cert-duration=%.0fm", istiocsrConfigs.IstiodTLSConfig.CertificateDuration.Minutes()),
		fmt.Sprintf("--istiod-cert-renew-before=%.0fm", istiocsrConfigs.IstiodTLSConfig.CertificateRenewBefore.Minutes()),
		fmt.Sprintf("--istiod-cert-key-algorithm=%s", istiocsrConfigs.IstiodTLSConfig.SignatureAlgorithm),
		fmt.Sprintf("--istiod-cert-key-size=%d", istiocsrConfigs.IstiodTLSConfig.PrivateKeySize),
		fmt.Sprintf("--istiod-cert-additional-dns-names=%s", strings.Join(istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.CertificateDNSNames, ",")),
		fmt.Sprintf("--istiod-cert-istio-revisions=%s", strings.Join(istiocsrConfigs.Istio.Revisions, ",")),
	}

	for i, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name == istiocsrContainerName {
			deployment.Spec.Template.Spec.Containers[i].Args = args
		}
	}
}

func updateResourceRequirement(deployment *appsv1.Deployment, istiocsr *v1alpha1.IstioCSR) {
	for i := range deployment.Spec.Template.Spec.Containers {
		deployment.Spec.Template.Spec.Containers[i].Resources = istiocsr.Spec.IstioCSRConfig.Resources
	}
}

func updateAffinityRules(deployment *appsv1.Deployment, istiocsr *v1alpha1.IstioCSR) {
	if istiocsr.Spec.IstioCSRConfig.Affinity != nil {
		deployment.Spec.Template.Spec.Affinity = istiocsr.Spec.IstioCSRConfig.Affinity
	}
}

func updatePodTolerations(deployment *appsv1.Deployment, istiocsr *v1alpha1.IstioCSR) {
	if istiocsr.Spec.IstioCSRConfig.Tolerations != nil {
		deployment.Spec.Template.Spec.Tolerations = istiocsr.Spec.IstioCSRConfig.Tolerations
	}
}

func updateNodeSelector(deployment *appsv1.Deployment, istiocsr *v1alpha1.IstioCSR) {
	if istiocsr.Spec.IstioCSRConfig.NodeSelector != nil {
		deployment.Spec.Template.Spec.NodeSelector = istiocsr.Spec.IstioCSRConfig.NodeSelector
	}
}

func (r *Reconciler) assertIssuerRefExists(istiocsr *v1alpha1.IstioCSR) error {
	issuerRefKind := strings.ToLower(istiocsr.Spec.IstioCSRConfig.CertManager.IssuerRef.Kind)
	if issuerRefKind != clusterIssuerKind && issuerRefKind != issuerKind {
		return NewIrrecoverableError(invalidIssuerRefConfigError, "spec.istioCSRConfig.certManager.issuerRef.kind can be anyof `%s` or `%s`, configured: %s", clusterIssuerKind, issuerKind, issuerKind)
	}

	issuerRefGroup := strings.ToLower(istiocsr.Spec.IstioCSRConfig.CertManager.IssuerRef.Group)
	if issuerRefGroup != issuerGroup {
		return NewIrrecoverableError(invalidIssuerRefConfigError, "spec.istioCSRConfig.certManager.issuerRef.group can be only `%s`, configured: %s", issuerGroup, issuerRefGroup)
	}

	obj, err := r.getIssuer(istiocsr)
	if err != nil {
		return FromClientError(err, "failed to fetch issuer")
	}

	var issuerConfig certmanagerv1.IssuerConfig
	switch issuerRefKind {
	case clusterIssuerKind:
		issuerConfig = obj.(*certmanagerv1.ClusterIssuer).Spec.IssuerConfig
	case issuerKind:
		issuerConfig = obj.(*certmanagerv1.Issuer).Spec.IssuerConfig
	}
	if issuerConfig.ACME != nil {
		return NewIrrecoverableError(invalidIssuerRefConfigError, "spec.istioCSRConfig.certManager.issuerRef uses unsupported ACME issuer")
	}

	return nil
}

func (r *Reconciler) updateVolumes(deployment *appsv1.Deployment, istiocsr *v1alpha1.IstioCSR) error {
	var (
		issuerConfig certmanagerv1.IssuerConfig
	)

	obj, err := r.getIssuer(istiocsr)
	if err != nil {
		return FromClientError(err, "failed to fetch issuer")
	}

	issuerRefKind := strings.ToLower(istiocsr.Spec.IstioCSRConfig.CertManager.IssuerRef.Kind)
	switch issuerRefKind {
	case clusterIssuerKind:
		issuerConfig = obj.(*certmanagerv1.ClusterIssuer).Spec.IssuerConfig
	case issuerKind:
		issuerConfig = obj.(*certmanagerv1.Issuer).Spec.IssuerConfig
	}

	if issuerConfig.CA != nil && issuerConfig.CA.SecretName != "" {
		if err := r.createCAConfigMap(istiocsr, issuerConfig); err != nil {
			return FromClientError(err, "failed to create CA ConfigMap")
		}
		updateVolumeWithIssuerCA(deployment)
	}

	return nil
}

func updateVolumeWithIssuerCA(deployment *appsv1.Deployment) {
	const (
		caVolumeName = "root-ca"
	)
	var (
		defaultMode = int32(420)
	)

	volumeMounts := []corev1.VolumeMount{
		{
			Name:      caVolumeName,
			MountPath: caVolumeMountPath,
			ReadOnly:  true,
		},
	}

	volumes := []corev1.Volume{
		{
			Name: caVolumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: istiocsrCAConfigMapName,
					},
					Items: []corev1.KeyToPath{
						{
							Key:  istiocsrCAKeyName,
							Path: istiocsrCAKeyName,
							Mode: &defaultMode,
						},
					},
					DefaultMode: &defaultMode,
				},
			},
		},
	}

	for i, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name == istiocsrContainerName {
			deployment.Spec.Template.Spec.Containers[i].VolumeMounts = volumeMounts
		}
	}
	deployment.Spec.Template.Spec.Volumes = volumes
}

func (r *Reconciler) getIssuer(istiocsr *v1alpha1.IstioCSR) (client.Object, error) {
	issuerRefKind := strings.ToLower(istiocsr.Spec.IstioCSRConfig.CertManager.IssuerRef.Kind)
	namespacedName := types.NamespacedName{
		Name:      istiocsr.Spec.IstioCSRConfig.CertManager.IssuerRef.Name,
		Namespace: istiocsr.Spec.IstioCSRConfig.Istio.Namespace,
	}

	var object client.Object
	switch issuerRefKind {
	case clusterIssuerKind:
		object = &certmanagerv1.ClusterIssuer{}
	case issuerKind:
		object = &certmanagerv1.Issuer{}
	}

	if err := r.Get(r.ctx, namespacedName, object); err != nil {
		return nil, fmt.Errorf("failed to fetch %q issuer: %w", namespacedName, err)
	}
	return object, nil
}

func (r *Reconciler) createCAConfigMap(istiocsr *v1alpha1.IstioCSR, issuerConfig certmanagerv1.IssuerConfig) error {
	if issuerConfig.CA == nil || issuerConfig.CA.SecretName == "" {
		return nil
	}

	secretKey := types.NamespacedName{
		Name:      issuerConfig.CA.SecretName,
		Namespace: istiocsr.Spec.IstioCSRConfig.Istio.Namespace,
	}
	secret := &corev1.Secret{}
	if err := r.Get(r.ctx, secretKey, secret); err != nil {
		return fmt.Errorf("failed to fetch secret in issuer: %w", err)
	}
	if err := r.updateWatchLabelOnSecret(secret, istiocsr); err != nil {
		return err
	}

	configmapKey := types.NamespacedName{
		Name:      istiocsrCAConfigMapName,
		Namespace: istiocsr.GetNamespace(),
	}
	fetched := &corev1.ConfigMap{}
	exist, err := r.Exists(r.ctx, configmapKey, fetched)
	if err != nil {
		return fmt.Errorf("failed to check if CA configmap exists: %w", err)
	}

	desired := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configmapKey.Name,
			Namespace: configmapKey.Namespace,
		},
		Data: map[string]string{
			istiocsrCAKeyName: string(secret.Data[istiocsrCAKeyName]),
		},
	}
	if exist && hasObjectChanged(desired, fetched) {
		r.log.V(1).Info("ca configmap need update", "name", configmapKey)
		if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
			return fmt.Errorf("failed to update %s configmap resource: %w", configmapKey, err)
		}
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeNormal, "Reconciled", "configmap resource %s reconciled back to desired state", configmapKey)
	} else {
		r.log.V(1).Info("configmap resource already exists and is in expected state", "name", configmapKey)
	}
	if !exist {
		if err := r.Create(r.ctx, desired); err != nil {
			return fmt.Errorf("failed to create %s configmap resource: %w", configmapKey, err)
		}
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeNormal, "Reconciled", "configmap resource %s created", configmapKey)
	}
	return nil
}

func (r *Reconciler) updateWatchLabelOnSecret(secret *corev1.Secret, istiocsr *v1alpha1.IstioCSR) error {
	labels := secret.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[istiocsrResourceWatchLabelName] = fmt.Sprintf(istiocsrResourceWatchLabelValueFmt, istiocsr.GetNamespace(), istiocsr.GetName())
	secret.SetLabels(labels)

	if err := r.UpdateWithRetry(r.ctx, secret); err != nil {
		return fmt.Errorf("failed to update %s secret with custom watch label: %w", secret.GetName(), err)
	}
	return nil
}
