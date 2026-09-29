package trustmanager

import (
	"context"
	"fmt"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	policyv1alpha1 "github.com/cert-manager/approver-policy/pkg/apis/policy/v1alpha1"
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common/fakes"
)

func TestCertificateRequestPolicyObject(t *testing.T) {
	labels := testResourceLabels()
	annotations := map[string]string{"user-annotation": "test-value"}
	obj := getCertificateRequestPolicyObject(labels, annotations)

	if obj.GetName() != trustManagerCertificateRequestPolicyName {
		t.Errorf("expected name %q, got %q", trustManagerCertificateRequestPolicyName, obj.GetName())
	}
	expectedGVK := policyv1alpha1.SchemeGroupVersion.WithKind(policyv1alpha1.CertificateRequestPolicyKind)
	if obj.GroupVersionKind() != expectedGVK {
		t.Errorf("expected gvk %s, got %s", expectedGVK, obj.GroupVersionKind())
	}
	if obj.GetLabels()["app"] != trustManagerCommonName {
		t.Errorf("expected app label %q, got %q", trustManagerCommonName, obj.GetLabels()["app"])
	}
	if obj.GetAnnotations()["user-annotation"] != "test-value" {
		t.Errorf("expected user annotation to be preserved")
	}

	expectedDNS := trustManagerServiceName + "." + operandNamespace + ".svc"
	if obj.Spec.Allowed == nil || obj.Spec.Allowed.CommonName == nil || obj.Spec.Allowed.CommonName.Value == nil || *obj.Spec.Allowed.CommonName.Value != expectedDNS {
		t.Errorf("expected commonName %q, got %#v", expectedDNS, obj.Spec.Allowed)
	}
	if obj.Spec.Allowed.Usages == nil || len(*obj.Spec.Allowed.Usages) != 2 ||
		(*obj.Spec.Allowed.Usages)[0] != certmanagerv1.UsageDigitalSignature ||
		(*obj.Spec.Allowed.Usages)[1] != certmanagerv1.UsageKeyEncipherment {
		t.Errorf("expected usages [digital signature, key encipherment], got %#v", obj.Spec.Allowed.Usages)
	}
}

func TestPolicyClusterRoleBindingSubjects(t *testing.T) {
	binding := getPolicyClusterRoleBindingObject(testResourceLabels(), testResourceAnnotations())
	if binding.Name != trustManagerPolicyClusterRoleBindingName {
		t.Errorf("expected name %q, got %q", trustManagerPolicyClusterRoleBindingName, binding.Name)
	}
	if binding.RoleRef.Name != trustManagerPolicyClusterRoleName {
		t.Errorf("expected roleRef %q, got %q", trustManagerPolicyClusterRoleName, binding.RoleRef.Name)
	}
	if len(binding.Subjects) != 1 {
		t.Fatalf("expected 1 subject, got %d", len(binding.Subjects))
	}
	if binding.Subjects[0].Name != certManagerControllerServiceAccountName || binding.Subjects[0].Namespace != operandNamespace {
		t.Errorf("expected subject %s/%s, got %s/%s", operandNamespace, certManagerControllerServiceAccountName, binding.Subjects[0].Namespace, binding.Subjects[0].Name)
	}
}

func TestApproverPolicyReconciliation(t *testing.T) {
	notFound := apierrors.NewNotFound(schema.GroupResource{Group: "policy.cert-manager.io", Resource: "certificaterequestpolicies"}, trustManagerCertificateRequestPolicyName)
	tests := []struct {
		name            string
		tmBuilder       *trustManagerBuilder
		preReq          func(*Reconciler, *fakes.FakeCtrlClient)
		wantErr         string
		wantExistsCount int
		wantPatchCount  int
		wantDeleteCount int
		wantDeleted     []string
	}{
		{
			name:            "deletes approver policy resources when disabled",
			wantDeleteCount: 3,
			wantDeleted: []string{
				trustManagerPolicyClusterRoleBindingName,
				trustManagerPolicyClusterRoleName,
				trustManagerCertificateRequestPolicyName,
			},
		},
		{
			name: "treats already-absent approver policy resources as success",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.DeleteCalls(func(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
					return notFound
				})
			},
			wantDeleteCount: 3,
			wantDeleted: []string{
				trustManagerPolicyClusterRoleBindingName,
				trustManagerPolicyClusterRoleName,
				trustManagerCertificateRequestPolicyName,
			},
		},
		{
			name: "ignores missing CertificateRequestPolicy CRD when disabled",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.DeleteCalls(func(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
					switch obj.(type) {
					case *policyv1alpha1.CertificateRequestPolicy:
						return &meta.NoKindMatchError{GroupKind: schema.GroupKind{Group: "policy.cert-manager.io", Kind: "CertificateRequestPolicy"}}
					default:
						return notFound
					}
				})
			},
			wantDeleteCount: 3,
			wantDeleted: []string{
				trustManagerPolicyClusterRoleBindingName,
				trustManagerPolicyClusterRoleName,
				trustManagerCertificateRequestPolicyName,
			},
		},
		{
			name: "returns error when deleting approver policy RBAC fails",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.DeleteCalls(func(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
					if _, ok := obj.(*rbacv1.ClusterRoleBinding); ok {
						return fmt.Errorf("delete failed")
					}
					return nil
				})
			},
			wantErr:         "failed to delete clusterrolebinding",
			wantDeleteCount: 1,
			wantDeleted:     []string{trustManagerPolicyClusterRoleBindingName},
		},
		{
			name:      "apply policy resources when enabled and missing",
			tmBuilder: testTrustManager().WithApproverPolicy(v1alpha1.Enabled),
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					return false, nil
				})
			},
			wantExistsCount: 3,
			wantPatchCount:  3,
		},
		{
			name:      "skip apply when existing policy resources match",
			tmBuilder: testTrustManager().WithApproverPolicy(v1alpha1.Enabled),
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					switch dst := obj.(type) {
					case *policyv1alpha1.CertificateRequestPolicy:
						getCertificateRequestPolicyObject(testResourceLabels(), testResourceAnnotations()).DeepCopyInto(dst)
					case *rbacv1.ClusterRole:
						getPolicyClusterRoleObject(testResourceLabels(), testResourceAnnotations()).DeepCopyInto(dst)
					case *rbacv1.ClusterRoleBinding:
						getPolicyClusterRoleBindingObject(testResourceLabels(), testResourceAnnotations()).DeepCopyInto(dst)
					}
					return true, nil
				})
			},
			wantExistsCount: 3,
			wantPatchCount:  0,
		},
		{
			name:      "apply when certificate request policy spec drifted",
			tmBuilder: testTrustManager().WithApproverPolicy(v1alpha1.Enabled),
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					switch dst := obj.(type) {
					case *policyv1alpha1.CertificateRequestPolicy:
						existing := getCertificateRequestPolicyObject(testResourceLabels(), testResourceAnnotations())
						existing.Spec.Selector.IssuerRef.Name = ptr.To("wrong")
						existing.DeepCopyInto(dst)
					case *rbacv1.ClusterRole:
						getPolicyClusterRoleObject(testResourceLabels(), testResourceAnnotations()).DeepCopyInto(dst)
					case *rbacv1.ClusterRoleBinding:
						getPolicyClusterRoleBindingObject(testResourceLabels(), testResourceAnnotations()).DeepCopyInto(dst)
					}
					return true, nil
				})
			},
			wantExistsCount: 3,
			wantPatchCount:  1,
		},
		{
			name:      "reports missing CertificateRequestPolicy CRD",
			tmBuilder: testTrustManager().WithApproverPolicy(v1alpha1.Enabled),
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					return false, &meta.NoKindMatchError{GroupKind: schema.GroupKind{Group: "policy.cert-manager.io", Kind: "CertificateRequestPolicy"}}
				})
			},
			wantErr:         "CertificateRequestPolicy CRD is not installed",
			wantExistsCount: 1,
			wantPatchCount:  0,
			wantDeleteCount: 0,
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
			err := r.createOrApplyApproverPolicyResources(tm, getResourceLabels(tm), getResourceAnnotations(tm))
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
			for i, name := range tt.wantDeleted {
				if i >= mock.DeleteCallCount() {
					break
				}
				_, obj, _ := mock.DeleteArgsForCall(i)
				if obj.GetName() != name {
					t.Errorf("delete call %d: expected name %q, got %q", i, name, obj.GetName())
				}
			}
		})
	}
}
