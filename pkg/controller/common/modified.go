package common

import (
	"maps"
	"reflect"
	"slices"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
)

// ManagedLabelsModified reports whether any label on desired is missing or
// different on existing. Extra labels on existing are ignored.
func ManagedLabelsModified(desired, existing client.Object) bool {
	existingLabels := existing.GetLabels()
	for k, v := range desired.GetLabels() {
		if existingLabels[k] != v {
			return true
		}
	}
	return false
}

// ManagedAnnotationsModified reports whether any annotation on desired is
// missing or different on existing. Extra annotations on existing are ignored.
func ManagedAnnotationsModified(desired, existing client.Object) bool {
	existingAnnotations := existing.GetAnnotations()
	for k, v := range desired.GetAnnotations() {
		if existingAnnotations[k] != v {
			return true
		}
	}
	return false
}

// ManagedMetadataModified reports whether any managed label or annotation has drifted.
func ManagedMetadataModified(desired, existing client.Object) bool {
	return ManagedLabelsModified(desired, existing) || ManagedAnnotationsModified(desired, existing)
}

// CertificateManagedFieldsModified compares the Certificate spec fields the
// trust-manager controller sets. Other spec fields are ignored because the
// cert-manager webhook defaults them (for example Duration).
func CertificateManagedFieldsModified(desired, existing *certmanagerv1.Certificate) bool {
	if desired.Spec.CommonName != existing.Spec.CommonName ||
		!slices.Equal(desired.Spec.DNSNames, existing.Spec.DNSNames) ||
		desired.Spec.SecretName != existing.Spec.SecretName ||
		!ptr.Equal(desired.Spec.RevisionHistoryLimit, existing.Spec.RevisionHistoryLimit) ||
		!reflect.DeepEqual(desired.Spec.IssuerRef, existing.Spec.IssuerRef) {
		return true
	}
	return false
}

// CertificateSpecModified reports whether the full Certificate specs differ.
func CertificateSpecModified(desired, existing *certmanagerv1.Certificate) bool {
	return !reflect.DeepEqual(desired.Spec, existing.Spec)
}

// IssuerSpecModified reports whether the Issuer specs differ.
func IssuerSpecModified(desired, existing *certmanagerv1.Issuer) bool {
	return !reflect.DeepEqual(desired.Spec, existing.Spec)
}

// ServiceSpecModified compares Service type, selector, and ports.
// Selector uses maps.Equal, so a nil selector and an empty selector match.
func ServiceSpecModified(desired, existing *corev1.Service) bool {
	if desired.Spec.Type != existing.Spec.Type ||
		!maps.Equal(desired.Spec.Selector, existing.Spec.Selector) ||
		!reflect.DeepEqual(desired.Spec.Ports, existing.Spec.Ports) {
		return true
	}
	return false
}

// ConfigMapDataModified reports whether ConfigMap data differs.
// A nil data map and an empty data map match.
func ConfigMapDataModified(desired, existing *corev1.ConfigMap) bool {
	return !maps.Equal(desired.Data, existing.Data)
}

// RBACRulesModified reports whether PolicyRules differ.
func RBACRulesModified(desired, existing []rbacv1.PolicyRule) bool {
	return !reflect.DeepEqual(desired, existing)
}

// RBACRoleRefModified reports whether RoleRefs differ.
func RBACRoleRefModified(desired, existing rbacv1.RoleRef) bool {
	return !reflect.DeepEqual(desired, existing)
}

// RBACSubjectsModified reports whether RBAC subjects differ.
func RBACSubjectsModified(desired, existing []rbacv1.Subject) bool {
	return !reflect.DeepEqual(desired, existing)
}

// NetworkPolicySpecModified reports whether NetworkPolicy specs differ.
func NetworkPolicySpecModified(desired, existing *networkingv1.NetworkPolicy) bool {
	return !reflect.DeepEqual(desired.Spec, existing.Spec)
}

// ServiceAccountModified reports whether AutomountServiceAccountToken differs.
func ServiceAccountModified(desired, existing *corev1.ServiceAccount) bool {
	return !ptr.Equal(desired.AutomountServiceAccountToken, existing.AutomountServiceAccountToken)
}
