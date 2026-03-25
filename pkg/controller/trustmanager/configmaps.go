package trustmanager

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
)

// caPackage represents the JSON format expected by trust-manager
type caPackage struct {
	Name    string `json:"name"`
	Bundle  string `json:"bundle"`
	Version string `json:"version"`
}

// createOrApplyDefaultCAPackageConfigMap reads the CNO-injected CA bundle,
// formats it into trust-manager's expected JSON package format, and creates
// or updates the package ConfigMap in the operand namespace.
// Returns the SHA-256 hash of the CA bundle content and any error.
// Returns ("", nil) when defaultCAPackage is disabled.
func (r *Reconciler) createOrApplyDefaultCAPackageConfigMap(trustManager *v1alpha1.TrustManager, resourceLabels, resourceAnnotations map[string]string) (string, error) {
	if !defaultCAPackageEnabled(trustManager.Spec.TrustManagerConfig.DefaultCAPackage) {
		return "", nil
	}

	caBundle, resourceVersion, err := r.readTrustedCABundle()
	if err != nil {
		return "", err
	}

	pkgJSON, err := formatCAPackage(caBundle, resourceVersion)
	if err != nil {
		return "", err
	}

	bundleHash := computeCABundleHash(caBundle)

	desired := buildDefaultCAPackageConfigMap(pkgJSON, resourceLabels, resourceAnnotations)

	cmName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(4).Info("reconciling default CA package ConfigMap", "name", cmName)

	existing := &corev1.ConfigMap{}
	exists, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), existing)
	if err != nil {
		return "", common.FromClientError(err, "failed to check if ConfigMap %q exists", cmName)
	}
	if exists && !configMapModified(desired, existing) {
		r.log.V(4).Info("default CA package ConfigMap exists and is in desired state", "name", cmName)
		return bundleHash, nil
	}

	r.log.V(2).Info("default CA package ConfigMap has been modified, updating to desired state", "name", cmName)
	if err := r.Patch(r.ctx, desired, client.Apply, client.FieldOwner(fieldOwner), client.ForceOwnership); err != nil {
		return "", common.FromClientError(err, "failed to apply ConfigMap %q", cmName)
	}

	r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "default CA package ConfigMap %s applied", cmName)
	return bundleHash, nil
}

// readTrustedCABundle reads the CNO-injected CA bundle from the operator namespace.
// Returns the PEM bundle, the ConfigMap's resource version, and any error.
func (r *Reconciler) readTrustedCABundle() (string, string, error) {
	injectionCM := &corev1.ConfigMap{}
	key := client.ObjectKey{
		Namespace: common.OperatorNamespace,
		Name:      common.TrustedCABundleConfigMapName,
	}
	if err := r.Get(r.ctx, key, injectionCM); err != nil {
		return "", "", common.FromClientError(
			err,
			"failed to read CA bundle ConfigMap %q in namespace %q",
			common.TrustedCABundleConfigMapName, common.OperatorNamespace,
		)
	}

	caBundle, ok := injectionCM.Data[common.TrustedCABundleKey]
	if !ok || caBundle == "" {
		return "", "", common.FromClientError(
			fmt.Errorf("CA bundle ConfigMap %q does not contain key %q", common.TrustedCABundleConfigMapName, common.TrustedCABundleKey),
			"missing key %q in ConfigMap %q", common.TrustedCABundleKey, common.TrustedCABundleConfigMapName,
		)
	}

	return caBundle, injectionCM.ResourceVersion, nil
}

// formatCAPackage encodes the CA bundle into trust-manager's expected JSON package format.
func formatCAPackage(caBundle, version string) ([]byte, error) {
	pkg := caPackage{
		Name:    defaultCAPackageName,
		Bundle:  caBundle,
		Version: version,
	}
	// TODO: cross check the formatting of the JSON package
	pkgJSON, err := json.Marshal(pkg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal CA package to JSON: %w", err)
	}
	return pkgJSON, nil
}

// computeCABundleHash returns the SHA-256 hash of the CA bundle PEM content.
// The hash is used as the pod template annotation to trigger rolling restarts
// only when the actual certificate content changes.
func computeCABundleHash(caBundle string) string {
	hash := sha256.Sum256([]byte(caBundle))
	return hex.EncodeToString(hash[:])
}

// buildDefaultCAPackageConfigMap constructs the desired ConfigMap for the
// formatted CA package in the operand namespace.
func buildDefaultCAPackageConfigMap(pkgJSON []byte, resourceLabels, resourceAnnotations map[string]string) *corev1.ConfigMap {
	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultCAPackageConfigMapName,
			Namespace: operandNamespace,
		},
		Data: map[string]string{
			defaultCAPackageFilename: string(pkgJSON),
		},
	}
	common.UpdateResourceLabels(cm, resourceLabels)
	updateResourceAnnotations(cm, resourceAnnotations)
	return cm
}

// configMapModified checks whether the desired ConfigMap differs from the existing one.
func configMapModified(desired, existing *corev1.ConfigMap) bool {
	return managedMetadataModified(desired, existing) ||
		!maps.Equal(desired.Data, existing.Data)
}
