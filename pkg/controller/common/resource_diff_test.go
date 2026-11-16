package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

const testFieldOwner = "test-controller"

func TestManagedMapModified(t *testing.T) {
	ownedFields := func(raw string) []metav1.ManagedFieldsEntry {
		return []metav1.ManagedFieldsEntry{{Manager: testFieldOwner, FieldsV1: &metav1.FieldsV1{Raw: []byte(raw)}}}
	}
	for _, tc := range []struct {
		name          string
		desired       map[string]string
		existing      map[string]string
		managedFields []metav1.ManagedFieldsEntry
		want          bool
	}{
		{
			name:     "missing key with an empty desired value",
			desired:  map[string]string{"team": ""},
			existing: map[string]string{},
			want:     true,
		},
		{
			name:     "desired key missing from existing",
			desired:  map[string]string{"team": "security"},
			existing: map[string]string{},
			want:     true,
		},
		{
			name:     "desired key present with a different value",
			desired:  map[string]string{"team": "security"},
			existing: map[string]string{"team": "other"},
			want:     true,
		},
		{
			name:     "a foreign key with no managed fields is not drift",
			desired:  map[string]string{"team": "security"},
			existing: map[string]string{"team": "security", "foreign": "keep"},
			want:     false,
		},
		{
			name:          "a key we previously owned and dropped is removed",
			desired:       map[string]string{},
			existing:      map[string]string{"removed": "old"},
			managedFields: ownedFields(`{"f:metadata":{"f:labels":{"f:removed":{}}}}`),
			want:          true,
		},
		{
			name:          "an owned key still desired is not drift",
			desired:       map[string]string{"team": "security"},
			existing:      map[string]string{"team": "security"},
			managedFields: ownedFields(`{"f:metadata":{"f:labels":{"f:team":{}}}}`),
			want:          false,
		},
		{
			name:          "a previously owned key already removed is not drift",
			managedFields: ownedFields(`{"f:metadata":{"f:labels":{"f:removed":{}}}}`),
			want:          false,
		},
		{
			name:          "ownership of another map does not claim labels",
			existing:      map[string]string{"removed": "keep"},
			managedFields: ownedFields(`{"f:metadata":{"f:annotations":{"f:removed":{}}}}`),
			want:          false,
		},
		{
			name:     "finds owned keys after another manager's entry",
			existing: map[string]string{"removed": "old"},
			managedFields: append([]metav1.ManagedFieldsEntry{{Manager: "other-controller"}},
				ownedFields(`{"f:metadata":{"f:labels":{"f:removed":{}}}}`)...),
			want: true,
		},
		{
			name:          "a managed fields entry from another manager is ignored",
			desired:       map[string]string{},
			existing:      map[string]string{"removed": "old"},
			managedFields: []metav1.ManagedFieldsEntry{{Manager: "other-controller", FieldsV1: &metav1.FieldsV1{Raw: []byte(`{"f:metadata":{"f:labels":{"f:removed":{}}}}`)}}},
			want:          false,
		},
		{
			name:          "a managed fields entry for a subresource is ignored",
			desired:       map[string]string{},
			existing:      map[string]string{"removed": "old"},
			managedFields: []metav1.ManagedFieldsEntry{{Manager: testFieldOwner, Subresource: "status", FieldsV1: &metav1.FieldsV1{Raw: []byte(`{"f:metadata":{"f:labels":{"f:removed":{}}}}`)}}},
			want:          false,
		},
		{
			name:          "a managed fields entry without FieldsV1 is ignored",
			desired:       map[string]string{},
			existing:      map[string]string{"removed": "old"},
			managedFields: []metav1.ManagedFieldsEntry{{Manager: testFieldOwner}},
			want:          false,
		},
		{
			name:          "unparsable FieldsV1 is treated as drift",
			desired:       map[string]string{},
			existing:      map[string]string{"removed": "old"},
			managedFields: ownedFields(`not-json`),
			want:          true,
		},
		{
			// Real FieldsV1 payloads include a "." marker key alongside "f:" keys
			// (e.g. to record ownership of the map itself); it must not be
			// mistaken for an owned field. Only the "." key is present here so
			// the assertion does not depend on Go's random map iteration order.
			name:          "a non f: prefixed field path segment (e.g. '.') is skipped",
			desired:       map[string]string{},
			existing:      map[string]string{},
			managedFields: ownedFields(`{"f:metadata":{"f:labels":{".":{}}}}`),
			want:          false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			existing := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{ManagedFields: tc.managedFields}}
			assert.Equal(t, tc.want, ManagedMapModified(tc.desired, tc.existing, existing, testFieldOwner, "f:metadata", "f:labels"))
		})
	}
}

func TestManagedMetadataControllerReference(t *testing.T) {
	desired := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{OwnerReferences: []metav1.OwnerReference{{
		APIVersion: "operator.example/v1", Kind: "Operand", Name: "cluster", UID: "owner", Controller: ptr.To(true),
	}}}}
	existing := desired.DeepCopy()
	assert.False(t, ManagedMetadataModified(desired, existing, testFieldOwner))

	existing.OwnerReferences[0].UID = "other-owner"
	assert.True(t, ManagedMetadataModified(desired, existing, testFieldOwner))

	existing.OwnerReferences = nil
	assert.True(t, ManagedMetadataModified(desired, existing, testFieldOwner))

	assert.False(t, ManagedMetadataModified(&corev1.Secret{}, desired, testFieldOwner),
		"controllers that do not set an owner reference must not claim another controller's reference")
}

func TestManagedMetadataRemoval(t *testing.T) {
	desired := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "operand"}}}
	existing := desired.DeepCopy()
	existing.Labels["removed"] = "old"
	existing.Labels["foreign"] = "keep"
	existing.ManagedFields = []metav1.ManagedFieldsEntry{{
		Manager:  testFieldOwner,
		FieldsV1: &metav1.FieldsV1{Raw: []byte(`{"f:metadata":{"f:labels":{"f:removed":{}}}}`)},
	}}
	assert.True(t, ManagedMetadataModified(desired, existing, testFieldOwner))
	delete(existing.Labels, "removed")
	assert.False(t, ManagedMetadataModified(desired, existing, testFieldOwner), "foreign metadata is not removed")
}

func TestDeploymentModified(t *testing.T) {
	desired := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "operand"}},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "operand"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "operand"}},
				Spec: corev1.PodSpec{
					ServiceAccountName: "operand",
					NodeSelector:       map[string]string{"kubernetes.io/os": "linux"},
					Containers: []corev1.Container{{
						Name: "operand", Image: "example.test/operand:v1", Args: []string{"--log-level=1"},
						Ports: []corev1.ContainerPort{{Name: "webhook", ContainerPort: 6443}, {Name: "metrics", ContainerPort: 9402}},
						ReadinessProbe: &corev1.Probe{
							ProbeHandler:  corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/readyz", Port: intstr.FromInt(6060)}},
							PeriodSeconds: 7,
						},
					}},
				},
			},
		},
	}

	for _, tc := range []struct {
		name   string
		mutate func(*appsv1.Deployment)
		want   bool
	}{
		{"unchanged", func(*appsv1.Deployment) {}, false},
		{"API defaults and rollout status", func(d *appsv1.Deployment) {
			d.ResourceVersion = "2"
			d.Status.AvailableReplicas = 1
			d.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyAlways
			c := &d.Spec.Template.Spec.Containers[0]
			for i := range c.Ports {
				c.Ports[i].Protocol = corev1.ProtocolTCP
			}
			c.ReadinessProbe.TimeoutSeconds = 1
			c.ReadinessProbe.SuccessThreshold = 1
			c.ReadinessProbe.FailureThreshold = 3
			c.ReadinessProbe.HTTPGet.Scheme = corev1.URISchemeHTTP
		}, false},
		{"foreign metadata", func(d *appsv1.Deployment) {
			d.Labels["external"] = "keep"
			d.Spec.Template.Labels["external"] = "keep"
			d.Spec.Template.Annotations = map[string]string{"external": "keep"}
		}, false},
		{"removed node selector key still present", func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.NodeSelector["e2e.example/no-such-node"] = "old"
		}, true},
		{"node selector key missing", func(d *appsv1.Deployment) {
			delete(d.Spec.Template.Spec.NodeSelector, "kubernetes.io/os")
		}, true},
		{"node selector value changed", func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.NodeSelector["kubernetes.io/os"] = "windows"
		}, true},
		{"port order", func(d *appsv1.Deployment) {
			p := d.Spec.Template.Spec.Containers[0].Ports
			p[0], p[1] = p[1], p[0]
		}, false},
		{"port removed", func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.Containers[0].Ports = d.Spec.Template.Spec.Containers[0].Ports[:1]
		}, true},
		{"port protocol drift", func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.Containers[0].Ports[0].Protocol = corev1.ProtocolUDP
		}, true},
		{"configured argument drift", func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.Containers[0].Args = []string{"--log-level=5"}
		}, true},
		{"readiness endpoint drift", func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.Containers[0].ReadinessProbe.HTTPGet.Path = "/wrong"
		}, true},
		{"readiness probe removed", func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.Containers[0].ReadinessProbe = nil
		}, true},
		{"readiness polling period changed", func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.Containers[0].ReadinessProbe.PeriodSeconds++
		}, true},
		{"readiness handler replaced", func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.Containers[0].ReadinessProbe.ProbeHandler = corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt(6060)},
			}
		}, true},
		{"removed affinity still present", func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.Affinity = &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{}}
		}, true},
		{"removed toleration still present", func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.Tolerations = []corev1.Toleration{{Key: "dedicated", Operator: corev1.TolerationOpExists}}
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			existing := desired.DeepCopy()
			tc.mutate(existing)
			before := existing.DeepCopy()
			assert.Equal(t, tc.want, DeploymentModified(desired, existing, testFieldOwner))
			assert.Equal(t, before, existing, "comparison must not mutate cached objects")
		})
	}

	for _, tc := range []struct {
		name        string
		mutate      func(*appsv1.Deployment)
		ownedFields string
	}{
		{
			name:        "pod labels",
			mutate:      func(d *appsv1.Deployment) { d.Spec.Template.Labels["removed"] = "old" },
			ownedFields: `{"f:spec":{"f:template":{"f:metadata":{"f:labels":{"f:removed":{}}}}}}`,
		},
		{
			name:        "pod annotations",
			mutate:      func(d *appsv1.Deployment) { d.Spec.Template.Annotations = map[string]string{"removed": "old"} },
			ownedFields: `{"f:spec":{"f:template":{"f:metadata":{"f:annotations":{"f:removed":{}}}}}}`,
		},
	} {
		t.Run("field ownership for "+tc.name, func(t *testing.T) {
			existing := desired.DeepCopy()
			tc.mutate(existing)
			existing.ManagedFields = []metav1.ManagedFieldsEntry{{
				Manager:  "operand-a",
				FieldsV1: &metav1.FieldsV1{Raw: []byte(tc.ownedFields)},
			}}
			assert.True(t, DeploymentModified(desired, existing, "operand-a"), "remove a key previously applied by this controller")
			assert.False(t, DeploymentModified(desired, existing, "operand-b"), "preserve a key belonging to another controller")
		})
	}

	t.Run("node selector ownership covers the whole map", func(t *testing.T) {
		existing := desired.DeepCopy()
		existing.Spec.Template.Spec.NodeSelector["removed"] = "old"
		existing.ManagedFields = []metav1.ManagedFieldsEntry{{
			Manager:  testFieldOwner,
			FieldsV1: &metav1.FieldsV1{Raw: []byte(`{"f:spec":{"f:template":{"f:spec":{"f:nodeSelector":{}}}}}`)},
		}}
		assert.True(t, DeploymentModified(desired, existing, testFieldOwner))
		assert.True(t, DeploymentModified(desired, existing, "another-controller"), "SSA cannot preserve individually owned keys in an atomic map")
	})
}

func TestServiceModified(t *testing.T) {
	desired := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "operand"}},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: map[string]string{"app": "operand"},
			Ports: []corev1.ServicePort{{
				Name: "webhook", Port: 443, TargetPort: intstr.FromInt(6443), Protocol: corev1.ProtocolTCP,
			}},
		},
	}
	for _, tc := range []struct {
		name   string
		mutate func(*corev1.Service)
		want   bool
	}{
		{"unchanged", func(*corev1.Service) {}, false},
		{"managed label changed", func(s *corev1.Service) { s.Labels["app"] = "other" }, true},
		{"foreign metadata", func(s *corev1.Service) { s.Annotations = map[string]string{"external": "keep"} }, false},
		{"allocated addresses and API defaults", func(s *corev1.Service) {
			s.Spec.ClusterIP = "10.0.0.1"
			s.Spec.ClusterIPs = []string{"10.0.0.1"}
			s.Spec.IPFamilies = []corev1.IPFamily{corev1.IPv4Protocol}
			s.Spec.IPFamilyPolicy = ptr.To(corev1.IPFamilyPolicySingleStack)
			s.Spec.SessionAffinity = corev1.ServiceAffinityNone
			s.Spec.InternalTrafficPolicy = ptr.To(corev1.ServiceInternalTrafficPolicyCluster)
		}, false},
		{"type changed", func(s *corev1.Service) { s.Spec.Type = corev1.ServiceTypeLoadBalancer }, true},
		{"selector changed", func(s *corev1.Service) { s.Spec.Selector["app"] = "other" }, true},
		{"selector key added", func(s *corev1.Service) { s.Spec.Selector["extra"] = "value" }, true},
		{"selector removed", func(s *corev1.Service) { s.Spec.Selector = nil }, true},
		{"port name changed", func(s *corev1.Service) { s.Spec.Ports[0].Name = "other" }, true},
		{"port number changed", func(s *corev1.Service) { s.Spec.Ports[0].Port = 8443 }, true},
		{"target port changed", func(s *corev1.Service) { s.Spec.Ports[0].TargetPort = intstr.FromInt(9443) }, true},
		{"port protocol changed", func(s *corev1.Service) { s.Spec.Ports[0].Protocol = corev1.ProtocolUDP }, true},
		{"port added", func(s *corev1.Service) {
			s.Spec.Ports = append(s.Spec.Ports, corev1.ServicePort{Name: "metrics", Port: 9402, Protocol: corev1.ProtocolTCP})
		}, true},
		{"ports removed", func(s *corev1.Service) { s.Spec.Ports = nil }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			existing := desired.DeepCopy()
			tc.mutate(existing)
			assert.Equal(t, tc.want, ServiceModified(desired, existing, testFieldOwner))
		})
	}
}

func TestServiceAccountModified(t *testing.T) {
	desired := &corev1.ServiceAccount{
		ObjectMeta:                   metav1.ObjectMeta{Labels: map[string]string{"app": "operand"}},
		AutomountServiceAccountToken: ptr.To(false),
	}
	for _, tc := range []struct {
		name   string
		mutate func(*corev1.ServiceAccount)
		want   bool
	}{
		{"unchanged", func(*corev1.ServiceAccount) {}, false},
		{"managed label removed", func(sa *corev1.ServiceAccount) { delete(sa.Labels, "app") }, true},
		{"foreign metadata", func(sa *corev1.ServiceAccount) { sa.Labels["external"] = "keep" }, false},
		{"generated token references", func(sa *corev1.ServiceAccount) {
			sa.Secrets = []corev1.ObjectReference{{Name: "generated-token"}}
		}, false},
		{"injected image pull secrets", func(sa *corev1.ServiceAccount) {
			sa.ImagePullSecrets = []corev1.LocalObjectReference{{Name: "cluster-pull-secret"}}
		}, false},
		{"token automount enabled", func(sa *corev1.ServiceAccount) { sa.AutomountServiceAccountToken = ptr.To(true) }, true},
		{"explicit token automount setting removed", func(sa *corev1.ServiceAccount) { sa.AutomountServiceAccountToken = nil }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			existing := desired.DeepCopy()
			tc.mutate(existing)
			assert.Equal(t, tc.want, ServiceAccountModified(desired, existing, testFieldOwner))
		})
	}
}

func TestClusterRoleModified(t *testing.T) {
	desired := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "operand"}},
		Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{"cert-manager.io"}, Resources: []string{"signers"},
			ResourceNames: []string{"issuers.cert-manager.io/*"}, Verbs: []string{"approve"},
		}},
	}
	for _, tc := range []struct {
		name   string
		mutate func(*rbacv1.ClusterRole)
		want   bool
	}{
		{"unchanged", func(*rbacv1.ClusterRole) {}, false},
		{"managed label changed", func(r *rbacv1.ClusterRole) { r.Labels["app"] = "other" }, true},
		{"foreign metadata", func(r *rbacv1.ClusterRole) { r.Labels["external"] = "keep" }, false},
		{"API group changed", func(r *rbacv1.ClusterRole) { r.Rules[0].APIGroups = []string{"other.example"} }, true},
		{"resource changed", func(r *rbacv1.ClusterRole) { r.Rules[0].Resources = []string{"certificaterequests"} }, true},
		{"signer scope changed", func(r *rbacv1.ClusterRole) { r.Rules[0].ResourceNames = []string{"external.example/*"} }, true},
		{"signer scope restriction removed", func(r *rbacv1.ClusterRole) { r.Rules[0].ResourceNames = nil }, true},
		{"permission changed", func(r *rbacv1.ClusterRole) { r.Rules[0].Verbs = []string{"get"} }, true},
		{"rule added", func(r *rbacv1.ClusterRole) {
			r.Rules = append(r.Rules, rbacv1.PolicyRule{APIGroups: []string{""}, Resources: []string{"secrets"}, Verbs: []string{"get"}})
		}, true},
		{"rules removed", func(r *rbacv1.ClusterRole) { r.Rules = nil }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			existing := desired.DeepCopy()
			tc.mutate(existing)
			assert.Equal(t, tc.want, ClusterRoleModified(desired, existing, testFieldOwner))
		})
	}
}

func TestClusterRoleBindingModified(t *testing.T) {
	desired := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "operand"}},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "operand"},
		Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "operand", Namespace: "cert-manager"}},
	}
	for _, tc := range []struct {
		name   string
		mutate func(*rbacv1.ClusterRoleBinding)
		want   bool
	}{
		{"unchanged", func(*rbacv1.ClusterRoleBinding) {}, false},
		{"managed label changed", func(b *rbacv1.ClusterRoleBinding) { b.Labels["app"] = "other" }, true},
		{"foreign metadata", func(b *rbacv1.ClusterRoleBinding) { b.Labels["external"] = "keep" }, false},
		{"role reference changed", func(b *rbacv1.ClusterRoleBinding) { b.RoleRef.Name = "other-role" }, true},
		{"subject name changed", func(b *rbacv1.ClusterRoleBinding) { b.Subjects[0].Name = "other-account" }, true},
		{"subject namespace changed", func(b *rbacv1.ClusterRoleBinding) { b.Subjects[0].Namespace = "other-namespace" }, true},
		{"subject added", func(b *rbacv1.ClusterRoleBinding) {
			b.Subjects = append(b.Subjects, rbacv1.Subject{Kind: "ServiceAccount", Name: "extra", Namespace: "cert-manager"})
		}, true},
		{"subjects removed", func(b *rbacv1.ClusterRoleBinding) { b.Subjects = nil }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			existing := desired.DeepCopy()
			tc.mutate(existing)
			assert.Equal(t, tc.want, ClusterRoleBindingModified(desired, existing, testFieldOwner))
		})
	}
}

func TestRoleModified(t *testing.T) {
	desired := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Namespace: "cert-manager", Labels: map[string]string{"app": "operand"}},
		Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{""}, Resources: []string{"secrets"}, ResourceNames: []string{"operand-tls"}, Verbs: []string{"get"},
		}},
	}
	for _, tc := range []struct {
		name   string
		mutate func(*rbacv1.Role)
		want   bool
	}{
		{"unchanged", func(*rbacv1.Role) {}, false},
		{"managed label changed", func(r *rbacv1.Role) { r.Labels["app"] = "other" }, true},
		{"foreign metadata", func(r *rbacv1.Role) { r.Labels["external"] = "keep" }, false},
		{"resource name changed", func(r *rbacv1.Role) { r.Rules[0].ResourceNames = []string{"other-secret"} }, true},
		{"permission added", func(r *rbacv1.Role) { r.Rules[0].Verbs = append(r.Rules[0].Verbs, "update") }, true},
		{"rule added", func(r *rbacv1.Role) {
			r.Rules = append(r.Rules, rbacv1.PolicyRule{APIGroups: []string{""}, Resources: []string{"configmaps"}, Verbs: []string{"get"}})
		}, true},
		{"rules removed", func(r *rbacv1.Role) { r.Rules = nil }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			existing := desired.DeepCopy()
			tc.mutate(existing)
			assert.Equal(t, tc.want, RoleModified(desired, existing, testFieldOwner))
		})
	}
}

func TestRoleBindingModified(t *testing.T) {
	desired := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Namespace: "cert-manager", Labels: map[string]string{"app": "operand"}},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: "operand"},
		Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "operand", Namespace: "cert-manager"}},
	}
	for _, tc := range []struct {
		name   string
		mutate func(*rbacv1.RoleBinding)
		want   bool
	}{
		{"unchanged", func(*rbacv1.RoleBinding) {}, false},
		{"managed label changed", func(b *rbacv1.RoleBinding) { b.Labels["app"] = "other" }, true},
		{"foreign metadata", func(b *rbacv1.RoleBinding) { b.Labels["external"] = "keep" }, false},
		{"role reference changed", func(b *rbacv1.RoleBinding) { b.RoleRef.Name = "other-role" }, true},
		{"reference to a ClusterRole", func(b *rbacv1.RoleBinding) { b.RoleRef.Kind = "ClusterRole" }, true},
		{"subject name changed", func(b *rbacv1.RoleBinding) { b.Subjects[0].Name = "other-account" }, true},
		{"subject namespace changed", func(b *rbacv1.RoleBinding) { b.Subjects[0].Namespace = "other-namespace" }, true},
		{"subject added", func(b *rbacv1.RoleBinding) {
			b.Subjects = append(b.Subjects, rbacv1.Subject{Kind: "ServiceAccount", Name: "extra", Namespace: "cert-manager"})
		}, true},
		{"subjects removed", func(b *rbacv1.RoleBinding) { b.Subjects = nil }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			existing := desired.DeepCopy()
			tc.mutate(existing)
			assert.Equal(t, tc.want, RoleBindingModified(desired, existing, testFieldOwner))
		})
	}
}

func TestConfigMapModified(t *testing.T) {
	desired := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "operand"}},
		Data:       map[string]string{"bundle.pem": "original bundle"},
	}
	for _, tc := range []struct {
		name   string
		mutate func(*corev1.ConfigMap)
		want   bool
	}{
		{"unchanged", func(*corev1.ConfigMap) {}, false},
		{"managed label changed", func(cm *corev1.ConfigMap) { cm.Labels["app"] = "other" }, true},
		{"foreign metadata", func(cm *corev1.ConfigMap) { cm.Labels["external"] = "keep" }, false},
		{"data changed", func(cm *corev1.ConfigMap) { cm.Data["bundle.pem"] = "changed bundle" }, true},
		{"data key added", func(cm *corev1.ConfigMap) { cm.Data["extra.pem"] = "extra bundle" }, true},
		{"data key removed", func(cm *corev1.ConfigMap) { delete(cm.Data, "bundle.pem") }, true},
		{"data cleared", func(cm *corev1.ConfigMap) { cm.Data = nil }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			existing := desired.DeepCopy()
			tc.mutate(existing)
			assert.Equal(t, tc.want, ConfigMapModified(desired, existing, testFieldOwner))
		})
	}
}

func TestValidatingWebhookConfigurationModified(t *testing.T) {
	desired := &admissionregistrationv1.ValidatingWebhookConfiguration{
		Webhooks: []admissionregistrationv1.ValidatingWebhook{{
			Name: "operand.example.com",
			Rules: []admissionregistrationv1.RuleWithOperations{{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
				Rule:       admissionregistrationv1.Rule{APIGroups: []string{"example.com"}, APIVersions: []string{"v1"}, Resources: []string{"policies"}},
			}},
			ClientConfig: admissionregistrationv1.WebhookClientConfig{Service: &admissionregistrationv1.ServiceReference{
				Name: "operand", Namespace: "operand", Path: ptr.To("/validate"),
			}},
		}},
	}
	for _, tc := range []struct {
		name   string
		mutate func(*admissionregistrationv1.ValidatingWebhook)
		want   bool
	}{
		{"unchanged", func(*admissionregistrationv1.ValidatingWebhook) {}, false},
		{"defaults and injected CA", func(w *admissionregistrationv1.ValidatingWebhook) {
			w.Rules[0].Scope = ptr.To(admissionregistrationv1.AllScopes)
			w.ClientConfig.Service.Port = ptr.To(int32(443))
			w.ClientConfig.CABundle = []byte("injected by cainjector")
			w.MatchPolicy = ptr.To(admissionregistrationv1.Equivalent)
			w.NamespaceSelector = &metav1.LabelSelector{}
			w.ObjectSelector = &metav1.LabelSelector{}
		}, false},
		{"non-default scope", func(w *admissionregistrationv1.ValidatingWebhook) {
			w.Rules[0].Scope = ptr.To(admissionregistrationv1.NamespacedScope)
		}, true},
		{"non-default service port", func(w *admissionregistrationv1.ValidatingWebhook) {
			w.ClientConfig.Service.Port = ptr.To(int32(8443))
		}, true},
		{"unexpected URL", func(w *admissionregistrationv1.ValidatingWebhook) {
			w.ClientConfig.URL = ptr.To("https://other.example/validate")
		}, true},
		{"service reference removed", func(w *admissionregistrationv1.ValidatingWebhook) {
			w.ClientConfig.Service = nil
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			existing := desired.DeepCopy()
			tc.mutate(&existing.Webhooks[0])
			before := existing.DeepCopy()
			assert.Equal(t, tc.want, ValidatingWebhookConfigurationModified(desired, existing, testFieldOwner))
			assert.Equal(t, before, existing, "comparison must not mutate cached objects")
		})
	}

	t.Run("webhook removed", func(t *testing.T) {
		existing := desired.DeepCopy()
		existing.Webhooks = nil
		assert.True(t, ValidatingWebhookConfigurationModified(desired, existing, testFieldOwner))
	})
}
