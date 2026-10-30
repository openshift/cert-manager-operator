package trustmanager

import (
	"context"
	"fmt"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	policyv1alpha1 "github.com/cert-manager/approver-policy/pkg/apis/policy/v1alpha1"
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanagermetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common/fakes"
)

func TestCertificateRequestPolicyObject(t *testing.T) {
	labels := testResourceLabels()
	annotations := map[string]string{"user-annotation": "test-value"}
	obj := getCertificateRequestPolicyObject(v1alpha1.TrustManagerCertConfig{}, labels, annotations)

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
	assertPolicyIssuerRef(t, obj, certmanagermetav1.ObjectReference{
		Name: trustManagerIssuerName, Kind: "Issuer", Group: "cert-manager.io",
	})
}

func TestCertificateRequestPolicyIssuerSelector(t *testing.T) {
	tests := []struct {
		name string
		ref  *certmanagermetav1.ObjectReference
		want certmanagermetav1.ObjectReference
	}{
		{
			name: "defaults to the operator self-signed issuer",
			want: certmanagermetav1.ObjectReference{Name: trustManagerIssuerName, Kind: "Issuer", Group: "cert-manager.io"},
		},
		{
			name: "uses a custom issuer",
			ref:  &certmanagermetav1.ObjectReference{Name: "corp-ca", Kind: "Issuer", Group: "cert-manager.io"},
			want: certmanagermetav1.ObjectReference{Name: "corp-ca", Kind: "Issuer", Group: "cert-manager.io"},
		},
		{
			name: "uses a custom cluster issuer",
			ref:  &certmanagermetav1.ObjectReference{Name: "corp-ca", Kind: "ClusterIssuer", Group: "cert-manager.io"},
			want: certmanagermetav1.ObjectReference{Name: "corp-ca", Kind: "ClusterIssuer", Group: "cert-manager.io"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := getCertificateRequestPolicyObject(v1alpha1.TrustManagerCertConfig{IssuerRef: tt.ref}, nil, nil)
			assertPolicyIssuerRef(t, obj, tt.want)
		})
	}
}

func assertPolicyIssuerRef(t *testing.T, obj *policyv1alpha1.CertificateRequestPolicy, want certmanagermetav1.ObjectReference) {
	t.Helper()
	got := obj.Spec.Selector.IssuerRef
	if got == nil || got.Name == nil || got.Kind == nil || got.Group == nil ||
		*got.Name != want.Name || *got.Kind != want.Kind || *got.Group != want.Group {
		t.Errorf("selector issuerRef = %#v, want %#v", got, want)
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

func markOperatorManaged(obj client.Object) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels["app.kubernetes.io/managed-by"] = controllerDefaultResourceLabels["app.kubernetes.io/managed-by"]
	obj.SetLabels(labels)
}

func TestApproverPolicyReconciliation(t *testing.T) {
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
			name:            "skips approver policy cleanup when status does not record a policy",
			wantExistsCount: 0,
			wantDeleteCount: 0,
		},
		{
			name:      "deletes managed approver policy resources recorded in status",
			tmBuilder: testTrustManager().WithCertificateRequestPolicyStatus(trustManagerCertificateRequestPolicyName),
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					markOperatorManaged(obj)
					return true, nil
				})
			},
			wantExistsCount: 3,
			wantDeleteCount: 3,
			wantDeleted: []string{
				trustManagerPolicyClusterRoleBindingName,
				trustManagerPolicyClusterRoleName,
				trustManagerCertificateRequestPolicyName,
			},
		},
		{
			name:            "skips delete when recorded approver policy resources are already absent",
			tmBuilder:       testTrustManager().WithCertificateRequestPolicyStatus(trustManagerCertificateRequestPolicyName),
			wantExistsCount: 3,
			wantDeleteCount: 0,
		},
		{
			name:      "leaves an existing policy that this operator does not manage",
			tmBuilder: testTrustManager().WithCertificateRequestPolicyStatus(trustManagerCertificateRequestPolicyName),
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					obj.SetLabels(map[string]string{"app.kubernetes.io/managed-by": "helm"})
					return true, nil
				})
			},
			wantExistsCount: 3,
			wantDeleteCount: 0,
		},
		{
			name:      "ignores missing CertificateRequestPolicy CRD when disabled",
			tmBuilder: testTrustManager().WithCertificateRequestPolicyStatus(trustManagerCertificateRequestPolicyName),
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					if _, ok := obj.(*policyv1alpha1.CertificateRequestPolicy); ok {
						return false, &meta.NoKindMatchError{GroupKind: schema.GroupKind{Group: "policy.cert-manager.io", Kind: "CertificateRequestPolicy"}}
					}
					return false, nil
				})
			},
			wantExistsCount: 3,
			wantDeleteCount: 0,
		},
		{
			name:      "returns error when deleting approver policy RBAC fails",
			tmBuilder: testTrustManager().WithCertificateRequestPolicyStatus(trustManagerCertificateRequestPolicyName),
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					markOperatorManaged(obj)
					return true, nil
				})
				m.DeleteCalls(func(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
					if _, ok := obj.(*rbacv1.ClusterRoleBinding); ok {
						return fmt.Errorf("delete failed")
					}
					return nil
				})
			},
			wantErr:         "failed to delete clusterrolebinding",
			wantExistsCount: 1,
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
						getCertificateRequestPolicyObject(v1alpha1.TrustManagerCertConfig{}, testResourceLabels(), testResourceAnnotations()).DeepCopyInto(dst)
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
						existing := getCertificateRequestPolicyObject(v1alpha1.TrustManagerCertConfig{}, testResourceLabels(), testResourceAnnotations())
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
