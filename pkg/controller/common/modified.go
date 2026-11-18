package common

import (
	"fmt"
	"maps"
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
)

// istioCSRFieldOwner is empty because istio-csr still updates objects with
// Update, so managedFields do not record this controller as the field owner.
// Desired-key drift is still detected. Cleared keys that were never recorded
// are covered by the full-equality checks in DeploymentSpecModified.
const istioCSRFieldOwner = ""

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

// HasObjectChanged reports whether desired and fetched differ in managed
// labels or annotations, or in the spec fields istio-csr reconciles.
// Types that pkg/controller/common/resource_diff.go already compares are
// delegated there. desired and fetched must be the same concrete type.
func HasObjectChanged(desired, fetched client.Object) bool {
	if reflect.TypeOf(desired) != reflect.TypeOf(fetched) {
		panic("both objects to be compared must be of same type")
	}

	switch desiredObj := desired.(type) {
	case *certmanagerv1.Certificate:
		fetchedCert, ok := fetched.(*certmanagerv1.Certificate)
		if !ok {
			panic("failed to convert fetched to *certmanagerv1.Certificate")
		}
		return CertificateSpecModified(desiredObj, fetchedCert) || ManagedMetadataModified(desired, fetched, istioCSRFieldOwner)
	case *certmanagerv1.Issuer:
		fetchedIssuer, ok := fetched.(*certmanagerv1.Issuer)
		if !ok {
			panic("failed to convert fetched to *certmanagerv1.Issuer")
		}
		return IssuerSpecModified(desiredObj, fetchedIssuer) || ManagedMetadataModified(desired, fetched, istioCSRFieldOwner)
	case *rbacv1.ClusterRole:
		fetchedClusterRole, ok := fetched.(*rbacv1.ClusterRole)
		if !ok {
			panic("failed to convert fetched to *rbacv1.ClusterRole")
		}
		return ClusterRoleModified(desiredObj, fetchedClusterRole, istioCSRFieldOwner)
	case *rbacv1.ClusterRoleBinding:
		fetchedClusterRoleBinding, ok := fetched.(*rbacv1.ClusterRoleBinding)
		if !ok {
			panic("failed to convert fetched to *rbacv1.ClusterRoleBinding")
		}
		return ClusterRoleBindingModified(desiredObj, fetchedClusterRoleBinding, istioCSRFieldOwner)
	case *appsv1.Deployment:
		fetchedDeployment, ok := fetched.(*appsv1.Deployment)
		if !ok {
			panic("failed to convert fetched to *appsv1.Deployment")
		}
		return DeploymentSpecModified(desiredObj, fetchedDeployment)
	case *rbacv1.Role:
		fetchedRole, ok := fetched.(*rbacv1.Role)
		if !ok {
			panic("failed to convert fetched to *rbacv1.Role")
		}
		return RoleModified(desiredObj, fetchedRole, istioCSRFieldOwner)
	case *rbacv1.RoleBinding:
		fetchedRoleBinding, ok := fetched.(*rbacv1.RoleBinding)
		if !ok {
			panic("failed to convert fetched to *rbacv1.RoleBinding")
		}
		return RoleBindingModified(desiredObj, fetchedRoleBinding, istioCSRFieldOwner)
	case *corev1.Service:
		fetchedService, ok := fetched.(*corev1.Service)
		if !ok {
			panic("failed to convert fetched to *corev1.Service")
		}
		return ServiceModified(desiredObj, fetchedService, istioCSRFieldOwner)
	case *corev1.ConfigMap:
		fetchedConfigMap, ok := fetched.(*corev1.ConfigMap)
		if !ok {
			panic("failed to convert fetched to *corev1.ConfigMap")
		}
		return ConfigMapModified(desiredObj, fetchedConfigMap, istioCSRFieldOwner)
	case *networkingv1.NetworkPolicy:
		fetchedNetworkPolicy, ok := fetched.(*networkingv1.NetworkPolicy)
		if !ok {
			panic("failed to convert fetched to *networkingv1.NetworkPolicy")
		}
		return NetworkPolicySpecModified(desiredObj, fetchedNetworkPolicy) || ManagedMetadataModified(desired, fetched, istioCSRFieldOwner)
	case *corev1.ServiceAccount:
		fetchedServiceAccount, ok := fetched.(*corev1.ServiceAccount)
		if !ok {
			panic("failed to convert fetched to *corev1.ServiceAccount")
		}
		return ServiceAccountModified(desiredObj, fetchedServiceAccount, istioCSRFieldOwner)
	default:
		panic(fmt.Sprintf("unsupported object type: %T", desired))
	}
}

// DeploymentSpecModified compares istio-csr Deployment drift.
// Shared fields go through DeploymentModified. Revision history, volumes, and
// volume mounts are istio-csr specific. Template labels and node selectors use
// full map equality so a cleared key is drift even though istio-csr does not
// record a field owner.
func DeploymentSpecModified(desired, fetched *appsv1.Deployment) bool {
	if DeploymentModified(desired, fetched, istioCSRFieldOwner) {
		return true
	}
	if !ptr.Equal(desired.Spec.RevisionHistoryLimit, fetched.Spec.RevisionHistoryLimit) {
		return true
	}
	if !maps.Equal(desired.Spec.Template.Labels, fetched.Spec.Template.Labels) ||
		!maps.Equal(desired.Spec.Template.Spec.NodeSelector, fetched.Spec.Template.Spec.NodeSelector) {
		return true
	}
	if !reflect.DeepEqual(desired.Spec.Template.Spec.Volumes, fetched.Spec.Template.Spec.Volumes) {
		return true
	}
	desiredContainers := desired.Spec.Template.Spec.Containers
	fetchedContainers := fetched.Spec.Template.Spec.Containers
	if len(desiredContainers) != len(fetchedContainers) {
		return true
	}
	for i := range desiredContainers {
		if !reflect.DeepEqual(desiredContainers[i].VolumeMounts, fetchedContainers[i].VolumeMounts) {
			return true
		}
	}
	return false
}
