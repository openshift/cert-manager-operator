package trustmanager

import (
	"context"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

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
	if obj.GroupVersionKind() != certificateRequestPolicyGVK {
		t.Errorf("expected gvk %s, got %s", certificateRequestPolicyGVK, obj.GroupVersionKind())
	}
	if obj.GetLabels()["app"] != trustManagerCommonName {
		t.Errorf("expected app label %q, got %q", trustManagerCommonName, obj.GetLabels()["app"])
	}
	if obj.GetAnnotations()["user-annotation"] != "test-value" {
		t.Errorf("expected user annotation to be preserved")
	}

	expectedDNS := trustManagerServiceName + "." + operandNamespace + ".svc"
	cn, found, err := unstructured.NestedString(obj.Object, "spec", "allowed", "commonName", "value")
	if err != nil || !found || cn != expectedDNS {
		t.Errorf("expected commonName %q, got %q found=%v err=%v", expectedDNS, cn, found, err)
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
	tests := []struct {
		name            string
		tmBuilder       *trustManagerBuilder
		preReq          func(*Reconciler, *fakes.FakeCtrlClient)
		wantErr         string
		wantExistsCount int
		wantPatchCount  int
	}{
		{
			name:            "skip when approver policy is disabled",
			wantExistsCount: 0,
			wantPatchCount:  0,
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
					case *unstructured.Unstructured:
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
					case *unstructured.Unstructured:
						existing := getCertificateRequestPolicyObject(testResourceLabels(), testResourceAnnotations())
						_ = unstructured.SetNestedStringMap(existing.Object, map[string]string{"name": "wrong"}, "spec", "selector", "issuerRef")
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
		})
	}
}
