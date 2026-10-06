package common

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanagermetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
)

func TestManagedLabelsModified(t *testing.T) {
	tests := []struct {
		name     string
		desired  map[string]string
		existing map[string]string
		want     bool
	}{
		{
			name:     "identical labels returns not modified",
			desired:  map[string]string{"a": "1"},
			existing: map[string]string{"a": "1"},
			want:     false,
		},
		{
			name:     "different value for same key returns modified",
			desired:  map[string]string{"a": "1"},
			existing: map[string]string{"a": "2"},
			want:     true,
		},
		{
			name:     "existing has extra labels beyond desired still not modified",
			desired:  map[string]string{"a": "1"},
			existing: map[string]string{"a": "1", "b": "2"},
			want:     false,
		},
		{
			name:     "desired label missing on existing returns modified",
			desired:  map[string]string{"a": "1"},
			existing: map[string]string{},
			want:     true,
		},
		{
			name:     "nil desired labels returns not modified",
			desired:  nil,
			existing: map[string]string{"a": "1"},
			want:     false,
		},
		{
			name:     "both nil labels returns not modified",
			desired:  nil,
			existing: nil,
			want:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			desired := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Labels: tt.desired}}
			existing := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Labels: tt.existing}}
			assert.Equal(t, tt.want, ManagedLabelsModified(desired, existing))
		})
	}
}

func TestManagedAnnotationsModified(t *testing.T) {
	tests := []struct {
		name     string
		desired  map[string]string
		existing map[string]string
		want     bool
	}{
		{
			name:     "identical annotations",
			desired:  map[string]string{"a": "1"},
			existing: map[string]string{"a": "1"},
			want:     false,
		},
		{
			name:     "different annotation value",
			desired:  map[string]string{"a": "1"},
			existing: map[string]string{"a": "2"},
			want:     true,
		},
		{
			name:     "extra annotation on existing is ignored",
			desired:  map[string]string{"a": "1"},
			existing: map[string]string{"a": "1", "b": "2"},
			want:     false,
		},
		{
			name:     "missing desired annotation",
			desired:  map[string]string{"a": "1"},
			existing: nil,
			want:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			desired := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Annotations: tt.desired}}
			existing := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Annotations: tt.existing}}
			assert.Equal(t, tt.want, ManagedAnnotationsModified(desired, existing))
		})
	}
}

func TestManagedMetadataModified(t *testing.T) {
	tests := []struct {
		name     string
		desired  client.Object
		existing client.Object
		want     bool
	}{
		{
			name: "labels and annotations match",
			desired: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
				Labels:      map[string]string{"app": "trust-manager"},
				Annotations: map[string]string{"note": "a"},
			}},
			existing: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
				Labels:      map[string]string{"app": "trust-manager", "extra": "ok"},
				Annotations: map[string]string{"note": "a"},
			}},
			want: false,
		},
		{
			name: "annotation drift",
			desired: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{"note": "a"},
			}},
			existing: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{"note": "b"},
			}},
			want: true,
		},
		{
			name: "label drift",
			desired: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{"app": "trust-manager"},
			}},
			existing: &corev1.Secret{},
			want:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ManagedMetadataModified(tt.desired, tt.existing))
		})
	}
}

func TestCertificateManagedFieldsModified(t *testing.T) {
	issuerRef := certmanagermetav1.ObjectReference{Name: "selfsigned", Kind: "Issuer", Group: "cert-manager.io"}
	base := func() *certmanagerv1.Certificate {
		return &certmanagerv1.Certificate{
			Spec: certmanagerv1.CertificateSpec{
				CommonName:           "trust-manager.cert-manager.svc",
				DNSNames:             []string{"trust-manager.cert-manager.svc"},
				SecretName:           "trust-manager-tls",
				RevisionHistoryLimit: ptr.To(int32(1)),
				IssuerRef:            issuerRef,
			},
		}
	}

	tests := []struct {
		name   string
		mutate func(*certmanagerv1.Certificate)
		want   bool
	}{
		{
			name:   "identical managed fields",
			mutate: func(*certmanagerv1.Certificate) {},
			want:   false,
		},
		{
			name: "webhook-defaulted duration is ignored",
			mutate: func(existing *certmanagerv1.Certificate) {
				existing.Spec.Duration = &metav1.Duration{Duration: time.Hour}
			},
			want: false,
		},
		{
			name: "common name drift",
			mutate: func(existing *certmanagerv1.Certificate) {
				existing.Spec.CommonName = "other"
			},
			want: true,
		},
		{
			name: "dns names drift",
			mutate: func(existing *certmanagerv1.Certificate) {
				existing.Spec.DNSNames = []string{"other.svc"}
			},
			want: true,
		},
		{
			name: "nil and empty dns names match",
			mutate: func(existing *certmanagerv1.Certificate) {
				existing.Spec.DNSNames = nil
			},
			want: false,
		},
		{
			name: "secret name drift",
			mutate: func(existing *certmanagerv1.Certificate) {
				existing.Spec.SecretName = "other"
			},
			want: true,
		},
		{
			name: "revision history limit drift",
			mutate: func(existing *certmanagerv1.Certificate) {
				existing.Spec.RevisionHistoryLimit = ptr.To(int32(2))
			},
			want: true,
		},
		{
			name: "issuer ref drift",
			mutate: func(existing *certmanagerv1.Certificate) {
				existing.Spec.IssuerRef.Name = "other"
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			desired := base()
			existing := base()
			if tt.name == "nil and empty dns names match" {
				desired.Spec.DNSNames = []string{}
			}
			tt.mutate(existing)
			assert.Equal(t, tt.want, CertificateManagedFieldsModified(desired, existing))
		})
	}
}

func TestCertificateSpecModified(t *testing.T) {
	tests := []struct {
		name     string
		desired  *certmanagerv1.Certificate
		existing *certmanagerv1.Certificate
		want     bool
	}{
		{
			name:     "identical",
			desired:  &certmanagerv1.Certificate{Spec: certmanagerv1.CertificateSpec{DNSNames: []string{"a.example.com"}}},
			existing: &certmanagerv1.Certificate{Spec: certmanagerv1.CertificateSpec{DNSNames: []string{"a.example.com"}}},
			want:     false,
		},
		{
			name:     "different DNSNames",
			desired:  &certmanagerv1.Certificate{Spec: certmanagerv1.CertificateSpec{DNSNames: []string{"a.example.com"}}},
			existing: &certmanagerv1.Certificate{Spec: certmanagerv1.CertificateSpec{DNSNames: []string{"b.example.com"}}},
			want:     true,
		},
		{
			name: "duration counts as a spec change",
			desired: &certmanagerv1.Certificate{Spec: certmanagerv1.CertificateSpec{
				Duration: &metav1.Duration{Duration: time.Hour},
			}},
			existing: &certmanagerv1.Certificate{Spec: certmanagerv1.CertificateSpec{}},
			want:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, CertificateSpecModified(tt.desired, tt.existing))
		})
	}
}

func TestIssuerSpecModified(t *testing.T) {
	selfSigned := certmanagerv1.IssuerSpec{IssuerConfig: certmanagerv1.IssuerConfig{SelfSigned: &certmanagerv1.SelfSignedIssuer{}}}
	ca := certmanagerv1.IssuerSpec{IssuerConfig: certmanagerv1.IssuerConfig{CA: &certmanagerv1.CAIssuer{SecretName: "ca"}}}
	assert.False(t, IssuerSpecModified(
		&certmanagerv1.Issuer{Spec: selfSigned},
		&certmanagerv1.Issuer{Spec: selfSigned},
	))
	assert.True(t, IssuerSpecModified(
		&certmanagerv1.Issuer{Spec: selfSigned},
		&certmanagerv1.Issuer{Spec: ca},
	))
}

func TestServiceSpecModified(t *testing.T) {
	port := corev1.ServicePort{Name: "https", Port: 443}
	tests := []struct {
		name     string
		desired  *corev1.Service
		existing *corev1.Service
		want     bool
	}{
		{
			name: "identical",
			desired: &corev1.Service{Spec: corev1.ServiceSpec{
				Type:     corev1.ServiceTypeClusterIP,
				Selector: map[string]string{"app": "a"},
				Ports:    []corev1.ServicePort{port},
			}},
			existing: &corev1.Service{Spec: corev1.ServiceSpec{
				Type:     corev1.ServiceTypeClusterIP,
				Selector: map[string]string{"app": "a"},
				Ports:    []corev1.ServicePort{port},
			}},
			want: false,
		},
		{
			name: "nil and empty selector match",
			desired: &corev1.Service{Spec: corev1.ServiceSpec{
				Selector: nil,
			}},
			existing: &corev1.Service{Spec: corev1.ServiceSpec{
				Selector: map[string]string{},
			}},
			want: false,
		},
		{
			name: "selector drift",
			desired: &corev1.Service{Spec: corev1.ServiceSpec{
				Selector: map[string]string{"app": "a"},
			}},
			existing: &corev1.Service{Spec: corev1.ServiceSpec{
				Selector: map[string]string{"app": "b"},
			}},
			want: true,
		},
		{
			name: "port drift",
			desired: &corev1.Service{Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{port},
			}},
			existing: &corev1.Service{Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{{Name: "https", Port: 8443}},
			}},
			want: true,
		},
		{
			name:     "type drift",
			desired:  &corev1.Service{Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP}},
			existing: &corev1.Service{Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort}},
			want:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ServiceSpecModified(tt.desired, tt.existing))
		})
	}
}

func TestConfigMapDataModified(t *testing.T) {
	tests := []struct {
		name     string
		desired  *corev1.ConfigMap
		existing *corev1.ConfigMap
		want     bool
	}{
		{
			name:     "identical",
			desired:  &corev1.ConfigMap{Data: map[string]string{"key": "v1"}},
			existing: &corev1.ConfigMap{Data: map[string]string{"key": "v1"}},
			want:     false,
		},
		{
			name:     "different value",
			desired:  &corev1.ConfigMap{Data: map[string]string{"key": "v1"}},
			existing: &corev1.ConfigMap{Data: map[string]string{"key": "v2"}},
			want:     true,
		},
		{
			name:     "extra key",
			desired:  &corev1.ConfigMap{Data: map[string]string{"key": "v1"}},
			existing: &corev1.ConfigMap{Data: map[string]string{"key": "v1", "extra": "x"}},
			want:     true,
		},
		{
			name:     "nil and empty data match",
			desired:  &corev1.ConfigMap{Data: nil},
			existing: &corev1.ConfigMap{Data: map[string]string{}},
			want:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ConfigMapDataModified(tt.desired, tt.existing))
		})
	}
}

func TestRBACComparisons(t *testing.T) {
	rule := rbacv1.PolicyRule{APIGroups: []string{""}, Resources: []string{"secrets"}, Verbs: []string{"get"}}
	otherRule := rbacv1.PolicyRule{APIGroups: []string{""}, Resources: []string{"configmaps"}, Verbs: []string{"get"}}
	roleRef := rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "ClusterRole", Name: "trust-manager"}
	otherRef := roleRef
	otherRef.Name = "other"
	subject := rbacv1.Subject{Kind: "ServiceAccount", Name: "trust-manager", Namespace: "cert-manager"}
	otherSubject := subject
	otherSubject.Namespace = "other"

	assert.False(t, RBACRulesModified([]rbacv1.PolicyRule{rule}, []rbacv1.PolicyRule{rule}))
	assert.True(t, RBACRulesModified([]rbacv1.PolicyRule{rule}, []rbacv1.PolicyRule{otherRule}))
	assert.False(t, RBACRoleRefModified(roleRef, roleRef))
	assert.True(t, RBACRoleRefModified(roleRef, otherRef))
	assert.False(t, RBACSubjectsModified([]rbacv1.Subject{subject}, []rbacv1.Subject{subject}))
	assert.True(t, RBACSubjectsModified([]rbacv1.Subject{subject}, []rbacv1.Subject{otherSubject}))
}

func TestNetworkPolicySpecModified(t *testing.T) {
	ingress := &networkingv1.NetworkPolicy{Spec: networkingv1.NetworkPolicySpec{PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}}}
	egress := &networkingv1.NetworkPolicy{Spec: networkingv1.NetworkPolicySpec{PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress}}}
	assert.False(t, NetworkPolicySpecModified(ingress, ingress.DeepCopy()))
	assert.True(t, NetworkPolicySpecModified(ingress, egress))
}

func TestServiceAccountModified(t *testing.T) {
	assert.False(t, ServiceAccountModified(
		&corev1.ServiceAccount{},
		&corev1.ServiceAccount{},
	))
	assert.False(t, ServiceAccountModified(
		&corev1.ServiceAccount{AutomountServiceAccountToken: ptr.To(false)},
		&corev1.ServiceAccount{AutomountServiceAccountToken: ptr.To(false)},
	))
	assert.True(t, ServiceAccountModified(
		&corev1.ServiceAccount{AutomountServiceAccountToken: ptr.To(true)},
		&corev1.ServiceAccount{AutomountServiceAccountToken: ptr.To(false)},
	))
	assert.True(t, ServiceAccountModified(
		&corev1.ServiceAccount{},
		&corev1.ServiceAccount{AutomountServiceAccountToken: ptr.To(true)},
	))
}
