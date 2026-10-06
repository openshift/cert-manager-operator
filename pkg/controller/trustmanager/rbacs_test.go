package trustmanager

import (
	"context"
	"reflect"
	"slices"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common/fakes"
)

func TestRoleObject(t *testing.T) {
	tests := []struct {
		name            string
		tm              *trustManagerBuilder
		getRole         func(map[string]string, map[string]string) client.Object
		wantName        string
		wantNamespace   string
		wantLabels      map[string]string
		wantAnnotations map[string]string
	}{
		{
			name: "cluster role has correct metadata",
			tm:   testTrustManager(),
			getRole: func(l, a map[string]string) client.Object {
				return getClusterRoleObject(v1alpha1.TrustManagerConfig{}, l, a)
			},
			wantName: trustManagerClusterRoleName,
			wantLabels: map[string]string{
				"app": trustManagerCommonName,
			},
		},
		{
			name: "trust namespace role has correct metadata",
			tm:   testTrustManager(),
			getRole: func(l, a map[string]string) client.Object {
				return getTrustNamespaceRoleObject(l, a, defaultTrustNamespace)
			},
			wantName:      trustManagerRoleName,
			wantNamespace: defaultTrustNamespace,
			wantLabels: map[string]string{
				"app": trustManagerCommonName,
			},
		},
		{
			name: "leader election role has correct metadata",
			tm:   testTrustManager(),
			getRole: func(l, a map[string]string) client.Object {
				return getLeaderElectionRoleObject(l, a)
			},
			wantName:      trustManagerLeaderElectionRoleName,
			wantNamespace: operandNamespace,
			wantLabels: map[string]string{
				"app": trustManagerCommonName,
			},
		},
		{
			name: "cluster role default labels take precedence over user labels",
			tm:   testTrustManager().WithLabels(map[string]string{"app": "should-be-overridden"}),
			getRole: func(l, a map[string]string) client.Object {
				return getClusterRoleObject(v1alpha1.TrustManagerConfig{}, l, a)
			},
			wantLabels: map[string]string{
				"app": trustManagerCommonName,
			},
		},
		{
			name: "cluster role merges custom labels and annotations",
			tm: testTrustManager().
				WithLabels(map[string]string{"user-label": "test-value"}).
				WithAnnotations(map[string]string{"user-annotation": "test-value"}),
			getRole: func(l, a map[string]string) client.Object {
				return getClusterRoleObject(v1alpha1.TrustManagerConfig{}, l, a)
			},
			wantLabels:      map[string]string{"user-label": "test-value"},
			wantAnnotations: map[string]string{"user-annotation": "test-value"},
		},
		{
			name: "trust namespace role default labels take precedence over user labels",
			tm:   testTrustManager().WithLabels(map[string]string{"app": "should-be-overridden"}),
			getRole: func(l, a map[string]string) client.Object {
				return getTrustNamespaceRoleObject(l, a, defaultTrustNamespace)
			},
			wantLabels: map[string]string{
				"app": trustManagerCommonName,
			},
		},
		{
			name: "trust namespace role merges custom labels and annotations",
			tm: testTrustManager().
				WithLabels(map[string]string{"user-label": "test-value"}).
				WithAnnotations(map[string]string{"user-annotation": "test-value"}),
			getRole: func(l, a map[string]string) client.Object {
				return getTrustNamespaceRoleObject(l, a, defaultTrustNamespace)
			},
			wantLabels:      map[string]string{"user-label": "test-value"},
			wantAnnotations: map[string]string{"user-annotation": "test-value"},
		},
		{
			name: "leader election role default labels take precedence over user labels",
			tm:   testTrustManager().WithLabels(map[string]string{"app": "should-be-overridden"}),
			getRole: func(l, a map[string]string) client.Object {
				return getLeaderElectionRoleObject(l, a)
			},
			wantLabels: map[string]string{
				"app": trustManagerCommonName,
			},
		},
		{
			name: "leader election role merges custom labels and annotations",
			tm: testTrustManager().
				WithLabels(map[string]string{"user-label": "test-value"}).
				WithAnnotations(map[string]string{"user-annotation": "test-value"}),
			getRole: func(l, a map[string]string) client.Object {
				return getLeaderElectionRoleObject(l, a)
			},
			wantLabels:      map[string]string{"user-label": "test-value"},
			wantAnnotations: map[string]string{"user-annotation": "test-value"},
		},
		{
			name: "target namespace role has correct metadata",
			tm:   testTrustManager().WithTargetNamespaces("app-ns"),
			getRole: func(l, a map[string]string) client.Object {
				return getTargetNamespaceRoleObject("app-ns", v1alpha1.SecretTargetsConfig{}, l, a)
			},
			wantName:      trustManagerTargetRoleName,
			wantNamespace: "app-ns",
			wantLabels: map[string]string{
				"app": trustManagerCommonName,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm := tt.tm.Build()
			obj := tt.getRole(getResourceLabels(tm), getResourceAnnotations(tm))

			if tt.wantName != "" && obj.GetName() != tt.wantName {
				t.Errorf("expected name %q, got %q", tt.wantName, obj.GetName())
			}
			if tt.wantNamespace != "" && obj.GetNamespace() != tt.wantNamespace {
				t.Errorf("expected namespace %q, got %q", tt.wantNamespace, obj.GetNamespace())
			}
			for key, val := range tt.wantLabels {
				if obj.GetLabels()[key] != val {
					t.Errorf("expected label %s=%q, got %q", key, val, obj.GetLabels()[key])
				}
			}
			for key, val := range tt.wantAnnotations {
				if obj.GetAnnotations()[key] != val {
					t.Errorf("expected annotation %s=%q, got %q", key, val, obj.GetAnnotations()[key])
				}
			}
		})
	}
}

func TestRoleBindingObject(t *testing.T) {
	tests := []struct {
		name            string
		tm              *trustManagerBuilder
		getBinding      func(map[string]string, map[string]string) client.Object
		wantName        string
		wantNamespace   string
		wantRoleRefName string
		wantRoleRefKind string
		wantSubjectName string
		wantSubjectNS   string
		wantLabels      map[string]string
		wantAnnotations map[string]string
	}{
		{
			name: "cluster role binding has correct metadata and subjects",
			tm:   testTrustManager(),
			getBinding: func(l, a map[string]string) client.Object {
				return getClusterRoleBindingObject(l, a)
			},
			wantName:        trustManagerClusterRoleBindingName,
			wantRoleRefName: trustManagerClusterRoleName,
			wantRoleRefKind: "ClusterRole",
			wantSubjectName: trustManagerServiceAccountName,
			wantSubjectNS:   operandNamespace,
			wantLabels: map[string]string{
				"app": trustManagerCommonName,
			},
		},
		{
			name: "trust namespace role binding has correct metadata and subjects",
			tm:   testTrustManager(),
			getBinding: func(l, a map[string]string) client.Object {
				return getTrustNamespaceRoleBindingObject(l, a, defaultTrustNamespace)
			},
			wantName:        trustManagerRoleBindingName,
			wantNamespace:   defaultTrustNamespace,
			wantRoleRefName: trustManagerRoleName,
			wantRoleRefKind: "Role",
			wantSubjectName: trustManagerServiceAccountName,
			wantSubjectNS:   operandNamespace,
			wantLabels: map[string]string{
				"app": trustManagerCommonName,
			},
		},
		{
			name: "leader election role binding has correct metadata and subjects",
			tm:   testTrustManager(),
			getBinding: func(l, a map[string]string) client.Object {
				return getLeaderElectionRoleBindingObject(l, a)
			},
			wantName:        trustManagerLeaderElectionRoleBindingName,
			wantNamespace:   operandNamespace,
			wantRoleRefName: trustManagerLeaderElectionRoleName,
			wantRoleRefKind: "Role",
			wantSubjectName: trustManagerServiceAccountName,
			wantSubjectNS:   operandNamespace,
			wantLabels: map[string]string{
				"app": trustManagerCommonName,
			},
		},
		{
			name: "cluster role binding default labels take precedence over user labels",
			tm:   testTrustManager().WithLabels(map[string]string{"app": "should-be-overridden"}),
			getBinding: func(l, a map[string]string) client.Object {
				return getClusterRoleBindingObject(l, a)
			},
			wantLabels: map[string]string{
				"app": trustManagerCommonName,
			},
		},
		{
			name: "cluster role binding merges custom labels and annotations",
			tm: testTrustManager().
				WithLabels(map[string]string{"user-label": "test-value"}).
				WithAnnotations(map[string]string{"user-annotation": "test-value"}),
			getBinding: func(l, a map[string]string) client.Object {
				return getClusterRoleBindingObject(l, a)
			},
			wantLabels:      map[string]string{"user-label": "test-value"},
			wantAnnotations: map[string]string{"user-annotation": "test-value"},
		},
		{
			name: "trust namespace role binding default labels take precedence over user labels",
			tm:   testTrustManager().WithLabels(map[string]string{"app": "should-be-overridden"}),
			getBinding: func(l, a map[string]string) client.Object {
				return getTrustNamespaceRoleBindingObject(l, a, defaultTrustNamespace)
			},
			wantLabels: map[string]string{
				"app": trustManagerCommonName,
			},
		},
		{
			name: "trust namespace role binding merges custom labels and annotations",
			tm: testTrustManager().
				WithLabels(map[string]string{"user-label": "test-value"}).
				WithAnnotations(map[string]string{"user-annotation": "test-value"}),
			getBinding: func(l, a map[string]string) client.Object {
				return getTrustNamespaceRoleBindingObject(l, a, defaultTrustNamespace)
			},
			wantLabels:      map[string]string{"user-label": "test-value"},
			wantAnnotations: map[string]string{"user-annotation": "test-value"},
		},
		{
			name: "leader election role binding default labels take precedence over user labels",
			tm:   testTrustManager().WithLabels(map[string]string{"app": "should-be-overridden"}),
			getBinding: func(l, a map[string]string) client.Object {
				return getLeaderElectionRoleBindingObject(l, a)
			},
			wantLabels: map[string]string{
				"app": trustManagerCommonName,
			},
		},
		{
			name: "leader election role binding merges custom labels and annotations",
			tm: testTrustManager().
				WithLabels(map[string]string{"user-label": "test-value"}).
				WithAnnotations(map[string]string{"user-annotation": "test-value"}),
			getBinding: func(l, a map[string]string) client.Object {
				return getLeaderElectionRoleBindingObject(l, a)
			},
			wantLabels:      map[string]string{"user-label": "test-value"},
			wantAnnotations: map[string]string{"user-annotation": "test-value"},
		},
		{
			name: "target namespace role binding has correct metadata and subjects",
			tm:   testTrustManager().WithTargetNamespaces("app-ns"),
			getBinding: func(l, a map[string]string) client.Object {
				return getTargetNamespaceRoleBindingObject("app-ns", l, a)
			},
			wantName:        trustManagerTargetRoleBindingName,
			wantNamespace:   "app-ns",
			wantRoleRefName: trustManagerTargetRoleName,
			wantRoleRefKind: "Role",
			wantSubjectName: trustManagerServiceAccountName,
			wantSubjectNS:   operandNamespace,
			wantLabels: map[string]string{
				"app": trustManagerCommonName,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm := tt.tm.Build()
			obj := tt.getBinding(getResourceLabels(tm), getResourceAnnotations(tm))

			if tt.wantName != "" && obj.GetName() != tt.wantName {
				t.Errorf("expected name %q, got %q", tt.wantName, obj.GetName())
			}
			if tt.wantNamespace != "" && obj.GetNamespace() != tt.wantNamespace {
				t.Errorf("expected namespace %q, got %q", tt.wantNamespace, obj.GetNamespace())
			}
			for key, val := range tt.wantLabels {
				if obj.GetLabels()[key] != val {
					t.Errorf("expected label %s=%q, got %q", key, val, obj.GetLabels()[key])
				}
			}
			for key, val := range tt.wantAnnotations {
				if obj.GetAnnotations()[key] != val {
					t.Errorf("expected annotation %s=%q, got %q", key, val, obj.GetAnnotations()[key])
				}
			}

			switch b := obj.(type) {
			case *rbacv1.ClusterRoleBinding:
				if tt.wantRoleRefName != "" && b.RoleRef.Name != tt.wantRoleRefName {
					t.Errorf("expected roleRef.name %q, got %q", tt.wantRoleRefName, b.RoleRef.Name)
				}
				if tt.wantRoleRefKind != "" && b.RoleRef.Kind != tt.wantRoleRefKind {
					t.Errorf("expected roleRef.kind %q, got %q", tt.wantRoleRefKind, b.RoleRef.Kind)
				}
				if tt.wantSubjectName != "" {
					assertSubjects(t, b.Subjects, tt.wantSubjectName, tt.wantSubjectNS)
				}
			case *rbacv1.RoleBinding:
				if tt.wantRoleRefName != "" && b.RoleRef.Name != tt.wantRoleRefName {
					t.Errorf("expected roleRef.name %q, got %q", tt.wantRoleRefName, b.RoleRef.Name)
				}
				if tt.wantRoleRefKind != "" && b.RoleRef.Kind != tt.wantRoleRefKind {
					t.Errorf("expected roleRef.kind %q, got %q", tt.wantRoleRefKind, b.RoleRef.Kind)
				}
				if tt.wantSubjectName != "" {
					assertSubjects(t, b.Subjects, tt.wantSubjectName, tt.wantSubjectNS)
				}
			}
		})
	}
}

func TestRBACReconciliation(t *testing.T) {
	tests := []struct {
		name            string
		tmBuilder       *trustManagerBuilder
		preReq          func(*Reconciler, *fakes.FakeCtrlClient)
		wantErr         string
		wantExistsCount int
		wantPatchCount  int
	}{
		{
			name: "successful apply of all RBAC resources",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					return false, nil
				})
			},
			wantExistsCount: 6,
			wantPatchCount:  6,
		},
		{
			name: "skip apply when all RBAC resources match desired",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				existsCall := 0
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					existsCall++
					switch existsCall {
					case 1:
						cr := getClusterRoleObject(v1alpha1.TrustManagerConfig{}, testResourceLabels(), testResourceAnnotations())
						cr.DeepCopyInto(obj.(*rbacv1.ClusterRole))
					case 2:
						crb := getClusterRoleBindingObject(testResourceLabels(), testResourceAnnotations())
						crb.DeepCopyInto(obj.(*rbacv1.ClusterRoleBinding))
					case 3:
						role := getTrustNamespaceRoleObject(testResourceLabels(), testResourceAnnotations(), defaultTrustNamespace)
						role.DeepCopyInto(obj.(*rbacv1.Role))
					case 4:
						rb := getTrustNamespaceRoleBindingObject(testResourceLabels(), testResourceAnnotations(), defaultTrustNamespace)
						rb.DeepCopyInto(obj.(*rbacv1.RoleBinding))
					case 5:
						role := getLeaderElectionRoleObject(testResourceLabels(), testResourceAnnotations())
						role.DeepCopyInto(obj.(*rbacv1.Role))
					case 6:
						rb := getLeaderElectionRoleBindingObject(testResourceLabels(), testResourceAnnotations())
						rb.DeepCopyInto(obj.(*rbacv1.RoleBinding))
					}
					return true, nil
				})
			},
			wantExistsCount: 6,
			wantPatchCount:  0,
		},
		{
			name: "apply when cluster role has label drift",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				existsCall := 0
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					existsCall++
					switch existsCall {
					case 1:
						cr := getClusterRoleObject(v1alpha1.TrustManagerConfig{}, testResourceLabels(), testResourceAnnotations())
						cr.Labels["app.kubernetes.io/instance"] = "modified-value"
						cr.DeepCopyInto(obj.(*rbacv1.ClusterRole))
					case 2:
						crb := getClusterRoleBindingObject(testResourceLabels(), testResourceAnnotations())
						crb.DeepCopyInto(obj.(*rbacv1.ClusterRoleBinding))
					case 3:
						role := getTrustNamespaceRoleObject(testResourceLabels(), testResourceAnnotations(), defaultTrustNamespace)
						role.DeepCopyInto(obj.(*rbacv1.Role))
					case 4:
						rb := getTrustNamespaceRoleBindingObject(testResourceLabels(), testResourceAnnotations(), defaultTrustNamespace)
						rb.DeepCopyInto(obj.(*rbacv1.RoleBinding))
					case 5:
						role := getLeaderElectionRoleObject(testResourceLabels(), testResourceAnnotations())
						role.DeepCopyInto(obj.(*rbacv1.Role))
					case 6:
						rb := getLeaderElectionRoleBindingObject(testResourceLabels(), testResourceAnnotations())
						rb.DeepCopyInto(obj.(*rbacv1.RoleBinding))
					}
					return true, nil
				})
			},
			wantExistsCount: 6,
			wantPatchCount:  1,
		},
		{
			name:      "apply when cluster role binding has annotation drift",
			tmBuilder: testTrustManager().WithAnnotations(map[string]string{"user-annotation": "original"}),
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				tm := testTrustManager().WithAnnotations(map[string]string{"user-annotation": "original"}).Build()
				labels := getResourceLabels(tm)
				annotations := getResourceAnnotations(tm)
				existsCall := 0
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					existsCall++
					switch existsCall {
					case 1:
						cr := getClusterRoleObject(v1alpha1.TrustManagerConfig{}, labels, annotations)
						cr.DeepCopyInto(obj.(*rbacv1.ClusterRole))
					case 2:
						crb := getClusterRoleBindingObject(labels, annotations)
						crb.Annotations["user-annotation"] = "tampered"
						crb.DeepCopyInto(obj.(*rbacv1.ClusterRoleBinding))
					case 3:
						role := getTrustNamespaceRoleObject(labels, annotations, defaultTrustNamespace)
						role.DeepCopyInto(obj.(*rbacv1.Role))
					case 4:
						rb := getTrustNamespaceRoleBindingObject(labels, annotations, defaultTrustNamespace)
						rb.DeepCopyInto(obj.(*rbacv1.RoleBinding))
					case 5:
						role := getLeaderElectionRoleObject(labels, annotations)
						role.DeepCopyInto(obj.(*rbacv1.Role))
					case 6:
						rb := getLeaderElectionRoleBindingObject(labels, annotations)
						rb.DeepCopyInto(obj.(*rbacv1.RoleBinding))
					}
					return true, nil
				})
			},
			wantExistsCount: 6,
			wantPatchCount:  1,
		},
		{
			name: "apply when role has rules drift",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				existsCall := 0
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					existsCall++
					switch existsCall {
					case 1:
						cr := getClusterRoleObject(v1alpha1.TrustManagerConfig{}, testResourceLabels(), testResourceAnnotations())
						cr.DeepCopyInto(obj.(*rbacv1.ClusterRole))
					case 2:
						crb := getClusterRoleBindingObject(testResourceLabels(), testResourceAnnotations())
						crb.DeepCopyInto(obj.(*rbacv1.ClusterRoleBinding))
					case 3:
						role := getTrustNamespaceRoleObject(testResourceLabels(), testResourceAnnotations(), defaultTrustNamespace)
						role.Rules = append(role.Rules, rbacv1.PolicyRule{
							APIGroups: []string{""}, Resources: []string{"extra"}, Verbs: []string{"get"},
						})
						role.DeepCopyInto(obj.(*rbacv1.Role))
					case 4:
						rb := getTrustNamespaceRoleBindingObject(testResourceLabels(), testResourceAnnotations(), defaultTrustNamespace)
						rb.DeepCopyInto(obj.(*rbacv1.RoleBinding))
					case 5:
						role := getLeaderElectionRoleObject(testResourceLabels(), testResourceAnnotations())
						role.DeepCopyInto(obj.(*rbacv1.Role))
					case 6:
						rb := getLeaderElectionRoleBindingObject(testResourceLabels(), testResourceAnnotations())
						rb.DeepCopyInto(obj.(*rbacv1.RoleBinding))
					}
					return true, nil
				})
			},
			wantExistsCount: 6,
			wantPatchCount:  1,
		},
		{
			name: "exists error propagates on first resource",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					return false, errTestClient
				})
			},
			wantErr:         "failed to check if clusterrole",
			wantExistsCount: 1,
			wantPatchCount:  0,
		},
		{
			name: "clusterrole patch error propagates",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					return false, nil
				})
				m.PatchCalls(func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					if _, ok := obj.(*rbacv1.ClusterRole); ok {
						return errTestClient
					}
					return nil
				})
			},
			wantErr:         "failed to apply clusterrole",
			wantExistsCount: 1,
			wantPatchCount:  1,
		},
		{
			name: "clusterrolebinding patch error propagates",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					return false, nil
				})
				m.PatchCalls(func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					if _, ok := obj.(*rbacv1.ClusterRoleBinding); ok {
						return errTestClient
					}
					return nil
				})
			},
			wantErr:         "failed to apply clusterrolebinding",
			wantExistsCount: 2,
			wantPatchCount:  2,
		},
		{
			name: "role patch error propagates",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					return false, nil
				})
				m.PatchCalls(func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					if _, ok := obj.(*rbacv1.Role); ok {
						return errTestClient
					}
					return nil
				})
			},
			wantErr:         "failed to apply role",
			wantExistsCount: 3,
			wantPatchCount:  3,
		},
		{
			name: "rolebinding patch error propagates",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					return false, nil
				})
				m.PatchCalls(func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					if _, ok := obj.(*rbacv1.RoleBinding); ok {
						return errTestClient
					}
					return nil
				})
			},
			wantErr:         "failed to apply rolebinding",
			wantExistsCount: 4,
			wantPatchCount:  4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testReconciler(t)
			mock := &fakes.FakeCtrlClient{}
			if tt.preReq != nil {
				tt.preReq(r, mock)
			}
			r.CtrlClient = mock

			tmBuilder := tt.tmBuilder
			if tmBuilder == nil {
				tmBuilder = testTrustManager()
			}
			tm := tmBuilder.Build()
			err := r.createOrApplyRBACResources(tm, getResourceLabels(tm), getResourceAnnotations(tm), defaultTrustNamespace)
			assertError(t, err, tt.wantErr)

			if got := mock.ExistsCallCount(); got != tt.wantExistsCount {
				t.Errorf("expected %d Exists calls, got %d", tt.wantExistsCount, got)
			}
			if got := mock.PatchCallCount(); got != tt.wantPatchCount {
				t.Errorf("expected %d Patch calls, got %d", tt.wantPatchCount, got)
			}
		})
	}
}

func TestClusterRoleSecretTargetRules(t *testing.T) {
	tests := []struct {
		name              string
		tm                *trustManagerBuilder
		wantSecretRead    bool
		wantSecretWrite   bool
		wantResourceNames []string
	}{
		{
			name: "no secret rules when policy is Disabled",
			tm:   testTrustManager().WithSecretTargets(v1alpha1.SecretTargetsPolicyDisabled, nil),
		},
		{
			name: "no secret rules when policy is unset",
			tm:   testTrustManager(),
		},
		{
			name: "no secret rules when policy is Custom but no authorized secrets",
			tm:   testTrustManager().WithSecretTargets(v1alpha1.SecretTargetsPolicyCustom, nil),
		},
		{
			name:              "adds secret read and scoped write rules when policy is Custom",
			tm:                testTrustManager().WithSecretTargets(v1alpha1.SecretTargetsPolicyCustom, []string{"my-bundle", "another-secret"}),
			wantSecretRead:    true,
			wantSecretWrite:   true,
			wantResourceNames: []string{"another-secret", "my-bundle"},
		},
		{
			name:              "authorizedSecrets are sorted for deterministic comparison",
			tm:                testTrustManager().WithSecretTargets(v1alpha1.SecretTargetsPolicyCustom, []string{"zzz", "aaa", "mmm"}),
			wantSecretRead:    true,
			wantSecretWrite:   true,
			wantResourceNames: []string{"aaa", "mmm", "zzz"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm := tt.tm.Build()
			cr := getClusterRoleObject(tm.Spec.TrustManagerConfig, testResourceLabels(), testResourceAnnotations())

			hasSecretRead := false
			hasSecretWrite := false
			var gotResourceNames []string
			for _, rule := range cr.Rules {
				if slices.Contains(rule.Resources, "secrets") {
					for _, verb := range rule.Verbs {
						if verb == "get" {
							hasSecretRead = true
						}
						if verb == "create" {
							hasSecretWrite = true
							gotResourceNames = rule.ResourceNames
						}
					}
				}
			}

			if tt.wantSecretRead != hasSecretRead {
				t.Errorf("expected secret read rule=%v, got %v", tt.wantSecretRead, hasSecretRead)
			}
			if tt.wantSecretWrite != hasSecretWrite {
				t.Errorf("expected secret write rule=%v, got %v", tt.wantSecretWrite, hasSecretWrite)
			}
			if tt.wantResourceNames != nil && !reflect.DeepEqual(gotResourceNames, tt.wantResourceNames) {
				t.Errorf("expected resourceNames %v, got %v", tt.wantResourceNames, gotResourceNames)
			}
		})
	}
}

func TestRBACReconciliationWithSecretTargets(t *testing.T) {
	tests := []struct {
		name            string
		tmBuilder       *trustManagerBuilder
		preReq          func(*Reconciler, *fakes.FakeCtrlClient, *v1alpha1.TrustManager)
		wantPatchCount  int
		wantExistsCount int
	}{
		{
			name:      "apply when secretTargets policy changes from Disabled to Custom",
			tmBuilder: testTrustManager().WithSecretTargets(v1alpha1.SecretTargetsPolicyCustom, []string{"bundle-secret"}),
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient, tm *v1alpha1.TrustManager) {
				existsCall := 0
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					existsCall++
					switch existsCall {
					case 1:
						// Existing ClusterRole has no secret rules (was Disabled)
						cr := getClusterRoleObject(v1alpha1.TrustManagerConfig{}, testResourceLabels(), testResourceAnnotations())
						cr.DeepCopyInto(obj.(*rbacv1.ClusterRole))
					case 2:
						crb := getClusterRoleBindingObject(testResourceLabels(), testResourceAnnotations())
						crb.DeepCopyInto(obj.(*rbacv1.ClusterRoleBinding))
					case 3:
						role := getTrustNamespaceRoleObject(testResourceLabels(), testResourceAnnotations(), defaultTrustNamespace)
						role.DeepCopyInto(obj.(*rbacv1.Role))
					case 4:
						rb := getTrustNamespaceRoleBindingObject(testResourceLabels(), testResourceAnnotations(), defaultTrustNamespace)
						rb.DeepCopyInto(obj.(*rbacv1.RoleBinding))
					case 5:
						role := getLeaderElectionRoleObject(testResourceLabels(), testResourceAnnotations())
						role.DeepCopyInto(obj.(*rbacv1.Role))
					case 6:
						rb := getLeaderElectionRoleBindingObject(testResourceLabels(), testResourceAnnotations())
						rb.DeepCopyInto(obj.(*rbacv1.RoleBinding))
					}
					return true, nil
				})
			},
			wantExistsCount: 6,
			wantPatchCount:  1,
		},
		{
			name:      "skip apply when secretTargets rules already match",
			tmBuilder: testTrustManager().WithSecretTargets(v1alpha1.SecretTargetsPolicyCustom, []string{"bundle-secret"}),
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient, tm *v1alpha1.TrustManager) {
				secretTargets := tm.Spec.TrustManagerConfig.SecretTargets
				existsCall := 0
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					existsCall++
					switch existsCall {
					case 1:
						cr := getClusterRoleObject(v1alpha1.TrustManagerConfig{SecretTargets: secretTargets}, testResourceLabels(), testResourceAnnotations())
						cr.DeepCopyInto(obj.(*rbacv1.ClusterRole))
					case 2:
						crb := getClusterRoleBindingObject(testResourceLabels(), testResourceAnnotations())
						crb.DeepCopyInto(obj.(*rbacv1.ClusterRoleBinding))
					case 3:
						role := getTrustNamespaceRoleObject(testResourceLabels(), testResourceAnnotations(), defaultTrustNamespace)
						role.DeepCopyInto(obj.(*rbacv1.Role))
					case 4:
						rb := getTrustNamespaceRoleBindingObject(testResourceLabels(), testResourceAnnotations(), defaultTrustNamespace)
						rb.DeepCopyInto(obj.(*rbacv1.RoleBinding))
					case 5:
						role := getLeaderElectionRoleObject(testResourceLabels(), testResourceAnnotations())
						role.DeepCopyInto(obj.(*rbacv1.Role))
					case 6:
						rb := getLeaderElectionRoleBindingObject(testResourceLabels(), testResourceAnnotations())
						rb.DeepCopyInto(obj.(*rbacv1.RoleBinding))
					}
					return true, nil
				})
			},
			wantExistsCount: 6,
			wantPatchCount:  0,
		},
		{
			name:      "apply when authorizedSecrets list changes",
			tmBuilder: testTrustManager().WithSecretTargets(v1alpha1.SecretTargetsPolicyCustom, []string{"new-secret"}),
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient, tm *v1alpha1.TrustManager) {
				oldSecretTargets := v1alpha1.SecretTargetsConfig{
					Policy:            v1alpha1.SecretTargetsPolicyCustom,
					AuthorizedSecrets: []string{"old-secret"},
				}
				existsCall := 0
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					existsCall++
					switch existsCall {
					case 1:
						cr := getClusterRoleObject(v1alpha1.TrustManagerConfig{SecretTargets: oldSecretTargets}, testResourceLabels(), testResourceAnnotations())
						cr.DeepCopyInto(obj.(*rbacv1.ClusterRole))
					case 2:
						crb := getClusterRoleBindingObject(testResourceLabels(), testResourceAnnotations())
						crb.DeepCopyInto(obj.(*rbacv1.ClusterRoleBinding))
					case 3:
						role := getTrustNamespaceRoleObject(testResourceLabels(), testResourceAnnotations(), defaultTrustNamespace)
						role.DeepCopyInto(obj.(*rbacv1.Role))
					case 4:
						rb := getTrustNamespaceRoleBindingObject(testResourceLabels(), testResourceAnnotations(), defaultTrustNamespace)
						rb.DeepCopyInto(obj.(*rbacv1.RoleBinding))
					case 5:
						role := getLeaderElectionRoleObject(testResourceLabels(), testResourceAnnotations())
						role.DeepCopyInto(obj.(*rbacv1.Role))
					case 6:
						rb := getLeaderElectionRoleBindingObject(testResourceLabels(), testResourceAnnotations())
						rb.DeepCopyInto(obj.(*rbacv1.RoleBinding))
					}
					return true, nil
				})
			},
			wantExistsCount: 6,
			wantPatchCount:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testReconciler(t)
			mock := &fakes.FakeCtrlClient{}
			tm := tt.tmBuilder.Build()
			if tt.preReq != nil {
				tt.preReq(r, mock, tm)
			}
			r.CtrlClient = mock

			err := r.createOrApplyRBACResources(tm, getResourceLabels(tm), getResourceAnnotations(tm), defaultTrustNamespace)
			assertError(t, err, "")

			if got := mock.ExistsCallCount(); got != tt.wantExistsCount {
				t.Errorf("expected %d Exists calls, got %d", tt.wantExistsCount, got)
			}
			if got := mock.PatchCallCount(); got != tt.wantPatchCount {
				t.Errorf("expected %d Patch calls, got %d", tt.wantPatchCount, got)
			}
		})
	}
}

func assertSubjects(t *testing.T, subjects []rbacv1.Subject, expectedName, expectedNamespace string) {
	t.Helper()
	if len(subjects) == 0 {
		t.Fatal("expected at least one subject")
	}
	found := false
	for _, s := range subjects {
		if s.Kind == "ServiceAccount" {
			found = true
			if s.Name != expectedName {
				t.Errorf("expected subject name %q, got %q", expectedName, s.Name)
			}
			if s.Namespace != expectedNamespace {
				t.Errorf("expected subject namespace %q, got %q", expectedNamespace, s.Namespace)
			}
		}
	}
	if !found {
		t.Error("expected to find a ServiceAccount subject")
	}
}

func TestClusterRoleTargetNamespacesRules(t *testing.T) {
	tests := []struct {
		name               string
		tm                 *trustManagerBuilder
		wantConfigMapWrite bool
		wantEvents         bool
		wantSecretRead     bool
		wantSecretWrite    bool
	}{
		{
			name:               "cluster-wide ConfigMap write and events when targetNamespaces is unset",
			tm:                 testTrustManager(),
			wantConfigMapWrite: true,
			wantEvents:         true,
		},
		{
			name: "drops cluster-wide ConfigMap write and events when targetNamespaces is set",
			tm:   testTrustManager().WithTargetNamespaces("app-ns"),
		},
		{
			name:               "secret rules stay on ClusterRole when targetNamespaces is unset and secretTargets is Custom",
			tm:                 testTrustManager().WithSecretTargets(v1alpha1.SecretTargetsPolicyCustom, []string{"bundle-secret"}),
			wantConfigMapWrite: true,
			wantEvents:         true,
			wantSecretRead:     true,
			wantSecretWrite:    true,
		},
		{
			name: "secret rules move off ClusterRole when targetNamespaces is set and secretTargets is Custom",
			tm: testTrustManager().
				WithTargetNamespaces("app-ns").
				WithSecretTargets(v1alpha1.SecretTargetsPolicyCustom, []string{"bundle-secret"}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm := tt.tm.Build()
			cr := getClusterRoleObject(tm.Spec.TrustManagerConfig, testResourceLabels(), testResourceAnnotations())

			if got := hasResourceVerb(cr.Rules, "configmaps", "create"); got != tt.wantConfigMapWrite {
				t.Errorf("expected configmap write=%v, got %v", tt.wantConfigMapWrite, got)
			}
			if got := hasResourceVerb(cr.Rules, "events", "create"); got != tt.wantEvents {
				t.Errorf("expected events write=%v, got %v", tt.wantEvents, got)
			}
			if got := hasResourceVerb(cr.Rules, "secrets", "get"); got != tt.wantSecretRead {
				t.Errorf("expected secret read=%v, got %v", tt.wantSecretRead, got)
			}
			if got := hasResourceVerb(cr.Rules, "secrets", "create"); got != tt.wantSecretWrite {
				t.Errorf("expected secret write=%v, got %v", tt.wantSecretWrite, got)
			}
		})
	}
}

func TestTargetNamespaceRoleRules(t *testing.T) {
	t.Run("includes ConfigMap write and events", func(t *testing.T) {
		role := getTargetNamespaceRoleObject("app-ns", v1alpha1.SecretTargetsConfig{}, testResourceLabels(), testResourceAnnotations())
		if !hasResourceVerb(role.Rules, "configmaps", "create") {
			t.Error("expected configmap write on target namespace Role")
		}
		if !hasResourceVerb(role.Rules, "events", "create") {
			t.Error("expected events write on target namespace Role")
		}
		if hasResourceVerb(role.Rules, "secrets", "create") {
			t.Error("expected no secret write on target namespace Role when secretTargets is Disabled")
		}
	})

	t.Run("includes secret rules when secretTargets is Custom", func(t *testing.T) {
		secretTargets := v1alpha1.SecretTargetsConfig{
			Policy:            v1alpha1.SecretTargetsPolicyCustom,
			AuthorizedSecrets: []string{"bundle-secret"},
		}
		role := getTargetNamespaceRoleObject("app-ns", secretTargets, testResourceLabels(), testResourceAnnotations())
		if !hasResourceVerb(role.Rules, "secrets", "get") {
			t.Error("expected secret read on target namespace Role")
		}
		if !hasResourceVerb(role.Rules, "secrets", "create") {
			t.Error("expected secret write on target namespace Role")
		}
	})
}

func TestTargetRBACNamespaces(t *testing.T) {
	tests := []struct {
		name           string
		tm             *trustManagerBuilder
		trustNamespace string
		want           []string
	}{
		{
			name:           "empty when targetNamespaces is unset",
			tm:             testTrustManager(),
			trustNamespace: defaultTrustNamespace,
		},
		{
			name:           "includes listed namespaces and trust namespace, sorted unique",
			tm:             testTrustManager().WithTargetNamespaces("zeta-ns", "app-ns"),
			trustNamespace: defaultTrustNamespace,
			want:           []string{"app-ns", defaultTrustNamespace, "zeta-ns"},
		},
		{
			name:           "does not duplicate trust namespace when it is already listed",
			tm:             testTrustManager().WithTargetNamespaces(defaultTrustNamespace, "app-ns"),
			trustNamespace: defaultTrustNamespace,
			want:           []string{"app-ns", defaultTrustNamespace},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := targetRBACNamespaces(tt.tm.Build(), tt.trustNamespace)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func TestRBACReconciliationWithTargetNamespaces(t *testing.T) {
	targetNS := "app-ns"
	// Sorted: app-ns, cert-manager
	targetNamespaces := []string{targetNS, defaultTrustNamespace}

	tests := []struct {
		name            string
		preReq          func(*Reconciler, *fakes.FakeCtrlClient)
		wantErr         string
		wantExistsCount int
		wantPatchCount  int
		wantDeleteCount int
	}{
		{
			name: "successful apply of cluster RBAC plus per-namespace target Roles",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					return false, nil
				})
			},
			wantExistsCount: 10,
			wantPatchCount:  10,
		},
		{
			name: "skip apply when all RBAC resources including target Roles match desired",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				tm := testTrustManager().WithTargetNamespaces(targetNS).Build()
				config := tm.Spec.TrustManagerConfig
				existsCall := 0
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					existsCall++
					switch existsCall {
					case 1:
						cr := getClusterRoleObject(config, testResourceLabels(), testResourceAnnotations())
						cr.DeepCopyInto(obj.(*rbacv1.ClusterRole))
					case 2:
						crb := getClusterRoleBindingObject(testResourceLabels(), testResourceAnnotations())
						crb.DeepCopyInto(obj.(*rbacv1.ClusterRoleBinding))
					case 3:
						role := getTrustNamespaceRoleObject(testResourceLabels(), testResourceAnnotations(), defaultTrustNamespace)
						role.DeepCopyInto(obj.(*rbacv1.Role))
					case 4:
						rb := getTrustNamespaceRoleBindingObject(testResourceLabels(), testResourceAnnotations(), defaultTrustNamespace)
						rb.DeepCopyInto(obj.(*rbacv1.RoleBinding))
					case 5:
						role := getLeaderElectionRoleObject(testResourceLabels(), testResourceAnnotations())
						role.DeepCopyInto(obj.(*rbacv1.Role))
					case 6:
						rb := getLeaderElectionRoleBindingObject(testResourceLabels(), testResourceAnnotations())
						rb.DeepCopyInto(obj.(*rbacv1.RoleBinding))
					case 7:
						role := getTargetNamespaceRoleObject(targetNamespaces[0], config.SecretTargets, testResourceLabels(), testResourceAnnotations())
						role.DeepCopyInto(obj.(*rbacv1.Role))
					case 8:
						rb := getTargetNamespaceRoleBindingObject(targetNamespaces[0], testResourceLabels(), testResourceAnnotations())
						rb.DeepCopyInto(obj.(*rbacv1.RoleBinding))
					case 9:
						role := getTargetNamespaceRoleObject(targetNamespaces[1], config.SecretTargets, testResourceLabels(), testResourceAnnotations())
						role.DeepCopyInto(obj.(*rbacv1.Role))
					case 10:
						rb := getTargetNamespaceRoleBindingObject(targetNamespaces[1], testResourceLabels(), testResourceAnnotations())
						rb.DeepCopyInto(obj.(*rbacv1.RoleBinding))
					}
					return true, nil
				})
			},
			wantExistsCount: 10,
			wantPatchCount:  0,
		},
		{
			name: "deletes leftover target Role and RoleBinding outside the desired set",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					return false, nil
				})
				m.ListCalls(func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
					switch l := list.(type) {
					case *rbacv1.RoleList:
						l.Items = []rbacv1.Role{
							{
								ObjectMeta: metav1.ObjectMeta{Name: trustManagerTargetRoleName, Namespace: "stale-ns"},
							},
							{
								ObjectMeta: metav1.ObjectMeta{Name: trustManagerRoleName, Namespace: defaultTrustNamespace},
							},
							{
								ObjectMeta: metav1.ObjectMeta{Name: trustManagerTargetRoleName, Namespace: targetNS},
							},
						}
					case *rbacv1.RoleBindingList:
						l.Items = []rbacv1.RoleBinding{
							{
								ObjectMeta: metav1.ObjectMeta{Name: trustManagerTargetRoleBindingName, Namespace: "stale-ns"},
							},
							{
								ObjectMeta: metav1.ObjectMeta{Name: trustManagerRoleBindingName, Namespace: defaultTrustNamespace},
							},
						}
					}
					return nil
				})
			},
			wantExistsCount: 10,
			wantPatchCount:  10,
			wantDeleteCount: 2,
		},
		{
			name: "target namespace role patch error propagates",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					return false, nil
				})
				m.PatchCalls(func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					if role, ok := obj.(*rbacv1.Role); ok && role.Name == trustManagerTargetRoleName {
						return errTestClient
					}
					return nil
				})
			},
			wantErr:         "failed to apply target namespace role",
			wantExistsCount: 7,
			wantPatchCount:  7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testReconciler(t)
			mock := &fakes.FakeCtrlClient{}
			if tt.preReq != nil {
				tt.preReq(r, mock)
			}
			r.CtrlClient = mock

			tm := testTrustManager().WithTargetNamespaces(targetNS).Build()
			err := r.createOrApplyRBACResources(tm, getResourceLabels(tm), getResourceAnnotations(tm), defaultTrustNamespace)
			assertError(t, err, tt.wantErr)

			if got := mock.ExistsCallCount(); got != tt.wantExistsCount {
				t.Errorf("expected %d Exists calls, got %d", tt.wantExistsCount, got)
			}
			if got := mock.PatchCallCount(); got != tt.wantPatchCount {
				t.Errorf("expected %d Patch calls, got %d", tt.wantPatchCount, got)
			}
			if got := mock.DeleteCallCount(); got != tt.wantDeleteCount {
				t.Errorf("expected %d Delete calls, got %d", tt.wantDeleteCount, got)
			}
		})
	}
}

func hasResourceVerb(rules []rbacv1.PolicyRule, resource, verb string) bool {
	for _, rule := range rules {
		if slices.Contains(rule.Resources, resource) && slices.Contains(rule.Verbs, verb) {
			return true
		}
	}
	return false
}
