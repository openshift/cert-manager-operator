package trustmanager

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

// caBundle represents the JSON structure for the default CA package.
type caBundle struct {
	Certificates []caCertificate `json:"certificates"`
}

// caCertificate represents a single certificate entry in the CA bundle.
type caCertificate struct {
	PEM string `json:"pem"`
}

// reconcileDefaultCAPackage handles the default CA package feature.
// When enabled, it creates a ConfigMap with the OpenShift trusted CA bundle annotation
// to receive the cluster's CA bundle, then processes it into a JSON format
// suitable for trust-manager's --default-package-location.
func (r *Reconciler) reconcileDefaultCAPackage(tm *v1alpha1.TrustManager, resourceLabels map[string]string) error {
	policy := tm.Spec.TrustManagerConfig.DefaultCAPackage.Policy

	if policy != v1alpha1.DefaultCAPackagePolicyEnabled {
		// If default CA package is disabled, clean up any existing resources
		return r.deleteDefaultCAPackageResourcesIfExists(tm)
	}

	trustNamespace := tm.Spec.TrustManagerConfig.TrustNamespace
	if trustNamespace == "" {
		trustNamespace = defaultTrustNamespace
	}

	// Step 1: Create or verify the injector ConfigMap (with OpenShift annotation for CA bundle injection)
	if err := r.createOrApplyCABundleInjectorConfigMap(tm, trustNamespace, resourceLabels); err != nil {
		return err
	}

	// Step 2: Read the injected CA bundle and convert it to the JSON format trust-manager expects
	if err := r.createOrApplyDefaultCAPackageConfigMap(tm, trustNamespace, resourceLabels); err != nil {
		return err
	}

	return nil
}

// createOrApplyCABundleInjectorConfigMap creates or updates the ConfigMap that receives
// the OpenShift trusted CA bundle injection via the config.openshift.io/inject-trusted-cabundle annotation.
func (r *Reconciler) createOrApplyCABundleInjectorConfigMap(tm *v1alpha1.TrustManager, trustNamespace string, resourceLabels map[string]string) error {
	cmLabels := make(map[string]string, len(resourceLabels))
	for k, v := range resourceLabels {
		cmLabels[k] = v
	}

	desired := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      trustedCAConfigMapName,
			Namespace: trustNamespace,
			Labels:    cmLabels,
			Annotations: map[string]string{
				trustedCAAnnotation: "true",
			},
		},
	}

	cmKey := client.ObjectKeyFromObject(desired)
	fetched := &corev1.ConfigMap{}
	exist, err := r.Exists(r.ctx, cmKey, fetched)
	if err != nil {
		return FromClientError(err, "failed to check if CA bundle injector ConfigMap %s exists", cmKey)
	}

	if exist {
		// Ensure the annotation is present
		annotations := fetched.GetAnnotations()
		if annotations == nil || annotations[trustedCAAnnotation] != "true" {
			r.log.V(1).Info("CA bundle injector ConfigMap annotation missing, updating", "name", cmKey)
			fetched.Annotations = desired.Annotations
			if err := r.UpdateWithRetry(r.ctx, fetched); err != nil {
				return FromClientError(err, "failed to update CA bundle injector ConfigMap %s", cmKey)
			}
		}
	} else {
		if err := r.Create(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to create CA bundle injector ConfigMap %s", cmKey)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "CA bundle injector ConfigMap %s created", cmKey)
	}

	return nil
}

// createOrApplyDefaultCAPackageConfigMap reads the injected CA bundle and creates
// the JSON-formatted ConfigMap that trust-manager uses for the default CA package.
func (r *Reconciler) createOrApplyDefaultCAPackageConfigMap(tm *v1alpha1.TrustManager, trustNamespace string, resourceLabels map[string]string) error {
	// Read the injected CA bundle ConfigMap
	injectorKey := client.ObjectKey{
		Name:      trustedCAConfigMapName,
		Namespace: trustNamespace,
	}
	injectorCM := &corev1.ConfigMap{}
	if err := r.Get(r.ctx, injectorKey, injectorCM); err != nil {
		return FromClientError(err, "failed to fetch CA bundle injector ConfigMap %s", injectorKey)
	}

	bundleData, exists := injectorCM.Data[trustedCABundleKey]
	if !exists || bundleData == "" {
		// Bundle hasn't been injected yet - this is expected on first reconciliation.
		// The operator will re-reconcile when the ConfigMap is updated by the cluster network operator.
		r.log.V(1).Info("CA bundle not yet injected, will retry", "configmap", injectorKey)
		return NewRetryRequiredError(fmt.Errorf("CA bundle not yet injected into %s", injectorKey), "waiting for CA bundle injection")
	}

	// Convert PEM bundle to JSON format
	jsonData, err := convertPEMBundleToJSON(bundleData)
	if err != nil {
		return NewIrrecoverableError(err, "failed to convert CA bundle to JSON format")
	}

	desired := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultCAPackageConfigMapName,
			Namespace: trustNamespace,
			Labels:    resourceLabels,
		},
		Data: map[string]string{
			defaultCAPackageKey: jsonData,
		},
	}

	cmKey := client.ObjectKeyFromObject(desired)
	fetched := &corev1.ConfigMap{}
	exist, err := r.Exists(r.ctx, cmKey, fetched)
	if err != nil {
		return FromClientError(err, "failed to check if default CA package ConfigMap %s exists", cmKey)
	}

	if exist && hasObjectChanged(desired, fetched) {
		r.log.V(1).Info("default CA package ConfigMap needs update", "name", cmKey)
		if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to update default CA package ConfigMap %s", cmKey)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "default CA package ConfigMap %s updated", cmKey)
	}
	if !exist {
		if err := r.Create(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to create default CA package ConfigMap %s", cmKey)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "default CA package ConfigMap %s created", cmKey)
	}

	return nil
}

// deleteDefaultCAPackageResourcesIfExists removes the default CA package resources if they exist.
func (r *Reconciler) deleteDefaultCAPackageResourcesIfExists(tm *v1alpha1.TrustManager) error {
	trustNamespace := tm.Spec.TrustManagerConfig.TrustNamespace
	if trustNamespace == "" {
		trustNamespace = defaultTrustNamespace
	}

	// Delete the default CA package ConfigMap
	packageCM := &corev1.ConfigMap{}
	packageKey := client.ObjectKey{Name: defaultCAPackageConfigMapName, Namespace: trustNamespace}
	exist, err := r.Exists(r.ctx, packageKey, packageCM)
	if err != nil {
		return FromClientError(err, "failed to check if default CA package ConfigMap exists")
	}
	if exist {
		if err := r.Delete(r.ctx, packageCM); err != nil {
			return FromClientError(err, "failed to delete default CA package ConfigMap")
		}
		r.log.V(1).Info("deleted default CA package ConfigMap", "name", packageKey)
	}

	// Delete the CA bundle injector ConfigMap
	injectorCM := &corev1.ConfigMap{}
	injectorKey := client.ObjectKey{Name: trustedCAConfigMapName, Namespace: trustNamespace}
	exist, err = r.Exists(r.ctx, injectorKey, injectorCM)
	if err != nil {
		return FromClientError(err, "failed to check if CA bundle injector ConfigMap exists")
	}
	if exist {
		if err := r.Delete(r.ctx, injectorCM); err != nil {
			return FromClientError(err, "failed to delete CA bundle injector ConfigMap")
		}
		r.log.V(1).Info("deleted CA bundle injector ConfigMap", "name", injectorKey)
	}

	return nil
}

// convertPEMBundleToJSON converts a PEM-encoded CA bundle into the JSON format
// that trust-manager expects for the default CA package.
func convertPEMBundleToJSON(pemData string) (string, error) {
	var bundle caBundle
	rest := []byte(pemData)

	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}

		if block.Type != "CERTIFICATE" {
			continue
		}

		// Validate this is a parseable certificate
		if _, err := x509.ParseCertificate(block.Bytes); err != nil {
			// Skip unparseable certificates but log the issue
			continue
		}

		// Re-encode the PEM block to a clean string
		pemStr := string(pem.EncodeToMemory(block))
		bundle.Certificates = append(bundle.Certificates, caCertificate{PEM: pemStr})
	}

	if len(bundle.Certificates) == 0 {
		return "", fmt.Errorf("no valid certificates found in PEM bundle")
	}

	data, err := json.Marshal(bundle)
	if err != nil {
		return "", fmt.Errorf("failed to marshal CA bundle to JSON: %w", err)
	}

	return string(data), nil
}
