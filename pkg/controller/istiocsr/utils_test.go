package istiocsr

import (
	"fmt"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
)

// baseDeployment returns a minimal deployment for spec comparison tests.
func baseDeployment(replicas int32, image string) *appsv1.Deployment {
	return &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "x"}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "x"}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "c",
						Image: image,
						Args:  []string{"--arg"},
						Ports: []corev1.ContainerPort{{ContainerPort: 8080}},
						ReadinessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/ready"}},
							InitialDelaySeconds: 0,
							PeriodSeconds:        1,
						},
						SecurityContext: &corev1.SecurityContext{},
						Resources:       corev1.ResourceRequirements{},
					}},
					ServiceAccountName: "sa",
				},
			},
		},
	}
}

func TestHasObjectChanged(t *testing.T) {
	tests := []struct {
		name        string
		desired     client.Object
		fetched     client.Object
		wantPanic   bool
		panicSubstr string
		wantChanged bool
	}{
		{
			name:        "different types panics",
			desired:     &rbacv1.ClusterRole{},
			fetched:     &rbacv1.ClusterRoleBinding{},
			wantPanic:   true,
			panicSubstr: "same type",
		},
		{
			name:        "mismatched types (CR vs CRB) panics",
			desired:     &rbacv1.ClusterRole{},
			fetched:     &rbacv1.ClusterRoleBinding{},
			wantPanic:   true,
			panicSubstr: "same type",
		},
		{
			name: "matching ClusterRole identical",
			desired: &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"a": "1"}},
				Rules:      []rbacv1.PolicyRule{{Verbs: []string{"get"}}},
			},
			fetched: &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"a": "1"}},
				Rules:      []rbacv1.PolicyRule{{Verbs: []string{"get"}}},
			},
			wantChanged: false,
		},
		{
			name: "ClusterRole rules different",
			desired: &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"a": "1"}},
				Rules:      []rbacv1.PolicyRule{{Verbs: []string{"get"}, APIGroups: []string{""}}},
			},
			fetched: &rbacv1.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"a": "1"}},
				Rules:      []rbacv1.PolicyRule{{Verbs: []string{"list"}}},
			},
			wantChanged: true,
		},
		{
			name: "matching ClusterRoleBinding identical",
			desired: &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"a": "1"}},
				RoleRef:    rbacv1.RoleRef{APIGroup: "rbac", Kind: "ClusterRole", Name: "x"},
				Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "y", Namespace: "z"}},
			},
			fetched: &rbacv1.ClusterRoleBinding{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"a": "1"}},
				RoleRef:    rbacv1.RoleRef{APIGroup: "rbac", Kind: "ClusterRole", Name: "x"},
				Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "y", Namespace: "z"}},
			},
			wantChanged: false,
		},
		{
			name:    "Deployment labels different",
			desired: func() *appsv1.Deployment { d := baseDeployment(1, "img"); d.Labels = map[string]string{"a": "1"}; return d }(),
			fetched: func() *appsv1.Deployment { d := baseDeployment(1, "img"); d.Labels = map[string]string{"a": "2"}; return d }(),
			wantChanged: true,
		},
		{
			name: "ConfigMap identical",
			desired: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"a": "1"}},
				Data:       map[string]string{"k": "v"},
			},
			fetched: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"a": "1"}},
				Data:       map[string]string{"k": "v"},
			},
			wantChanged: false,
		},
		{
			name: "Service identical",
			desired: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"a": "1"}},
				Spec: corev1.ServiceSpec{
					Type:     corev1.ServiceTypeClusterIP,
					Ports:    []corev1.ServicePort{{Port: 443}},
					Selector: map[string]string{"app": "x"},
				},
			},
			fetched: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"a": "1"}},
				Spec: corev1.ServiceSpec{
					Type:     corev1.ServiceTypeClusterIP,
					Ports:    []corev1.ServicePort{{Port: 443}},
					Selector: map[string]string{"app": "x"},
				},
			},
			wantChanged: false,
		},
		{
			name:        "unsupported type (Pod) panics",
			desired:     &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "a"}},
			fetched:     &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "a"}},
			wantPanic:   true,
			panicSubstr: "unsupported",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantPanic {
				defer func() {
					r := recover()
					if r == nil {
						t.Error("expected panic")
					}
					if msg := fmt.Sprintf("%v", r); tt.panicSubstr != "" && !strings.Contains(msg, tt.panicSubstr) {
						t.Errorf("panic message = %q, want substring %q", msg, tt.panicSubstr)
					}
				}()
			}
			got := hasObjectChanged(tt.desired, tt.fetched)
			if !tt.wantPanic && got != tt.wantChanged {
				t.Errorf("hasObjectChanged() = %v, want %v", got, tt.wantChanged)
			}
		})
	}
}

func TestDeploymentSpecModified(t *testing.T) {
	tests := []struct {
		name     string
		desired  *appsv1.Deployment
		fetched  *appsv1.Deployment
		wantTrue bool
	}{
		{
			name:     "identical",
			desired:  baseDeployment(1, "img"),
			fetched:  baseDeployment(1, "img"),
			wantTrue: false,
		},
		{
			name:     "replicas different",
			desired:  baseDeployment(1, "img"),
			fetched:  baseDeployment(2, "img"),
			wantTrue: true,
		},
		{
			name:     "image different",
			desired:  baseDeployment(1, "img:v1"),
			fetched:  baseDeployment(1, "img:v2"),
			wantTrue: true,
		},
		{
			name: "selector match labels different",
			desired: func() *appsv1.Deployment {
				d := baseDeployment(1, "img")
				d.Spec.Selector.MatchLabels = map[string]string{"app": "x"}
				return d
			}(),
			fetched: func() *appsv1.Deployment {
				d := baseDeployment(1, "img")
				d.Spec.Selector.MatchLabels = map[string]string{"app": "y"}
				return d
			}(),
			wantTrue: true,
		},
		{
			name: "template labels different",
			desired: func() *appsv1.Deployment {
				d := baseDeployment(1, "img")
				d.Spec.Template.Labels = map[string]string{"app": "x"}
				return d
			}(),
			fetched: func() *appsv1.Deployment {
				d := baseDeployment(1, "img")
				d.Spec.Template.Labels = map[string]string{"app": "other"}
				return d
			}(),
			wantTrue: true,
		},
		{
			name: "container count different",
			desired: func() *appsv1.Deployment {
				d := baseDeployment(1, "img")
				return d
			}(),
			fetched: func() *appsv1.Deployment {
				d := baseDeployment(1, "img")
				d.Spec.Template.Spec.Containers = append(d.Spec.Template.Spec.Containers, corev1.Container{Name: "sidecar"})
				return d
			}(),
			wantTrue: true,
		},
		{
			name: "args different",
			desired: func() *appsv1.Deployment {
				d := baseDeployment(1, "img")
				d.Spec.Template.Spec.Containers[0].Args = []string{"--arg=a"}
				return d
			}(),
			fetched: func() *appsv1.Deployment {
				d := baseDeployment(1, "img")
				d.Spec.Template.Spec.Containers[0].Args = []string{"--arg=b"}
				return d
			}(),
			wantTrue: true,
		},
		{
			name: "container name different",
			desired: func() *appsv1.Deployment {
				d := baseDeployment(1, "img")
				d.Spec.Template.Spec.Containers[0].Name = "c"
				return d
			}(),
			fetched: func() *appsv1.Deployment {
				d := baseDeployment(1, "img")
				d.Spec.Template.Spec.Containers[0].Name = "other"
				return d
			}(),
			wantTrue: true,
		},
		{
			name: "ports length different",
			desired: baseDeployment(1, "img"),
			fetched: func() *appsv1.Deployment {
				d := baseDeployment(1, "img")
				d.Spec.Template.Spec.Containers[0].Ports = append(d.Spec.Template.Spec.Containers[0].Ports, corev1.ContainerPort{ContainerPort: 9090})
				return d
			}(),
			wantTrue: true,
		},
		{
			name: "ports content different",
			desired: baseDeployment(1, "img"),
			fetched: func() *appsv1.Deployment {
				d := baseDeployment(1, "img")
				d.Spec.Template.Spec.Containers[0].Ports = []corev1.ContainerPort{{ContainerPort: 9090}}
				return d
			}(),
			wantTrue: true,
		},
		{
			name: "readiness probe path different",
			desired: baseDeployment(1, "img"),
			fetched: func() *appsv1.Deployment {
				d := baseDeployment(1, "img")
				d.Spec.Template.Spec.Containers[0].ReadinessProbe.HTTPGet.Path = "/other"
				return d
			}(),
			wantTrue: true,
		},
		{
			name: "serviceAccountName different",
			desired: baseDeployment(1, "img"),
			fetched: func() *appsv1.Deployment {
				d := baseDeployment(1, "img")
				d.Spec.Template.Spec.ServiceAccountName = "other-sa"
				return d
			}(),
			wantTrue: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deploymentSpecModified(tt.desired, tt.fetched)
			if got != tt.wantTrue {
				t.Errorf("deploymentSpecModified() = %v, want %v", got, tt.wantTrue)
			}
		})
	}
}

func TestHasObjectChanged_RoleAndRoleBinding(t *testing.T) {
	t.Run("Role rules different", func(t *testing.T) {
		desired := &rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"a": "1"}},
			Rules:     []rbacv1.PolicyRule{{Verbs: []string{"get"}, APIGroups: []string{""}}},
		}
		fetched := &rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"a": "1"}},
			Rules:     []rbacv1.PolicyRule{{Verbs: []string{"list"}}},
		}
		if !hasObjectChanged(desired, fetched) {
			t.Error("hasObjectChanged() = false, want true for Role rules different")
		}
	})
	t.Run("ClusterRoleBinding RoleRef different", func(t *testing.T) {
		desired := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"a": "1"}},
			RoleRef:    rbacv1.RoleRef{APIGroup: "rbac", Kind: "ClusterRole", Name: "role-a"},
			Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "y", Namespace: "z"}},
		}
		fetched := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"a": "1"}},
			RoleRef:    rbacv1.RoleRef{APIGroup: "rbac", Kind: "ClusterRole", Name: "role-b"},
			Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "y", Namespace: "z"}},
		}
		if !hasObjectChanged(desired, fetched) {
			t.Error("hasObjectChanged() = false, want true for RoleRef different")
		}
	})
	t.Run("ClusterRoleBinding Subjects different", func(t *testing.T) {
		desired := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"a": "1"}},
			RoleRef:    rbacv1.RoleRef{APIGroup: "rbac", Kind: "ClusterRole", Name: "x"},
			Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "y", Namespace: "z"}},
		}
		fetched := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"a": "1"}},
			RoleRef:    rbacv1.RoleRef{APIGroup: "rbac", Kind: "ClusterRole", Name: "x"},
			Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "other", Namespace: "z"}},
		}
		if !hasObjectChanged(desired, fetched) {
			t.Error("hasObjectChanged() = false, want true for Subjects different")
		}
	})
	t.Run("RoleBinding RoleRef different", func(t *testing.T) {
		desired := &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"a": "1"}},
			RoleRef:    rbacv1.RoleRef{APIGroup: "rbac", Kind: "Role", Name: "role-a"},
			Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "y", Namespace: "z"}},
		}
		fetched := &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"a": "1"}},
			RoleRef:    rbacv1.RoleRef{APIGroup: "rbac", Kind: "Role", Name: "role-b"},
			Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "y", Namespace: "z"}},
		}
		if !hasObjectChanged(desired, fetched) {
			t.Error("hasObjectChanged() = false, want true for RoleBinding RoleRef different")
		}
	})
}

func TestServiceSpecModified(t *testing.T) {
	tests := []struct {
		name     string
		desired  *corev1.Service
		fetched  *corev1.Service
		wantTrue bool
	}{
		{
			name: "identical",
			desired: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Type:     corev1.ServiceTypeClusterIP,
					Ports:    []corev1.ServicePort{{Port: 443}},
					Selector: map[string]string{"app": "x"},
				},
			},
			fetched: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Type:     corev1.ServiceTypeClusterIP,
					Ports:    []corev1.ServicePort{{Port: 443}},
					Selector: map[string]string{"app": "x"},
				},
			},
			wantTrue: false,
		},
		{
			name: "different type",
			desired: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Type:     corev1.ServiceTypeClusterIP,
					Ports:    []corev1.ServicePort{{Port: 443}},
					Selector: map[string]string{"app": "x"},
				},
			},
			fetched: &corev1.Service{
				Spec: corev1.ServiceSpec{
					Type:     corev1.ServiceTypeNodePort,
					Ports:    []corev1.ServicePort{{Port: 443}},
					Selector: map[string]string{"app": "x"},
				},
			},
			wantTrue: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := serviceSpecModified(tt.desired, tt.fetched)
			if got != tt.wantTrue {
				t.Errorf("serviceSpecModified() = %v, want %v", got, tt.wantTrue)
			}
		})
	}
}

func TestCertificateSpecModified(t *testing.T) {
	tests := []struct {
		name     string
		desired  *certmanagerv1.Certificate
		fetched  *certmanagerv1.Certificate
		wantTrue bool
	}{
		{
			name:     "identical",
			desired:  &certmanagerv1.Certificate{Spec: certmanagerv1.CertificateSpec{DNSNames: []string{"a.example.com"}}},
			fetched:  &certmanagerv1.Certificate{Spec: certmanagerv1.CertificateSpec{DNSNames: []string{"a.example.com"}}},
			wantTrue: false,
		},
		{
			name:     "different DNSNames",
			desired:  &certmanagerv1.Certificate{Spec: certmanagerv1.CertificateSpec{DNSNames: []string{"a.example.com"}}},
			fetched:  &certmanagerv1.Certificate{Spec: certmanagerv1.CertificateSpec{DNSNames: []string{"b.example.com"}}},
			wantTrue: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := certificateSpecModified(tt.desired, tt.fetched)
			if got != tt.wantTrue {
				t.Errorf("certificateSpecModified() = %v, want %v", got, tt.wantTrue)
			}
		})
	}
}

func TestConfigMapDataModified(t *testing.T) {
	tests := []struct {
		name     string
		desired  *corev1.ConfigMap
		fetched  *corev1.ConfigMap
		wantTrue bool
	}{
		{
			name:     "identical",
			desired:  &corev1.ConfigMap{Data: map[string]string{"key": "v1"}},
			fetched:  &corev1.ConfigMap{Data: map[string]string{"key": "v1"}},
			wantTrue: false,
		},
		{
			name:     "different value",
			desired:  &corev1.ConfigMap{Data: map[string]string{"key": "v1"}},
			fetched:  &corev1.ConfigMap{Data: map[string]string{"key": "v2"}},
			wantTrue: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := configMapDataModified(tt.desired, tt.fetched)
			if got != tt.wantTrue {
				t.Errorf("configMapDataModified() = %v, want %v", got, tt.wantTrue)
			}
		})
	}
}

func TestNetworkPolicySpecModified(t *testing.T) {
	tests := []struct {
		name     string
		desired  *networkingv1.NetworkPolicy
		fetched  *networkingv1.NetworkPolicy
		wantTrue bool
	}{
		{
			name:    "identical",
			desired: &networkingv1.NetworkPolicy{Spec: networkingv1.NetworkPolicySpec{PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}}},
			fetched: &networkingv1.NetworkPolicy{Spec: networkingv1.NetworkPolicySpec{PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}}},
			wantTrue: false,
		},
		{
			name:    "different PolicyTypes",
			desired: &networkingv1.NetworkPolicy{Spec: networkingv1.NetworkPolicySpec{PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}}},
			fetched: &networkingv1.NetworkPolicy{Spec: networkingv1.NetworkPolicySpec{PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress}}},
			wantTrue: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := networkPolicySpecModified(tt.desired, tt.fetched)
			if got != tt.wantTrue {
				t.Errorf("networkPolicySpecModified() = %v, want %v", got, tt.wantTrue)
			}
		})
	}
}
