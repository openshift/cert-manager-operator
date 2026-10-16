package common

import (
	"fmt"
	"maps"
	"reflect"
	"slices"

	appsv1 "k8s.io/api/apps/v1"
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
		!reflect.DeepEqual(desired.Spec.IssuerRef, existing.Spec.IssuerRef) ||
		!reflect.DeepEqual(desired.Spec.SecretTemplate, existing.Spec.SecretTemplate) ||
		certificatePrivateKeyRotationPolicy(desired.Spec.PrivateKey) != certificatePrivateKeyRotationPolicy(existing.Spec.PrivateKey) {
		return true
	}
	return false
}

// certificatePrivateKeyRotationPolicy returns the rotation policy we manage on
// the webhook Certificate. Other PrivateKey fields (algorithm, encoding, size)
// are omitted from the comparison because cert-manager may default them.
func certificatePrivateKeyRotationPolicy(pk *certmanagerv1.CertificatePrivateKey) certmanagerv1.PrivateKeyRotationPolicy {
	if pk == nil {
		return ""
	}
	return pk.RotationPolicy
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

// HasObjectChanged reports whether desired and fetched differ in managed
// labels or annotations, or in the spec fields the operand controllers reconcile.
// desired and fetched must be the same concrete type.
func HasObjectChanged(desired, fetched client.Object) bool {
	if reflect.TypeOf(desired) != reflect.TypeOf(fetched) {
		panic("both objects to be compared must be of same type")
	}

	var objectModified bool
	switch desiredObj := desired.(type) {
	case *certmanagerv1.Certificate:
		fetchedCert, ok := fetched.(*certmanagerv1.Certificate)
		if !ok {
			panic("failed to convert fetched to *certmanagerv1.Certificate")
		}
		objectModified = CertificateSpecModified(desiredObj, fetchedCert)
	case *certmanagerv1.Issuer:
		fetchedIssuer, ok := fetched.(*certmanagerv1.Issuer)
		if !ok {
			panic("failed to convert fetched to *certmanagerv1.Issuer")
		}
		objectModified = IssuerSpecModified(desiredObj, fetchedIssuer)
	case *rbacv1.ClusterRole:
		fetchedClusterRole, ok := fetched.(*rbacv1.ClusterRole)
		if !ok {
			panic("failed to convert fetched to *rbacv1.ClusterRole")
		}
		objectModified = RBACRulesModified(desiredObj.Rules, fetchedClusterRole.Rules)
	case *rbacv1.ClusterRoleBinding:
		fetchedClusterRoleBinding, ok := fetched.(*rbacv1.ClusterRoleBinding)
		if !ok {
			panic("failed to convert fetched to *rbacv1.ClusterRoleBinding")
		}
		objectModified = RBACRoleRefModified(desiredObj.RoleRef, fetchedClusterRoleBinding.RoleRef) ||
			RBACSubjectsModified(desiredObj.Subjects, fetchedClusterRoleBinding.Subjects)
	case *appsv1.Deployment:
		fetchedDeployment, ok := fetched.(*appsv1.Deployment)
		if !ok {
			panic("failed to convert fetched to *appsv1.Deployment")
		}
		objectModified = DeploymentSpecModified(desiredObj, fetchedDeployment)
	case *rbacv1.Role:
		fetchedRole, ok := fetched.(*rbacv1.Role)
		if !ok {
			panic("failed to convert fetched to *rbacv1.Role")
		}
		objectModified = RBACRulesModified(desiredObj.Rules, fetchedRole.Rules)
	case *rbacv1.RoleBinding:
		fetchedRoleBinding, ok := fetched.(*rbacv1.RoleBinding)
		if !ok {
			panic("failed to convert fetched to *rbacv1.RoleBinding")
		}
		objectModified = RBACRoleRefModified(desiredObj.RoleRef, fetchedRoleBinding.RoleRef) ||
			RBACSubjectsModified(desiredObj.Subjects, fetchedRoleBinding.Subjects)
	case *corev1.Service:
		fetchedService, ok := fetched.(*corev1.Service)
		if !ok {
			panic("failed to convert fetched to *corev1.Service")
		}
		objectModified = ServiceSpecModified(desiredObj, fetchedService)
	case *corev1.ConfigMap:
		fetchedConfigMap, ok := fetched.(*corev1.ConfigMap)
		if !ok {
			panic("failed to convert fetched to *corev1.ConfigMap")
		}
		objectModified = ConfigMapDataModified(desiredObj, fetchedConfigMap)
	case *networkingv1.NetworkPolicy:
		fetchedNetworkPolicy, ok := fetched.(*networkingv1.NetworkPolicy)
		if !ok {
			panic("failed to convert fetched to *networkingv1.NetworkPolicy")
		}
		objectModified = NetworkPolicySpecModified(desiredObj, fetchedNetworkPolicy)
	case *corev1.ServiceAccount:
		fetchedServiceAccount, ok := fetched.(*corev1.ServiceAccount)
		if !ok {
			panic("failed to convert fetched to *corev1.ServiceAccount")
		}
		objectModified = ServiceAccountModified(desiredObj, fetchedServiceAccount)
	default:
		panic(fmt.Sprintf("unsupported object type: %T", desired))
	}
	return objectModified || ManagedMetadataModified(desired, fetched)
}

// DeploymentSpecModified compares the Deployment fields the operand controllers set.
// NodeSelector, Affinity, Tolerations, and RevisionHistoryLimit are compared only
// when desired sets them, so an operand that leaves a field unset does not treat
// API defaults on the fetched object as drift.
func DeploymentSpecModified(desired, fetched *appsv1.Deployment) bool {
	if !ptr.Equal(desired.Spec.Replicas, fetched.Spec.Replicas) ||
		!reflect.DeepEqual(desired.Spec.Selector, fetched.Spec.Selector) {
		return true
	}

	if desired.Spec.RevisionHistoryLimit != nil &&
		!ptr.Equal(desired.Spec.RevisionHistoryLimit, fetched.Spec.RevisionHistoryLimit) {
		return true
	}

	if !maps.Equal(desired.Spec.Template.Labels, fetched.Spec.Template.Labels) ||
		!maps.Equal(desired.Spec.Template.Annotations, fetched.Spec.Template.Annotations) {
		return true
	}

	desiredPodSpec := desired.Spec.Template.Spec
	fetchedPodSpec := fetched.Spec.Template.Spec

	if desiredPodSpec.ServiceAccountName != fetchedPodSpec.ServiceAccountName ||
		!reflect.DeepEqual(desiredPodSpec.Volumes, fetchedPodSpec.Volumes) {
		return true
	}

	if desiredPodSpec.NodeSelector != nil &&
		!maps.Equal(desiredPodSpec.NodeSelector, fetchedPodSpec.NodeSelector) {
		return true
	}
	if desiredPodSpec.Affinity != nil &&
		!reflect.DeepEqual(desiredPodSpec.Affinity, fetchedPodSpec.Affinity) {
		return true
	}
	if desiredPodSpec.Tolerations != nil &&
		!reflect.DeepEqual(desiredPodSpec.Tolerations, fetchedPodSpec.Tolerations) {
		return true
	}

	if len(desiredPodSpec.Containers) != len(fetchedPodSpec.Containers) {
		return true
	}
	for i := range desiredPodSpec.Containers {
		if containerModified(&desiredPodSpec.Containers[i], &fetchedPodSpec.Containers[i]) {
			return true
		}
	}
	return false
}

func containerModified(desired, existing *corev1.Container) bool {
	if desired.Name != existing.Name ||
		desired.Image != existing.Image ||
		desired.ImagePullPolicy != existing.ImagePullPolicy {
		return true
	}

	if !slices.Equal(desired.Args, existing.Args) ||
		!reflect.DeepEqual(desired.Resources, existing.Resources) ||
		!reflect.DeepEqual(desired.SecurityContext, existing.SecurityContext) ||
		!reflect.DeepEqual(desired.VolumeMounts, existing.VolumeMounts) {
		return true
	}

	if !containerPortsMatch(desired.Ports, existing.Ports) {
		return true
	}

	if readinessProbeModified(desired.ReadinessProbe, existing.ReadinessProbe) {
		return true
	}

	return false
}

func containerPortsMatch(desired, existing []corev1.ContainerPort) bool {
	if len(desired) != len(existing) {
		return false
	}
	for _, dp := range desired {
		matched := false
		for _, ep := range existing {
			if dp.ContainerPort == ep.ContainerPort && dp.Name == ep.Name {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// readinessProbeModified compares the probe fields set in operand bindata.
// API-defaulted scalars such as TimeoutSeconds are ignored.
func readinessProbeModified(desired, existing *corev1.Probe) bool {
	if desired == nil && existing == nil {
		return false
	}
	if desired == nil || existing == nil {
		return true
	}
	if desired.InitialDelaySeconds != existing.InitialDelaySeconds ||
		desired.PeriodSeconds != existing.PeriodSeconds {
		return true
	}
	if desired.HTTPGet != nil && existing.HTTPGet != nil {
		if desired.HTTPGet.Path != existing.HTTPGet.Path ||
			desired.HTTPGet.Port != existing.HTTPGet.Port {
			return true
		}
	} else if desired.HTTPGet != existing.HTTPGet {
		return true
	}
	return false
}
