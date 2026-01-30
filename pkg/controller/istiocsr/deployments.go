package istiocsr

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"unsafe"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1validation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/kubernetes/pkg/apis/core"
	corevalidation "k8s.io/kubernetes/pkg/apis/core/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

var (
	errIstioCSRImageNotSet           = errors.New("environment variable with istiocsr image not set")
	errKeyNotFoundInConfigMap        = errors.New("key not found in ConfigMap")
	errFailedToFetchCACertificate    = errors.New("failed to fetch CA certificate configured for the issuer of CA type")
	errFailedToFindCACertificate     = errors.New("failed to find CA certificate")
	errPEMDataEmpty                  = errors.New("PEM data is empty")
	errNoValidPEMDataFound           = errors.New("no valid PEM data found")
	errPEMBlockNotCertificate        = errors.New("PEM block is not a certificate")
	errCertificateNoBasicConstraints = errors.New("certificate does not have valid Basic Constraints extension")
	errCertificateNotCA              = errors.New("certificate is not a CA certificate")
	errCertificateNoCertSignKeyUsage = errors.New("certificate does not have Certificate Sign key usage")
)

const (
	caVolumeMountPath = "/var/run/configmaps/istio-csr"
	// defaultVolumeMode is the default file permission mode for volumes (0644 in octal = 420 in decimal).
	defaultVolumeMode = int32(420)
)

var errInvalidIssuerRefConfig = fmt.Errorf("invalid issuerRef config")

func (r *Reconciler) createOrApplyDeployments(istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string, istioCSRCreateRecon bool) error {
	desired, err := r.getDeploymentObject(istiocsr, resourceLabels)
	if err != nil {
		return fmt.Errorf("failed to generate deployment resource for creation in %s: %w", istiocsr.GetNamespace(), err)
	}

	deploymentName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(logVerbosityLevelDebug).Info("reconciling deployment resource", "name", deploymentName)
	fetched := &appsv1.Deployment{}
	exist, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), fetched)
	if err != nil {
		return FromClientError(err, "failed to check %s deployment resource already exists", deploymentName)
	}

	if err := r.reconcileDeploymentResource(istiocsr, desired, fetched, deploymentName, exist, istioCSRCreateRecon); err != nil {
		return err
	}

	if err := r.updateImageInStatus(istiocsr, desired); err != nil {
		return FromClientError(err, "failed to update %s/%s istiocsr status with image info", istiocsr.GetNamespace(), istiocsr.GetName())
	}
	return nil
}

func (r *Reconciler) reconcileDeploymentResource(istiocsr *v1alpha1.IstioCSR, desired, fetched *appsv1.Deployment, deploymentName string, exist, istioCSRCreateRecon bool) error {
	if exist && istioCSRCreateRecon {
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s deployment resource already exists, maybe from previous installation", deploymentName)
	}
	if exist && hasObjectChanged(desired, fetched) {
		return r.updateDeploymentResource(istiocsr, desired, deploymentName)
	}
	if exist {
		r.log.V(logVerbosityLevelDebug).Info("deployment resource already exists and is in expected state", "name", deploymentName)
		return nil
	}
	return r.createDeploymentResource(istiocsr, desired, deploymentName)
}

func (r *Reconciler) updateDeploymentResource(istiocsr *v1alpha1.IstioCSR, desired *appsv1.Deployment, deploymentName string) error {
	r.log.V(1).Info("deployment has been modified, updating to desired state", "name", deploymentName)
	if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
		return FromClientError(err, "failed to update %s deployment resource", deploymentName)
	}
	r.eventRecorder.Eventf(istiocsr, corev1.EventTypeNormal, "Reconciled", "deployment resource %s reconciled back to desired state", deploymentName)
	return nil
}

func (r *Reconciler) createDeploymentResource(istiocsr *v1alpha1.IstioCSR, desired *appsv1.Deployment, deploymentName string) error {
	if err := r.Create(r.ctx, desired); err != nil {
		return FromClientError(err, "failed to create %s deployment resource", deploymentName)
	}
	r.eventRecorder.Eventf(istiocsr, corev1.EventTypeNormal, "Reconciled", "deployment resource %s created", deploymentName)
	return nil
}

func (r *Reconciler) getDeploymentObject(istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string) (*appsv1.Deployment, error) {
	if err := r.assertIssuerRefExists(istiocsr); err != nil {
		return nil, fmt.Errorf("failed to verify issuer in %s/%s: %w", istiocsr.GetNamespace(), istiocsr.GetName(), err)
	}

	deployment := decodeDeploymentObjBytes(assets.MustAsset(deploymentAssetName))

	updateNamespace(deployment, istiocsr.GetNamespace())
	updateResourceLabels(deployment, resourceLabels)
	updatePodTemplateLabels(deployment, resourceLabels)

	updateArgList(deployment, istiocsr)

	if err := updateResourceRequirement(deployment, istiocsr); err != nil {
		return nil, fmt.Errorf("failed to update resource requirements: %w", err)
	}
	if err := updateAffinityRules(deployment, istiocsr); err != nil {
		return nil, fmt.Errorf("failed to update affinity rules: %w", err)
	}
	if err := updatePodTolerations(deployment, istiocsr); err != nil {
		return nil, fmt.Errorf("failed to update pod tolerations: %w", err)
	}
	if err := updateNodeSelector(deployment, istiocsr); err != nil {
		return nil, fmt.Errorf("failed to update node selector: %w", err)
	}
	if err := r.updateImage(deployment); err != nil {
		return nil, NewIrrecoverableError(err, "failed to update image %s/%s", istiocsr.GetNamespace(), istiocsr.GetName())
	}
	if err := r.updateVolumes(deployment, istiocsr, resourceLabels); err != nil {
		return nil, fmt.Errorf("failed to update volume %s/%s: %w", istiocsr.GetNamespace(), istiocsr.GetName(), err)
	}

	return deployment, nil
}

func (r *Reconciler) updateImage(deployment *appsv1.Deployment) error {
	image := os.Getenv(istiocsrImageNameEnvVarName)
	if image == "" {
		return fmt.Errorf("%s %w", istiocsrImageNameEnvVarName, errIstioCSRImageNotSet)
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
	deployment.Spec.Template.Labels = resourceLabels
}

func updateArgList(deployment *appsv1.Deployment, istiocsr *v1alpha1.IstioCSR) {
	istiocsrConfigs := istiocsr.Spec.IstioCSRConfig
	// Default clusterID to "Kubernetes" if not provided.
	clusterID := "Kubernetes"
	if istiocsrConfigs.Server != nil && istiocsrConfigs.Server.ClusterID != "" {
		clusterID = istiocsrConfigs.Server.ClusterID
	}
	args := []string{
		fmt.Sprintf("--log-level=%d", istiocsrConfigs.LogLevel),
		fmt.Sprintf("--log-format=%s", istiocsrConfigs.LogFormat),
		"--metrics-port=9402", "--readiness-probe-port=6060", "--readiness-probe-path=/readyz",
		fmt.Sprintf("--certificate-namespace=%s", istiocsrConfigs.Istio.Namespace),
		"--issuer-enabled=true", "--preserve-certificate-requests=false",
		fmt.Sprintf("--issuer-name=%s", istiocsrConfigs.CertManager.IssuerRef.Name),
		fmt.Sprintf("--issuer-kind=%s", istiocsrConfigs.CertManager.IssuerRef.Kind),
		fmt.Sprintf("--issuer-group=%s", istiocsrConfigs.CertManager.IssuerRef.Group),
		fmt.Sprintf("--root-ca-file=%s/%s", caVolumeMountPath, IstiocsrCAKeyName),
		fmt.Sprintf("--serving-certificate-dns-names=cert-manager-istio-csr.%s.svc", istiocsr.GetNamespace()),
		fmt.Sprintf("--serving-certificate-duration=%.0fm", istiocsrConfigs.IstiodTLSConfig.CertificateDuration.Minutes()),
		fmt.Sprintf("--trust-domain=%s", istiocsrConfigs.IstiodTLSConfig.TrustDomain),
		fmt.Sprintf("--cluster-id=%s", clusterID),
		fmt.Sprintf("--max-client-certificate-duration=%.0fm", istiocsrConfigs.IstiodTLSConfig.MaxCertificateDuration.Minutes()),
		"--serving-address=0.0.0.0:6443",
		fmt.Sprintf("--serving-certificate-key-size=%d", istiocsrConfigs.IstiodTLSConfig.PrivateKeySize),
		fmt.Sprintf("--serving-signature-algorithm=%s", istiocsrConfigs.IstiodTLSConfig.PrivateKeyAlgorithm),
		"--enable-client-cert-authenticator=false",
		fmt.Sprintf("--leader-election-namespace=%s", istiocsrConfigs.Istio.Namespace),
		"--disable-kubernetes-client-rate-limiter=false", "--istiod-cert-enabled=false",
		fmt.Sprintf("--istiod-cert-namespace=%s", istiocsrConfigs.Istio.Namespace),
		fmt.Sprintf("--istiod-cert-duration=%.0fm", istiocsrConfigs.IstiodTLSConfig.CertificateDuration.Minutes()),
		fmt.Sprintf("--istiod-cert-renew-before=%.0fm", istiocsrConfigs.IstiodTLSConfig.CertificateRenewBefore.Minutes()),
		fmt.Sprintf("--istiod-cert-key-algorithm=%s", istiocsrConfigs.IstiodTLSConfig.PrivateKeyAlgorithm),
		fmt.Sprintf("--istiod-cert-key-size=%d", istiocsrConfigs.IstiodTLSConfig.PrivateKeySize),
		fmt.Sprintf("--istiod-cert-additional-dns-names=%s", strings.Join(istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.CertificateDNSNames, ",")),
		fmt.Sprintf("--istiod-cert-istio-revisions=%s", strings.Join(istiocsrConfigs.Istio.Revisions, ",")),
	}

	// Add configmap-namespace-selector argument if configured
	if istiocsrConfigs.IstioDataPlaneNamespaceSelector != "" {
		args = append(args, fmt.Sprintf("--configmap-namespace-selector=%s", istiocsrConfigs.IstioDataPlaneNamespaceSelector))
	}

	for i, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name == istiocsrContainerName {
			deployment.Spec.Template.Spec.Containers[i].Args = args
		}
	}
}

func updateResourceRequirement(deployment *appsv1.Deployment, istiocsr *v1alpha1.IstioCSR) error {
	if reflect.ValueOf(istiocsr.Spec.IstioCSRConfig.Resources).IsZero() {
		return nil
	}
	if err := validateResourceRequirements(istiocsr.Spec.IstioCSRConfig.Resources,
		field.NewPath("spec", "istioCSRConfig")); err != nil {
		return err
	}
	for i := range deployment.Spec.Template.Spec.Containers {
		deployment.Spec.Template.Spec.Containers[i].Resources = istiocsr.Spec.IstioCSRConfig.Resources
	}
	return nil
}

func updateAffinityRules(deployment *appsv1.Deployment, istiocsr *v1alpha1.IstioCSR) error {
	if istiocsr.Spec.IstioCSRConfig.Affinity == nil {
		return nil
	}
	if err := validateAffinityRules(istiocsr.Spec.IstioCSRConfig.Affinity,
		field.NewPath("spec", "istioCSRConfig")); err != nil {
		return err
	}
	deployment.Spec.Template.Spec.Affinity = istiocsr.Spec.IstioCSRConfig.Affinity
	return nil
}

func updatePodTolerations(deployment *appsv1.Deployment, istiocsr *v1alpha1.IstioCSR) error {
	if istiocsr.Spec.IstioCSRConfig.Tolerations == nil {
		return nil
	}
	if err := validateTolerationsConfig(istiocsr.Spec.IstioCSRConfig.Tolerations,
		field.NewPath("spec", "istioCSRConfig")); err != nil {
		return err
	}
	deployment.Spec.Template.Spec.Tolerations = istiocsr.Spec.IstioCSRConfig.Tolerations
	return nil
}

func updateNodeSelector(deployment *appsv1.Deployment, istiocsr *v1alpha1.IstioCSR) error {
	if istiocsr.Spec.IstioCSRConfig.NodeSelector == nil {
		return nil
	}
	if err := validateNodeSelectorConfig(istiocsr.Spec.IstioCSRConfig.NodeSelector,
		field.NewPath("spec", "istioCSRConfig")); err != nil {
		return err
	}
	deployment.Spec.Template.Spec.NodeSelector = istiocsr.Spec.IstioCSRConfig.NodeSelector
	return nil
}

func (r *Reconciler) assertIssuerRefExists(istiocsr *v1alpha1.IstioCSR) error {
	issuerRefKind := strings.ToLower(istiocsr.Spec.IstioCSRConfig.CertManager.IssuerRef.Kind)
	if issuerRefKind != clusterIssuerKind && issuerRefKind != issuerKind {
		return NewIrrecoverableError(errInvalidIssuerRefConfig, "spec.istioCSRConfig.certManager.issuerRef.kind can be any of `%s` or `%s`, configured: %s", clusterIssuerKind, issuerKind, issuerKind)
	}

	issuerRefGroup := strings.ToLower(istiocsr.Spec.IstioCSRConfig.CertManager.IssuerRef.Group)
	if issuerRefGroup != issuerGroup {
		return NewIrrecoverableError(errInvalidIssuerRefConfig, "spec.istioCSRConfig.certManager.issuerRef.group can be only `%s`, configured: %s", issuerGroup, issuerRefGroup)
	}

	obj, err := r.getIssuer(istiocsr)
	if err != nil {
		return FromClientError(err, "failed to fetch issuer")
	}

	var issuerConfig certmanagerv1.IssuerConfig
	switch issuerRefKind {
	case clusterIssuerKind:
		clusterIssuer, ok := obj.(*certmanagerv1.ClusterIssuer)
		if !ok {
			return NewIrrecoverableError(errInvalidIssuerRefConfig, "failed to convert to ClusterIssuer")
		}
		issuerConfig = clusterIssuer.Spec.IssuerConfig
	case issuerKind:
		issuer, ok := obj.(*certmanagerv1.Issuer)
		if !ok {
			return NewIrrecoverableError(errInvalidIssuerRefConfig, "failed to convert to Issuer")
		}
		issuerConfig = issuer.Spec.IssuerConfig
	}
	if issuerConfig.ACME != nil {
		return NewIrrecoverableError(errInvalidIssuerRefConfig, "spec.istioCSRConfig.certManager.issuerRef uses unsupported ACME issuer")
	}

	return nil
}

func (r *Reconciler) updateVolumes(deployment *appsv1.Deployment, istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string) error {
	// Use user-configured CA certificate if provided
	if istiocsr.Spec.IstioCSRConfig.CertManager.IstioCACertificate != nil {
		if err := r.handleUserProvidedCA(deployment, istiocsr, resourceLabels); err != nil {
			return FromError(err, "failed to validate and mount CA certificate ConfigMap")
		}
		return nil
	}

	// Fall back to issuer-based CA certificate if CA certificate is not configured
	// Handle issuer-based CA configuration
	if err := r.handleIssuerBasedCA(deployment, istiocsr, resourceLabels); err != nil {
		return err
	}

	return nil
}

// handleUserProvidedCA processes user-provided CA certificate configuration.
//
// It performs the following operations:
//  1. Validates that the source ConfigMap exists
//  2. Adds a watch label to the source ConfigMap for change tracking
//  3. Validates that the ConfigMap contains the specified key and is a valid CA certificate
//  4. Creates or updates a copy of the ConfigMap in the IstioCSR namespace
//  5. Configures the deployment to mount the CA certificate volume
//
// The caller must ensure that `istiocsr.Spec.IstioCSRConfig.CertManager.IstioCACertificate`
// is not nil before calling this function to avoid a panic.
//
// Returns an error if any validation fails or if ConfigMap operations fail.
func (r *Reconciler) handleUserProvidedCA(deployment *appsv1.Deployment, istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string) error {
	caCertConfig := istiocsr.Spec.IstioCSRConfig.CertManager.IstioCACertificate

	// Determine the namespace - use specified namespace or default to IstioCSR namespace
	namespace := caCertConfig.Namespace
	if namespace == "" {
		namespace = istiocsr.GetNamespace()
	}

	// Validate that the source ConfigMap exists
	sourceConfigMapKey := types.NamespacedName{
		Name:      caCertConfig.Name,
		Namespace: namespace,
	}

	sourceConfigMap := &corev1.ConfigMap{}
	if err := r.Get(r.ctx, sourceConfigMapKey, sourceConfigMap); err != nil {
		return NewIrrecoverableError(err, "failed to fetch CA certificate ConfigMap %s/%s", sourceConfigMapKey.Namespace, sourceConfigMapKey.Name)
	}

	// Add watch label to the source ConfigMap to trigger reconciliation on changes.
	// This is done before validation so that if validation fails now, fixing the ConfigMap
	// will trigger reconciliation.
	if err := r.updateWatchLabel(sourceConfigMap, istiocsr); err != nil {
		return FromClientError(err, "failed to update watch label on CA certificate ConfigMap %s/%s", sourceConfigMapKey.Namespace, sourceConfigMapKey.Name)
	}

	// Validate that the specified key exists in the ConfigMap
	if _, exists := sourceConfigMap.Data[caCertConfig.Key]; !exists {
		return NewIrrecoverableError(fmt.Errorf("key %q not found in ConfigMap %s/%s", caCertConfig.Key, sourceConfigMapKey.Namespace, sourceConfigMapKey.Name), "invalid CA certificate ConfigMap %s/%s", sourceConfigMapKey.Namespace, sourceConfigMapKey.Name)
	}

	// Validate that the key contains PEM-formatted content
	pemData := sourceConfigMap.Data[caCertConfig.Key]
	if err := r.validatePEMData(pemData); err != nil {
		return NewIrrecoverableError(err, "invalid PEM data in CA certificate ConfigMap %s/%s key %q", sourceConfigMapKey.Namespace, sourceConfigMapKey.Name, caCertConfig.Key)
	}

	// Create a managed copy of the ConfigMap in the IstioCSR namespace.
	// The operator does not directly mount the user-provided ConfigMap but creates a managed copy
	// (cert-manager-istio-csr-issuer-ca-copy) in the IstioCSR namespace. This approach enables the
	// operator to validate any changes to the source ConfigMap before propagating them to the istio-csr
	// agent, as ConfigMap changes automatically propagate to mounted pods. The watch label on the source
	// ConfigMap triggers reconciliation when modified, allowing the operator to re-validate and update
	// its managed copy. Additionally, if a user directly modifies the operator-managed copy, it will be
	// reconciled back to the desired state derived from the validated source ConfigMap.
	if err := r.createOrUpdateCAConfigMap(istiocsr, pemData, resourceLabels); err != nil {
		return FromClientError(err, "failed to create CA certificate ConfigMap copy")
	}

	// Mount the copied CA certificate ConfigMap (always uses the standard name)
	updateVolumeWithIssuerCA(deployment)

	return nil
}

// handleIssuerBasedCA handles the creation of CA ConfigMap from issuer secret and volume mounting.
func (r *Reconciler) handleIssuerBasedCA(deployment *appsv1.Deployment, istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string) error {
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
		clusterIssuer, ok := obj.(*certmanagerv1.ClusterIssuer)
		if !ok {
			return FromClientError(fmt.Errorf("failed to convert to ClusterIssuer"), "failed to fetch issuer")
		}
		issuerConfig = clusterIssuer.Spec.IssuerConfig
	case issuerKind:
		issuer, ok := obj.(*certmanagerv1.Issuer)
		if !ok {
			return FromClientError(fmt.Errorf("failed to convert to Issuer"), "failed to fetch issuer")
		}
		issuerConfig = issuer.Spec.IssuerConfig
	}

	shouldUpdateVolume := false

	if issuerConfig.CA != nil && issuerConfig.CA.SecretName != "" {
		if err := r.createCAConfigMapFromIssuerSecret(istiocsr, issuerConfig, resourceLabels); err != nil {
			return FromClientError(err, "failed to create CA ConfigMap")
		}
		shouldUpdateVolume = true
	}

	if issuerConfig.CA == nil {
		if err := r.createCAConfigMapFromIstiodCertificate(istiocsr, resourceLabels); err != nil {
			return FromClientError(err, "failed to create CA ConfigMap")
		}
		shouldUpdateVolume = true
	}

	if shouldUpdateVolume {
		updateVolumeWithIssuerCA(deployment)
	}

	return nil
}

func updateVolumeWithIssuerCA(deployment *appsv1.Deployment) {
	const (
		caVolumeName = "root-ca"
	)
	var (
		defaultMode = defaultVolumeMode
	)

	desiredVolumeMount := corev1.VolumeMount{
		Name:      caVolumeName,
		MountPath: caVolumeMountPath,
		ReadOnly:  true,
	}

	desiredVolume := corev1.Volume{
		Name: caVolumeName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: IstiocsrCAConfigMapName,
				},
				Items: []corev1.KeyToPath{
					{
						Key:  IstiocsrCAKeyName,
						Path: IstiocsrCAKeyName,
						Mode: &defaultMode,
					},
				},
				DefaultMode: &defaultMode,
			},
		},
	}

	// Update or append volume mount in the istio-csr container
	for i, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name == istiocsrContainerName {
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

func (r *Reconciler) getIssuer(istiocsr *v1alpha1.IstioCSR) (client.Object, error) {
	issuerRefKind := strings.ToLower(istiocsr.Spec.IstioCSRConfig.CertManager.IssuerRef.Kind)
	key := client.ObjectKey{
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

	if err := r.Get(r.ctx, key, object); err != nil {
		return nil, fmt.Errorf("failed to fetch %q issuer: %w", key, err)
	}
	return object, nil
}

func (r *Reconciler) createCAConfigMapFromIstiodCertificate(istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string) error {
	istiodCertificate, err := r.getCertificateObject(istiocsr, resourceLabels)
	if err != nil {
		return FromClientError(err, "failed to fetch istiod certificate")
	}

	secretKey := client.ObjectKey{
		Name:      istiodCertificate.Spec.SecretName,
		Namespace: istiodCertificate.GetNamespace(),
	}
	secret := &corev1.Secret{}
	if err := r.Get(r.ctx, secretKey, secret); err != nil {
		return fmt.Errorf("failed to fetch secret in issuer: %w", err)
	}
	if err := r.updateWatchLabel(secret, istiocsr); err != nil {
		return err
	}

	certData := string(secret.Data[IstiocsrCAKeyName])
	return r.createOrUpdateCAConfigMap(istiocsr, certData, resourceLabels)
}

func (r *Reconciler) createCAConfigMapFromIssuerSecret(istiocsr *v1alpha1.IstioCSR, issuerConfig certmanagerv1.IssuerConfig, resourceLabels map[string]string) error {
	if issuerConfig.CA.SecretName == "" {
		return fmt.Errorf("%w: %s", errFailedToFetchCACertificate, istiocsr.Spec.IstioCSRConfig.CertManager.IssuerRef.Name)
	}

	secretKey := client.ObjectKey{
		Name:      issuerConfig.CA.SecretName,
		Namespace: istiocsr.Spec.IstioCSRConfig.Istio.Namespace,
	}
	secret := &corev1.Secret{}
	if err := r.Get(r.ctx, secretKey, secret); err != nil {
		return fmt.Errorf("failed to fetch secret in issuer: %w", err)
	}
	if err := r.updateWatchLabel(secret, istiocsr); err != nil {
		return err
	}

	certData := string(secret.Data[IstiocsrCAKeyName])
	return r.createOrUpdateCAConfigMap(istiocsr, certData, resourceLabels)
}

// createOrUpdateCAConfigMap creates or updates the CA ConfigMap with the provided certificate data.
func (r *Reconciler) createOrUpdateCAConfigMap(istiocsr *v1alpha1.IstioCSR, certData string, resourceLabels map[string]string) error {
	if certData == "" {
		return errFailedToFindCACertificate
	}

	configmapKey := client.ObjectKey{
		Name:      IstiocsrCAConfigMapName,
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
			Labels:    resourceLabels,
		},
		Data: map[string]string{
			IstiocsrCAKeyName: certData,
		},
	}

	if exist && hasObjectChanged(desired, fetched) {
		r.log.V(1).Info("ca configmap need update", "name", configmapKey)
		if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
			return fmt.Errorf("failed to update %s configmap resource: %w", configmapKey, err)
		}
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeNormal, "Reconciled", "configmap resource %s reconciled back to desired state", configmapKey)
	} else {
		r.log.V(logVerbosityLevelDebug).Info("configmap resource already exists and is in expected state", "name", configmapKey)
	}

	if !exist {
		if err := r.Create(r.ctx, desired); err != nil {
			return fmt.Errorf("failed to create %s configmap resource: %w", configmapKey, err)
		}
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeNormal, "Reconciled", "configmap resource %s created", configmapKey)
	}

	return nil
}

func (r *Reconciler) validatePEMData(pemData string) error {
	if pemData == "" {
		return errPEMDataEmpty
	}

	// Parse the first certificate from PEM data
	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		return errNoValidPEMDataFound
	}

	if block.Type != "CERTIFICATE" {
		return fmt.Errorf("%w, found: %s", errPEMBlockNotCertificate, block.Type)
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse certificate: %w", err)
	}

	if !cert.BasicConstraintsValid {
		return errCertificateNoBasicConstraints
	}

	if !cert.IsCA {
		return errCertificateNotCA
	}

	// Check Key Usage for certificate signing
	if cert.KeyUsage&x509.KeyUsageCertSign == 0 {
		return errCertificateNoCertSignKeyUsage
	}

	return nil
}

// updateWatchLabel adds a watch label to any Kubernetes object that supports labels.
func (r *Reconciler) updateWatchLabel(obj client.Object, istiocsr *v1alpha1.IstioCSR) error {
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[IstiocsrResourceWatchLabelName] = fmt.Sprintf(istiocsrResourceWatchLabelValueFmt, istiocsr.GetNamespace(), istiocsr.GetName())
	obj.SetLabels(labels)

	if err := r.UpdateWithRetry(r.ctx, obj); err != nil {
		return fmt.Errorf("failed to update %s resource with watch label: %w", obj.GetName(), err)
	}
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
