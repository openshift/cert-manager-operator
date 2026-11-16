package common

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ManagedMetadataModified compares SSA-managed labels and annotations, and the
// controller reference when desired supplies one. fieldOwner must match the
// field manager used to apply the object. Metadata belonging to others is kept.
func ManagedMetadataModified(desired, existing client.Object, fieldOwner string) bool {
	return ManagedMapModified(desired.GetLabels(), existing.GetLabels(), existing, fieldOwner, "f:metadata", "f:labels") ||
		ManagedMapModified(desired.GetAnnotations(), existing.GetAnnotations(), existing, fieldOwner, "f:metadata", "f:annotations") ||
		(metav1.GetControllerOf(desired) != nil && !equality.Semantic.DeepEqual(metav1.GetControllerOf(desired), metav1.GetControllerOf(existing)))
}

// ManagedMapModified detects changed desired values and removal of keys previously
// applied by fieldOwner. path identifies the map in metadata.managedFields, such
// as f:spec/f:template/f:metadata/f:labels. Extra keys owned by others are ignored.
func ManagedMapModified(desired, existing map[string]string, obj client.Object, fieldOwner string, path ...string) bool {
	for key, value := range desired {
		if actual, ok := existing[key]; !ok || actual != value {
			return true
		}
	}
	for _, entry := range obj.GetManagedFields() {
		if entry.Manager != fieldOwner || entry.Subresource != "" || entry.FieldsV1 == nil {
			continue
		}
		var fields map[string]interface{}
		if json.Unmarshal(entry.FieldsV1.GetRawBytes(), &fields) != nil {
			return true
		}
		for _, segment := range path {
			fields, _ = fields[segment].(map[string]interface{})
		}
		for key := range fields {
			if !strings.HasPrefix(key, "f:") {
				continue
			}
			key = strings.TrimPrefix(key, "f:")
			if _, wanted := desired[key]; !wanted {
				if _, present := existing[key]; present {
					return true
				}
			}
		}
	}
	return false
}

// DeploymentModified compares the fields shared by our SSA operand controllers:
// metadata, replicas, selectors, scheduling, and container configuration. It
// ignores status and API-defaulted pod/probe fields. Callers additionally compare
// operand-specific fields such as volumes, mounts, networking and pod security.
func DeploymentModified(desired, existing *appsv1.Deployment, fieldOwner string) bool {
	if ManagedMetadataModified(desired, existing, fieldOwner) ||
		!ptr.Equal(desired.Spec.Replicas, existing.Spec.Replicas) ||
		!equality.Semantic.DeepEqual(desired.Spec.Selector, existing.Spec.Selector) ||
		ManagedMapModified(desired.Spec.Template.Labels, existing.Spec.Template.Labels, existing, fieldOwner, "f:spec", "f:template", "f:metadata", "f:labels") ||
		ManagedMapModified(desired.Spec.Template.Annotations, existing.Spec.Template.Annotations, existing, fieldOwner, "f:spec", "f:template", "f:metadata", "f:annotations") {
		return true
	}

	a, b := &desired.Spec.Template.Spec, &existing.Spec.Template.Spec
	// Unlike metadata, nodeSelector is atomic: SSA owns and replaces the whole
	// map, so removed keys must be compared without per-key managedFields.
	if a.ServiceAccountName != b.ServiceAccountName ||
		!maps.Equal(a.NodeSelector, b.NodeSelector) ||
		!equality.Semantic.DeepEqual(a.Affinity, b.Affinity) ||
		!equality.Semantic.DeepEqual(a.Tolerations, b.Tolerations) ||
		len(a.Containers) != len(b.Containers) {
		return true
	}
	for i := range a.Containers {
		if containerModified(&a.Containers[i], &b.Containers[i]) {
			return true
		}
	}
	return false
}

func containerModified(desired, existing *corev1.Container) bool {
	return desired.Name != existing.Name || desired.Image != existing.Image || desired.ImagePullPolicy != existing.ImagePullPolicy ||
		!slices.Equal(desired.Args, existing.Args) ||
		!equality.Semantic.DeepEqual(desired.Resources, existing.Resources) ||
		!equality.Semantic.DeepEqual(desired.SecurityContext, existing.SecurityContext) ||
		!containerPortsMatch(desired.Ports, existing.Ports) ||
		readinessProbeModified(desired.ReadinessProbe, existing.ReadinessProbe)
}

// Container ports are an associative list; API defaulting of protocol to TCP and
// reordering do not constitute drift. An explicit different protocol does.
func containerPortsMatch(desired, existing []corev1.ContainerPort) bool {
	if len(desired) != len(existing) {
		return false
	}
	protocol := func(value corev1.Protocol) corev1.Protocol {
		if value == "" {
			return corev1.ProtocolTCP
		}
		return value
	}
	for _, dp := range desired {
		if !slices.ContainsFunc(existing, func(ep corev1.ContainerPort) bool {
			return dp.Name == ep.Name && dp.ContainerPort == ep.ContainerPort && protocol(dp.Protocol) == protocol(ep.Protocol)
		}) {
			return false
		}
	}
	return true
}

// Both operand manifests manage the HTTP readiness endpoint and polling cadence.
// Timeout, threshold, and HTTP scheme defaults are left to the API server.
func readinessProbeModified(desired, existing *corev1.Probe) bool {
	if desired == nil || existing == nil {
		return desired != existing
	}
	if desired.InitialDelaySeconds != existing.InitialDelaySeconds || desired.PeriodSeconds != existing.PeriodSeconds {
		return true
	}
	if desired.HTTPGet == nil || existing.HTTPGet == nil {
		return desired.HTTPGet != existing.HTTPGet
	}
	return desired.HTTPGet.Path != existing.HTTPGet.Path || desired.HTTPGet.Port != existing.HTTPGet.Port
}

// ServiceModified compares the fields managed by our SSA operand controllers,
// excluding server-allocated addresses and unrelated Service defaults.
func ServiceModified(desired, existing *corev1.Service, fieldOwner string) bool {
	return ManagedMetadataModified(desired, existing, fieldOwner) ||
		desired.Spec.Type != existing.Spec.Type ||
		!maps.Equal(desired.Spec.Selector, existing.Spec.Selector) ||
		!equality.Semantic.DeepEqual(desired.Spec.Ports, existing.Spec.Ports)
}

// ServiceAccountModified ignores generated token references and pull secrets.
func ServiceAccountModified(desired, existing *corev1.ServiceAccount, fieldOwner string) bool {
	return ManagedMetadataModified(desired, existing, fieldOwner) ||
		!ptr.Equal(desired.AutomountServiceAccountToken, existing.AutomountServiceAccountToken)
}

// ClusterRoleModified compares metadata and the rules managed by the operand.
func ClusterRoleModified(desired, existing *rbacv1.ClusterRole, fieldOwner string) bool {
	return ManagedMetadataModified(desired, existing, fieldOwner) ||
		!equality.Semantic.DeepEqual(desired.Rules, existing.Rules)
}

// ClusterRoleBindingModified compares metadata, the role reference and subjects.
func ClusterRoleBindingModified(desired, existing *rbacv1.ClusterRoleBinding, fieldOwner string) bool {
	return ManagedMetadataModified(desired, existing, fieldOwner) ||
		!equality.Semantic.DeepEqual(desired.RoleRef, existing.RoleRef) ||
		!equality.Semantic.DeepEqual(desired.Subjects, existing.Subjects)
}

// RoleModified compares metadata and the rules managed by the operand.
func RoleModified(desired, existing *rbacv1.Role, fieldOwner string) bool {
	return ManagedMetadataModified(desired, existing, fieldOwner) ||
		!equality.Semantic.DeepEqual(desired.Rules, existing.Rules)
}

// RoleBindingModified compares metadata, the role reference and subjects.
func RoleBindingModified(desired, existing *rbacv1.RoleBinding, fieldOwner string) bool {
	return ManagedMetadataModified(desired, existing, fieldOwner) ||
		!equality.Semantic.DeepEqual(desired.RoleRef, existing.RoleRef) ||
		!equality.Semantic.DeepEqual(desired.Subjects, existing.Subjects)
}

// ConfigMapModified compares metadata and data authored by the operand controller.
func ConfigMapModified(desired, existing *corev1.ConfigMap, fieldOwner string) bool {
	return ManagedMetadataModified(desired, existing, fieldOwner) || !maps.Equal(desired.Data, existing.Data)
}

// ValidatingWebhookConfigurationModified compares the fields managed by our SSA
// operand controllers. caBundle belongs to cainjector; matchPolicy and selectors
// are defaulted by the API server and omitted from these operand manifests.
func ValidatingWebhookConfigurationModified(desired, existing *admissionregistrationv1.ValidatingWebhookConfiguration, fieldOwner string) bool {
	if ManagedMetadataModified(desired, existing, fieldOwner) || len(desired.Webhooks) != len(existing.Webhooks) {
		return true
	}
	for i := range desired.Webhooks {
		if validatingWebhookModified(&desired.Webhooks[i], &existing.Webhooks[i]) {
			return true
		}
	}
	return false
}

func validatingWebhookModified(desired, existing *admissionregistrationv1.ValidatingWebhook) bool {
	if desired.Name != existing.Name || len(desired.Rules) != len(existing.Rules) ||
		!equality.Semantic.DeepEqual(desired.AdmissionReviewVersions, existing.AdmissionReviewVersions) ||
		!ptr.Equal(desired.FailurePolicy, existing.FailurePolicy) || !ptr.Equal(desired.SideEffects, existing.SideEffects) ||
		!ptr.Equal(desired.TimeoutSeconds, existing.TimeoutSeconds) || !ptr.Equal(desired.ClientConfig.URL, existing.ClientConfig.URL) {
		return true
	}
	for i := range desired.Rules {
		a, b := desired.Rules[i].DeepCopy(), existing.Rules[i].DeepCopy()
		// A missing scope defaults to "*", not to an arbitrary existing value.
		a.Scope = ptr.To(ptr.Deref(a.Scope, admissionregistrationv1.AllScopes))
		b.Scope = ptr.To(ptr.Deref(b.Scope, admissionregistrationv1.AllScopes))
		if !equality.Semantic.DeepEqual(a, b) {
			return true
		}
	}
	// Service.port defaults to 443. Compare without mutating either object.
	a, b := desired.ClientConfig.Service, existing.ClientConfig.Service
	if a == nil || b == nil {
		return a != b
	}
	return a.Name != b.Name || a.Namespace != b.Namespace || !ptr.Equal(a.Path, b.Path) ||
		ptr.Deref(a.Port, 443) != ptr.Deref(b.Port, 443)
}
