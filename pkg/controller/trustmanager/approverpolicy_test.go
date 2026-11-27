package trustmanager

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

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
	// The generated policy must require the webhook identity and permit only its
	// default key usages, including after a Helm chart update.
	assert.Equal(t, &policyv1alpha1.CertificateRequestPolicyAllowed{
		CommonName: &policyv1alpha1.CertificateRequestPolicyAllowedString{
			Value:    ptr.To(expectedDNS),
			Required: ptr.To(true),
		},
		DNSNames: &policyv1alpha1.CertificateRequestPolicyAllowedStringSlice{
			Values:   ptr.To([]string{expectedDNS}),
			Required: ptr.To(true),
		},
		Usages: ptr.To([]certmanagerv1.KeyUsage{certmanagerv1.UsageDigitalSignature, certmanagerv1.UsageKeyEncipherment}),
	}, obj.Spec.Allowed)
	assertPolicyIssuerRef(t, obj, certmanagermetav1.IssuerReference{
		Name: trustManagerIssuerName, Kind: "Issuer", Group: "cert-manager.io",
	})
	assertPolicyNamespaceSelector(t, obj)
}

func TestCertificateRequestPolicyIssuerSelector(t *testing.T) {
	tests := []struct {
		name string
		ref  *certmanagermetav1.IssuerReference
		want certmanagermetav1.IssuerReference
	}{
		{
			name: "defaults to the operator self-signed issuer",
			want: certmanagermetav1.IssuerReference{Name: trustManagerIssuerName, Kind: "Issuer", Group: "cert-manager.io"},
		},
		{
			name: "uses a custom issuer",
			ref:  &certmanagermetav1.IssuerReference{Name: "corp-ca", Kind: "Issuer", Group: "cert-manager.io"},
			want: certmanagermetav1.IssuerReference{Name: "corp-ca", Kind: "Issuer", Group: "cert-manager.io"},
		},
		{
			name: "defaults kind and group for a custom issuer",
			ref:  &certmanagermetav1.IssuerReference{Name: "corp-ca"},
			want: certmanagermetav1.IssuerReference{Name: "corp-ca", Kind: "Issuer", Group: "cert-manager.io"},
		},
		{
			name: "uses a custom cluster issuer",
			ref:  &certmanagermetav1.IssuerReference{Name: "corp-ca", Kind: "ClusterIssuer", Group: "cert-manager.io"},
			want: certmanagermetav1.IssuerReference{Name: "corp-ca", Kind: "ClusterIssuer", Group: "cert-manager.io"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := getCertificateRequestPolicyObject(v1alpha1.TrustManagerCertConfig{IssuerRef: tt.ref}, nil, nil)
			assertPolicyIssuerRef(t, obj, tt.want)
			assertPolicyNamespaceSelector(t, obj)
		})
	}
}

func assertPolicyIssuerRef(t *testing.T, obj *policyv1alpha1.CertificateRequestPolicy, want certmanagermetav1.IssuerReference) {
	t.Helper()
	got := obj.Spec.Selector.IssuerRef
	if got == nil || got.Name == nil || got.Kind == nil || got.Group == nil ||
		*got.Name != want.Name || *got.Kind != want.Kind || *got.Group != want.Group {
		t.Errorf("selector issuerRef = %#v, want %#v", got, want)
	}
}

func assertPolicyNamespaceSelector(t *testing.T, obj *policyv1alpha1.CertificateRequestPolicy) {
	t.Helper()
	ns := obj.Spec.Selector.Namespace
	if ns == nil || len(ns.MatchNames) != 1 || ns.MatchNames[0] != operandNamespace {
		t.Errorf("selector namespace matchNames = %#v, want [%q]", ns, operandNamespace)
	}
}

func TestPolicyClusterRoleObject(t *testing.T) {
	labels := testResourceLabels()
	annotations := map[string]string{"user-annotation": "test-value"}
	role := getPolicyClusterRoleObject(labels, annotations)

	assert.Equal(t, trustManagerPolicyClusterRoleName, role.Name)
	assert.Equal(t, rbacv1.SchemeGroupVersion.WithKind("ClusterRole"), role.GroupVersionKind())
	assert.Equal(t, labels, role.Labels)
	assert.Equal(t, annotations, role.Annotations)
	assert.Nil(t, role.AggregationRule)
	// Chart updates must not grant access beyond using this one policy.
	assert.Equal(t, []rbacv1.PolicyRule{{
		APIGroups:     []string{"policy.cert-manager.io"},
		Resources:     []string{"certificaterequestpolicies"},
		Verbs:         []string{"use"},
		ResourceNames: []string{trustManagerCertificateRequestPolicyName},
	}}, role.Rules)
}

func TestPolicyClusterRoleBindingObject(t *testing.T) {
	labels := testResourceLabels()
	annotations := map[string]string{"user-annotation": "test-value"}
	binding := getPolicyClusterRoleBindingObject(labels, annotations)

	assert.Equal(t, trustManagerPolicyClusterRoleBindingName, binding.Name)
	assert.Equal(t, rbacv1.SchemeGroupVersion.WithKind("ClusterRoleBinding"), binding.GroupVersionKind())
	assert.Equal(t, labels, binding.Labels)
	assert.Equal(t, annotations, binding.Annotations)
	assert.Equal(t, rbacv1.RoleRef{
		APIGroup: rbacv1.GroupName,
		Kind:     "ClusterRole",
		Name:     trustManagerPolicyClusterRoleName,
	}, binding.RoleRef)
	assert.Equal(t, []rbacv1.Subject{{
		Kind:      "ServiceAccount",
		Name:      certManagerControllerServiceAccountName,
		Namespace: operandNamespace,
	}}, binding.Subjects)
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
		name                 string
		tmBuilder            *trustManagerBuilder
		autoApproverDisabled bool
		certManagerReadError error
		preReq               func(*Reconciler, *fakes.FakeCtrlClient)
		wantErr              string
		wantExistsCount      int
		wantPatchCount       int
		wantDeleteCount      int
		wantDeleted          []string
	}{
		{
			name:                 "reports failed binding apply",
			autoApproverDisabled: true,
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.PatchCalls(func(_ context.Context, obj client.Object, _ client.Patch, _ ...client.PatchOption) error {
					if _, ok := obj.(*rbacv1.ClusterRoleBinding); ok {
						return errTestClient
					}
					return nil
				})
			},
			wantExistsCount: 3,
			wantPatchCount:  3,
			wantErr:         "failed to apply clusterrolebinding",
		},
		{
			name:      "reports failed policy deletion",
			tmBuilder: testTrustManager(),
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(_ context.Context, _ client.ObjectKey, obj client.Object) (bool, error) {
					markOperatorManaged(obj)
					return true, nil
				})
				m.DeleteCalls(func(_ context.Context, obj client.Object, _ ...client.DeleteOption) error {
					if _, ok := obj.(*policyv1alpha1.CertificateRequestPolicy); ok {
						return errTestClient
					}
					return nil
				})
			},
			wantExistsCount: 3,
			wantDeleteCount: 3,
			wantErr:         "failed to delete certificaterequestpolicy",
		},
		{
			name:                 "missing CertManager runs cleanup without creating resources",
			certManagerReadError: apierrors.NewNotFound(v1alpha1.Resource("certmanagers"), "cluster"),
			wantExistsCount:      3,
		},
		{
			name:                 "CertManager read failure leaves approval resources unchanged",
			certManagerReadError: errTestClient,
			wantErr:              "failed to read CertManager auto-approver status",
		},
		{
			name:                 "Unmanaged skips CertManager and all approval resource checks",
			tmBuilder:            testTrustManager().WithApproverPolicy(v1alpha1.ApproverPolicyManagementStateUnmanaged),
			autoApproverDisabled: true,
			certManagerReadError: errTestClient,
		},
		{
			name:      "Unmanaged leaves an existing policy untouched after approval returns",
			tmBuilder: testTrustManager().WithApproverPolicy(v1alpha1.ApproverPolicyManagementStateUnmanaged),
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsReturns(true, nil)
			},
		},
		{
			name:            "skips delete when approver policy resources are already absent",
			tmBuilder:       testTrustManager().WithApproverPolicy(v1alpha1.ApproverPolicyManagementStateManaged),
			wantExistsCount: 3,
			wantDeleteCount: 0,
		},
		{
			name:      "deletes operator-managed approver policy resources when built-in approval returns",
			tmBuilder: testTrustManager().WithApproverPolicy(v1alpha1.ApproverPolicyManagementStateManaged),
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
			name:      "leaves an existing policy that this operator does not manage",
			tmBuilder: testTrustManager().WithApproverPolicy(v1alpha1.ApproverPolicyManagementStateManaged),
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
			name:      "ignores missing CertificateRequestPolicy CRD when built-in approval is active",
			tmBuilder: testTrustManager().WithApproverPolicy(v1alpha1.ApproverPolicyManagementStateManaged),
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
			tmBuilder: testTrustManager().WithApproverPolicy(v1alpha1.ApproverPolicyManagementStateManaged),
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
			name:                 "apply policy resources after handoff and missing",
			tmBuilder:            testTrustManager().WithApproverPolicy(v1alpha1.ApproverPolicyManagementStateManaged),
			autoApproverDisabled: true,
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					return false, nil
				})
			},
			wantExistsCount: 3,
			wantPatchCount:  3,
		},
		{
			name:                 "skip apply when existing policy resources match",
			tmBuilder:            testTrustManager().WithApproverPolicy(v1alpha1.ApproverPolicyManagementStateManaged),
			autoApproverDisabled: true,
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
			name:                 "apply when certificate request policy spec drifted",
			tmBuilder:            testTrustManager().WithApproverPolicy(v1alpha1.ApproverPolicyManagementStateManaged),
			autoApproverDisabled: true,
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
			wantPatchCount:  2,
		},
		{
			name:                 "reports missing CertificateRequestPolicy CRD",
			tmBuilder:            testTrustManager().WithApproverPolicy(v1alpha1.ApproverPolicyManagementStateManaged),
			autoApproverDisabled: true,
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
			mock.GetCalls(func(_ context.Context, key client.ObjectKey, obj client.Object) error {
				assert.Equal(t, "cluster", key.Name)
				obj.(*v1alpha1.CertManager).Status.AutoApproverDisabled = tt.autoApproverDisabled
				return tt.certManagerReadError
			})
			err := r.createOrApplyApproverPolicyResources(tm, getResourceLabels(tm), getResourceAnnotations(tm))
			assertError(t, err, tt.wantErr)
			if !approverPolicyManaged(tm) {
				assert.Zero(t, mock.GetCallCount(), "Unmanaged must not read CertManager")
			}

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

func TestCertManagerAutoApproverPredicate(t *testing.T) {
	p := certManagerAutoApproverPredicate()
	cm := &v1alpha1.CertManager{ObjectMeta: metav1.ObjectMeta{Name: "cluster"}}
	unrelatedUpdate := cm.DeepCopy()
	unrelatedUpdate.Generation++
	unrelatedUpdate.Status.ObservedGeneration = 12
	disabled := cm.DeepCopy()
	disabled.Status.AutoApproverDisabled = true
	other := cm.DeepCopy()
	other.Name = "other"

	tests := []struct {
		name       string
		evaluate   func() bool
		wantAccept bool
	}{
		{
			name:       "singleton created",
			evaluate:   func() bool { return p.Create(event.CreateEvent{Object: cm}) },
			wantAccept: true,
		},
		{
			name:       "singleton deleted",
			evaluate:   func() bool { return p.Delete(event.DeleteEvent{Object: cm}) },
			wantAccept: true,
		},
		{
			name:     "generic event ignored",
			evaluate: func() bool { return p.Generic(event.GenericEvent{Object: cm}) },
		},
		{
			name: "unrelated spec and status updates ignored",
			evaluate: func() bool {
				return p.Update(event.UpdateEvent{ObjectOld: cm, ObjectNew: unrelatedUpdate})
			},
		},
		{
			name: "auto-approver disabled",
			evaluate: func() bool {
				return p.Update(event.UpdateEvent{ObjectOld: cm, ObjectNew: disabled})
			},
			wantAccept: true,
		},
		{
			name: "auto-approver restored",
			evaluate: func() bool {
				return p.Update(event.UpdateEvent{ObjectOld: disabled, ObjectNew: cm})
			},
			wantAccept: true,
		},
		{
			name:     "other CertManager created",
			evaluate: func() bool { return p.Create(event.CreateEvent{Object: other}) },
		},
		{
			name:     "other CertManager deleted",
			evaluate: func() bool { return p.Delete(event.DeleteEvent{Object: other}) },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.wantAccept, tc.evaluate())
		})
	}
}
