package trustmanager

import (
	"context"
	"os"
	"testing"

	policyv1alpha1 "github.com/cert-manager/approver-policy/pkg/apis/policy/v1alpha1"
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanagermetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	authv1 "k8s.io/api/authorization/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/controller/common/fakes"
	"github.com/openshift/cert-manager-operator/pkg/testutil"
)

// Exercise SSA, CRD defaulting/validation, and the operator's actual RBAC. A fake
// API cannot catch escalation failures when granting "use" on a policy.
func TestApproverPolicyIntegration(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("set KUBEBUILDER_ASSETS to run approval integration")
	}
	env := &envtest.Environment{CRDDirectoryPaths: []string{
		"../../../config/crd/bases/operator.openshift.io_trustmanagers.yaml",
		"../../../config/crd/bases/operator.openshift.io_certmanagers.yaml",
		"../../../config/crd/bases/customresourcedefinition_certificaterequestpolicies.policy.cert-manager.io.yml",
	}, ErrorIfCRDPathMissing: true}
	config, err := env.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, env.Stop()) })
	admin, err := client.New(config, client.Options{Scheme: testutil.Scheme})
	require.NoError(t, err)
	ctx := t.Context()

	roleYAML, err := os.ReadFile("../../../config/rbac/role.yaml")
	require.NoError(t, err)
	operatorRole := common.DecodeObjBytes[*rbacv1.ClusterRole](codecs, rbacv1.SchemeGroupVersion, roleYAML)
	require.NoError(t, admin.Create(ctx, operatorRole))
	require.NoError(t, admin.Create(ctx, &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "trust-manager-operator-test"},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: operatorRole.Name},
		Subjects:   []rbacv1.Subject{{APIGroup: rbacv1.GroupName, Kind: "User", Name: "trust-manager-operator-test"}},
	}))
	operatorConfig := rest.CopyConfig(config)
	operatorConfig.Impersonate.UserName = "trust-manager-operator-test"
	operator, err := client.New(operatorConfig, client.Options{Scheme: testutil.Scheme})
	require.NoError(t, err)
	r := testReconciler(t)
	c := &fakes.FakeCtrlClient{}
	c.GetCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
		return operator.Get(ctx, key, obj)
	})
	c.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
		err := c.Get(ctx, key, obj)
		return err == nil, client.IgnoreNotFound(err)
	})
	c.StatusUpdateCalls(func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
		return operator.Status().Update(ctx, obj, opts...)
	})
	c.PatchCalls(operator.Patch)
	c.DeleteCalls(operator.Delete)
	r.CtrlClient = c

	tm := testTrustManager().Build()
	require.NoError(t, admin.Create(ctx, tm))
	assert.Equal(t, v1alpha1.ApproverPolicyManagementStateManaged, tm.Spec.TrustManagerConfig.WebhookTLS.ApproverPolicy.ManagementState)
	// Seed healthy conditions so the test cannot rely on a readiness change
	// to persist later policy status transitions.
	tm.Status.SetCondition(v1alpha1.Ready, metav1.ConditionTrue, v1alpha1.ReasonReady, "reconciliation successful")
	tm.Status.SetCondition(v1alpha1.Degraded, metav1.ConditionFalse, v1alpha1.ReasonReady, "")
	require.NoError(t, admin.Status().Update(ctx, tm))
	labels, annotations := getResourceLabels(tm), getResourceAnnotations(tm)
	desiredResources := func() []client.Object {
		return []client.Object{
			getCertificateRequestPolicyObject(tm.Spec.TrustManagerConfig.WebhookTLS.CertManager, labels, annotations),
			getPolicyClusterRoleObject(labels, annotations),
			getPolicyClusterRoleBindingObject(labels, annotations),
		}
	}
	resources := desiredResources()

	cm := &v1alpha1.CertManager{ObjectMeta: metav1.ObjectMeta{Name: "cluster"}}
	cm.Spec.ManagementState = "Managed"

	// Run these cases in order: each transition uses the resources from the
	// previous case to exercise installation, drift repair, and cleanup.
	tests := []struct {
		name          string
		prepare       func(*testing.T)
		wantResources bool
		wantReads     int
		wantApplies   int
		wantDeletes   int
		verify        func(*testing.T)
	}{
		{
			name:      "missing CertManager creates no resources",
			wantReads: 4,
		},
		{
			name: "default false creates no resources",
			prepare: func(t *testing.T) {
				require.NoError(t, admin.Create(ctx, cm))
			},
			wantReads: 4,
		},
		{
			name: "completed handoff creates narrowly scoped resources",
			prepare: func(t *testing.T) {
				cm.Status.AutoApproverDisabled = true
				require.NoError(t, admin.Status().Update(ctx, cm))
			},
			wantResources: true,
			wantReads:     5,
			wantApplies:   3,
			verify: func(t *testing.T) {
				assertTrustManagerPolicyScope(t, admin, resources)
			},
		},
		{
			name:          "unchanged resources do not cause repeated apply",
			wantResources: true,
			wantReads:     4,
		},
		{
			name: "policy and RBAC drift is repaired",
			prepare: func(t *testing.T) {
				policy := resources[0].(*policyv1alpha1.CertificateRequestPolicy)
				policy.Spec.Allowed.DNSNames.Values = &[]string{"*"}
				require.NoError(t, admin.Update(ctx, policy))
				role := resources[1].(*rbacv1.ClusterRole)
				role.Rules[0].ResourceNames = nil
				require.NoError(t, admin.Update(ctx, role))
				binding := resources[2].(*rbacv1.ClusterRoleBinding)
				binding.Subjects[0].Namespace = "other"
				require.NoError(t, admin.Update(ctx, binding))
			},
			wantResources: true,
			wantReads:     4,
			wantApplies:   3,
		},
		{
			name: "policy selector follows a configured ClusterIssuer",
			prepare: func(t *testing.T) {
				tm.Spec.TrustManagerConfig.WebhookTLS.CertManager.IssuerRef = &certmanagermetav1.IssuerReference{Name: "custom-ca", Kind: "ClusterIssuer", Group: "cert-manager.io"}
			},
			wantResources: true,
			wantReads:     4,
			wantApplies:   1,
		},
		{
			name: "removing the override restores the default issuer selector",
			prepare: func(t *testing.T) {
				tm.Spec.TrustManagerConfig.WebhookTLS.CertManager.IssuerRef = nil
			},
			wantResources: true,
			wantReads:     4,
			wantApplies:   1,
		},
		{
			name: "Unmanaged retains resources when built-in approval returns",
			prepare: func(t *testing.T) {
				tm.Spec.TrustManagerConfig.WebhookTLS.ApproverPolicy.ManagementState = v1alpha1.ApproverPolicyManagementStateUnmanaged
				cm.Status.AutoApproverDisabled = false
				require.NoError(t, admin.Status().Update(ctx, cm))
			},
			wantResources: true,
		},
		{
			name: "Managed resumes cleanup when built-in approval is active",
			prepare: func(t *testing.T) {
				tm.Spec.TrustManagerConfig.WebhookTLS.ApproverPolicy.ManagementState = v1alpha1.ApproverPolicyManagementStateManaged
			},
			wantReads:   5,
			wantDeletes: 3,
		},
	}

	for _, tc := range tests {
		if !t.Run(tc.name, func(t *testing.T) {
			if tc.prepare != nil {
				tc.prepare(t)
			}
			reads, applies, deletes := c.GetCallCount(), c.PatchCallCount(), c.DeleteCallCount()

			require.NoError(t, r.createOrApplyApproverPolicyResources(tm, labels, annotations))

			assert.Equal(t, tc.wantReads, c.GetCallCount()-reads, "read calls")
			assert.Equal(t, tc.wantApplies, c.PatchCallCount()-applies, "apply calls")
			assert.Equal(t, tc.wantDeletes, c.DeleteCallCount()-deletes, "delete calls")
			for i, desired := range desiredResources() {
				live := resources[i]
				err := admin.Get(ctx, client.ObjectKeyFromObject(desired), live)
				if tc.wantResources {
					require.NoError(t, err)
					switch desired := desired.(type) {
					case *policyv1alpha1.CertificateRequestPolicy:
						assert.False(t, certificateRequestPolicyModified(desired, live.(*policyv1alpha1.CertificateRequestPolicy)))
					case *rbacv1.ClusterRole:
						assert.False(t, common.ClusterRoleModified(desired, live.(*rbacv1.ClusterRole), fieldOwner))
					case *rbacv1.ClusterRoleBinding:
						assert.False(t, common.ClusterRoleBindingModified(desired, live.(*rbacv1.ClusterRoleBinding), fieldOwner))
					}
				} else {
					assert.True(t, apierrors.IsNotFound(err), "%s should be absent: %v", desired.GetName(), err)
				}
			}
			persisted := &v1alpha1.TrustManager{}
			require.NoError(t, admin.Get(ctx, client.ObjectKeyFromObject(tm), persisted))
			if tc.wantResources {
				assert.Equal(t, trustManagerCertificateRequestPolicyName, persisted.Status.CertificateRequestPolicy)
			} else {
				assert.Empty(t, persisted.Status.CertificateRequestPolicy)
			}
			assert.Equal(t, tm.Status.Conditions, persisted.Status.Conditions)
			if tc.verify != nil {
				tc.verify(t)
			}
		}) {
			// Later transitions need the preceding case to have succeeded.
			return
		}
	}
}

func assertTrustManagerPolicyScope(t *testing.T, admin client.Client, resources []client.Object) {
	t.Helper()

	policy := resources[0].(*policyv1alpha1.CertificateRequestPolicy)
	require.NotNil(t, policy.Spec.Allowed)
	require.NotNil(t, policy.Spec.Allowed.Usages)
	assert.ElementsMatch(t, []certmanagerv1.KeyUsage{certmanagerv1.UsageDigitalSignature, certmanagerv1.UsageKeyEncipherment}, *policy.Spec.Allowed.Usages)
	require.NotNil(t, policy.Spec.Selector.Namespace)
	assert.Equal(t, []string{operandNamespace}, policy.Spec.Selector.Namespace.MatchNames)
	binding := resources[2].(*rbacv1.ClusterRoleBinding)
	assert.Equal(t, []rbacv1.Subject{{Kind: "ServiceAccount", Name: "cert-manager", Namespace: "cert-manager"}}, binding.Subjects)

	tests := []struct {
		name        string
		user        string
		wantAllowed bool
	}{
		{
			name:        "cert-manager service account can use the policy",
			user:        "system:serviceaccount:cert-manager:cert-manager",
			wantAllowed: true,
		},
		{
			name: "service account in another namespace cannot use the policy",
			user: "system:serviceaccount:other:cert-manager",
		},
		{
			name: "another service account cannot use the policy",
			user: "system:serviceaccount:cert-manager:other",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			review := &authv1.SubjectAccessReview{Spec: authv1.SubjectAccessReviewSpec{
				User: tc.user,
				ResourceAttributes: &authv1.ResourceAttributes{
					Group:    "policy.cert-manager.io",
					Resource: "certificaterequestpolicies",
					Name:     "trust-manager-policy",
					Verb:     "use",
				},
			}}
			require.NoError(t, admin.Create(t.Context(), review))
			assert.Equal(t, tc.wantAllowed, review.Status.Allowed)
		})
	}
}
