package trustmanager

import (
	"context"
	"os"
	"testing"
	"time"

	policyv1alpha1 "github.com/cert-manager/approver-policy/pkg/apis/policy/v1alpha1"
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanagermetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	authv1 "k8s.io/api/authorization/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
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

	// The RBAC authorizer may not have observed the new role and binding yet.
	// CertManager is absent, so NotFound confirms the operator's read permission.
	err = wait.PollUntilContextTimeout(ctx, 100*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
		err := operator.Get(ctx, client.ObjectKey{Name: "cluster"}, &v1alpha1.CertManager{})
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		if apierrors.IsForbidden(err) {
			return false, nil
		}
		return false, err
	})
	require.NoError(t, err, "operator RBAC grant must be visible before reconciliation")

	for _, verb := range []string{"get", "patch", "delete", "use"} {
		t.Run("operator cannot "+verb+" unrelated policies", func(t *testing.T) {
			review := &authv1.SubjectAccessReview{Spec: authv1.SubjectAccessReviewSpec{
				User: operatorConfig.Impersonate.UserName,
				ResourceAttributes: &authv1.ResourceAttributes{
					Group:    "policy.cert-manager.io",
					Resource: "certificaterequestpolicies",
					Name:     "unrelated-policy",
					Verb:     verb,
				},
			}}
			require.NoError(t, admin.Create(t.Context(), review))
			assert.False(t, review.Status.Allowed, "operator must not %s an unrelated policy", verb)
		})
	}

	r := testReconciler(t)
	c := &fakes.FakeCtrlClient{}
	c.GetCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
		return operator.Get(ctx, key, obj)
	})
	c.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
		err := c.Get(ctx, key, obj)
		return err == nil, client.IgnoreNotFound(err)
	})
	c.PatchCalls(operator.Patch)
	c.DeleteCalls(operator.Delete)
	r.CtrlClient = c

	tm := testTrustManager().Build()
	require.NoError(t, admin.Create(ctx, tm))
	assert.Equal(t, v1alpha1.ApproverPolicyManagementStateManaged, tm.Spec.TrustManagerConfig.WebhookTLS.ApproverPolicy.ManagementState)
	// Verify that policy/RBAC transitions preserve the existing readiness conditions.
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
		wantPatches   int
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
			wantReads:     4,
			wantPatches:   3,
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
			wantPatches:   4,
		},
		{
			name: "policy selector follows a configured ClusterIssuer",
			prepare: func(t *testing.T) {
				tm.Spec.TrustManagerConfig.WebhookTLS.CertManager.IssuerRef = &certmanagermetav1.IssuerReference{Name: "custom-ca", Kind: "ClusterIssuer", Group: "cert-manager.io"}
			},
			wantResources: true,
			wantReads:     4,
			wantPatches:   2,
		},
		{
			name: "removing the override restores the default issuer selector",
			prepare: func(t *testing.T) {
				tm.Spec.TrustManagerConfig.WebhookTLS.CertManager.IssuerRef = nil
			},
			wantResources: true,
			wantReads:     4,
			wantPatches:   2,
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
			wantReads:   4,
			wantDeletes: 3,
		},
	}

	for _, tc := range tests {
		if !t.Run(tc.name, func(t *testing.T) {
			if tc.prepare != nil {
				tc.prepare(t)
			}
			reads, patches, deletes := c.GetCallCount(), c.PatchCallCount(), c.DeleteCallCount()

			require.NoError(t, r.createOrApplyApproverPolicyResources(tm, labels, annotations))

			assert.Equal(t, tc.wantReads, c.GetCallCount()-reads, "read calls")
			assert.Equal(t, tc.wantPatches, c.PatchCallCount()-patches, "patch calls")
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
			assert.Equal(t, tm.Status.Conditions, persisted.Status.Conditions)
			if tc.verify != nil {
				tc.verify(t)
			}
		}) {
			// Later transitions need the preceding case to have succeeded.
			return
		}
	}

	cm.Status.AutoApproverDisabled = true
	require.NoError(t, admin.Status().Update(ctx, cm))

	for _, state := range []v1alpha1.ApproverPolicyManagementState{
		v1alpha1.ApproverPolicyManagementStateUnmanaged,
		v1alpha1.ApproverPolicyManagementStateManaged,
	} {
		t.Run("repairs policy fields added while "+string(state), func(t *testing.T) {
			tm.Spec.TrustManagerConfig.WebhookTLS.ApproverPolicy.ManagementState = v1alpha1.ApproverPolicyManagementStateManaged
			require.NoError(t, r.createOrApplyApproverPolicyResources(tm, labels, annotations))
			tm.Spec.TrustManagerConfig.WebhookTLS.ApproverPolicy.ManagementState = state

			// A separate manager owns only the added validation and annotation.
			// Reapplying the operator's manifest must remove the validation but
			// retain unrelated metadata and the operand's status observation.
			policy := getCertificateRequestPolicyObject(tm.Spec.TrustManagerConfig.WebhookTLS.CertManager, labels, annotations)
			require.NoError(t, admin.Patch(ctx, policy, client.RawPatch(types.ApplyPatchType, []byte(`{
				"apiVersion": "policy.cert-manager.io/v1alpha1",
				"kind": "CertificateRequestPolicy",
				"metadata": {
					"name": "trust-manager-policy",
					"annotations": {"example.com/user-note": "keep"}
				},
				"spec": {"allowed": {"commonName": {"validations": [{"rule": "false"}]}}}
			}`)), client.FieldOwner("test-policy-user")))
			policy.Status.Conditions = []metav1.Condition{{
				Type: policyv1alpha1.ConditionTypeReady, Status: metav1.ConditionTrue,
				Reason: "Ready", LastTransitionTime: metav1.Now(),
			}}
			require.NoError(t, admin.Status().Update(ctx, policy))
			before := policy.DeepCopy()
			if state == v1alpha1.ApproverPolicyManagementStateUnmanaged {
				patches := c.PatchCallCount()
				require.NoError(t, r.createOrApplyApproverPolicyResources(tm, labels, annotations))
				assert.Equal(t, patches, c.PatchCallCount())
				require.NoError(t, admin.Get(ctx, client.ObjectKeyFromObject(policy), policy))
				assert.Equal(t, before.Spec, policy.Spec)
			}

			// A fresh reconciler proves recovery does not depend on remembering
			// a Managed/Unmanaged transition in memory.
			tm.Spec.TrustManagerConfig.WebhookTLS.ApproverPolicy.ManagementState = v1alpha1.ApproverPolicyManagementStateManaged
			fresh := testReconciler(t)
			fresh.CtrlClient = c
			require.NoError(t, fresh.createOrApplyApproverPolicyResources(tm, labels, annotations))
			require.NoError(t, admin.Get(ctx, client.ObjectKeyFromObject(policy), policy))
			assert.Equal(t, getCertificateRequestPolicyObject(tm.Spec.TrustManagerConfig.WebhookTLS.CertManager, labels, annotations).Spec, policy.Spec)
			assert.Equal(t, before.UID, policy.UID, "policy must be repaired in place")
			assert.Equal(t, "keep", policy.Annotations["example.com/user-note"])
			assert.Equal(t, before.Status, policy.Status)

			patches := c.PatchCallCount()
			require.NoError(t, fresh.createOrApplyApproverPolicyResources(tm, labels, annotations))
			assert.Equal(t, patches, c.PatchCallCount(), "repaired policy must converge without repeated writes")
		})
	}

	t.Run("recreates a managed binding with an immutable roleRef", func(t *testing.T) {
		tm.Spec.TrustManagerConfig.WebhookTLS.ApproverPolicy.ManagementState = v1alpha1.ApproverPolicyManagementStateUnmanaged
		binding := getPolicyClusterRoleBindingObject(labels, annotations)
		require.NoError(t, admin.Delete(ctx, binding))
		binding.RoleRef.Name = "custom-policy-role"
		require.NoError(t, admin.Create(ctx, binding))
		originalUID := binding.UID
		require.NoError(t, r.createOrApplyApproverPolicyResources(tm, labels, annotations))
		require.NoError(t, admin.Get(ctx, client.ObjectKeyFromObject(binding), binding))
		assert.Equal(t, "custom-policy-role", binding.RoleRef.Name)

		tm.Spec.TrustManagerConfig.WebhookTLS.ApproverPolicy.ManagementState = v1alpha1.ApproverPolicyManagementStateManaged
		deletes, patches := c.DeleteCallCount(), c.PatchCallCount()
		err := r.createOrApplyApproverPolicyResources(tm, labels, annotations)
		require.True(t, common.IsRetryRequiredError(err), "expected deletion/recreation retry, got %v", err)
		assert.Equal(t, deletes+1, c.DeleteCallCount())
		assert.Equal(t, patches, c.PatchCallCount(), "must not apply an immutable roleRef")

		require.NoError(t, r.createOrApplyApproverPolicyResources(tm, labels, annotations))
		require.NoError(t, admin.Get(ctx, client.ObjectKeyFromObject(binding), binding))
		assert.NotEqual(t, originalUID, binding.UID)
		assert.Equal(t, getPolicyClusterRoleBindingObject(labels, annotations).RoleRef, binding.RoleRef)
		assert.Equal(t, getPolicyClusterRoleBindingObject(labels, annotations).Subjects, binding.Subjects)

		patches = c.PatchCallCount()
		require.NoError(t, r.createOrApplyApproverPolicyResources(tm, labels, annotations))
		assert.Equal(t, patches, c.PatchCallCount(), "replacement binding must converge without repeated writes")
	})
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
		passed := t.Run(tc.name, func(t *testing.T) {
			review := &authv1.SubjectAccessReview{Spec: authv1.SubjectAccessReviewSpec{
				User: tc.user,
				ResourceAttributes: &authv1.ResourceAttributes{
					Group:    "policy.cert-manager.io",
					Resource: "certificaterequestpolicies",
					Name:     "trust-manager-policy",
					Verb:     "use",
				},
			}}
			if tc.wantAllowed {
				// Confirm the new grant is active before testing denied access.
				// Otherwise, those checks could pass against stale RBAC caches.
				err := wait.PollUntilContextTimeout(t.Context(), 100*time.Millisecond, 10*time.Second, true, func(ctx context.Context) (bool, error) {
					current := review.DeepCopy()
					if err := admin.Create(ctx, current); err != nil {
						return false, err
					}
					return current.Status.Allowed, nil
				})
				require.NoError(t, err, "cert-manager service account must be allowed to use the policy")
				return
			}
			require.NoError(t, admin.Create(t.Context(), review))
			assert.Equal(t, tc.wantAllowed, review.Status.Allowed)
		})
		if tc.wantAllowed && !passed {
			return
		}
	}
}
