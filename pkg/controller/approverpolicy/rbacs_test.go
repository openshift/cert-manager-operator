package approverpolicy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	rbacv1 "k8s.io/api/rbac/v1"

	"github.com/openshift/cert-manager-operator/pkg/controller/common"
)

// signerRule returns the single "signers"/"approve" rule from a ClusterRole,
// failing the test if it isn't found exactly once.
func signerRule(t *testing.T, role *rbacv1.ClusterRole) *rbacv1.PolicyRule {
	t.Helper()
	var found *rbacv1.PolicyRule
	for i, rule := range role.Rules {
		if len(rule.Resources) == 1 && rule.Resources[0] == "signers" {
			require.Nil(t, found, "expected exactly one signers rule in the asset")
			found = &role.Rules[i]
		}
	}
	require.NotNil(t, found, "signers rule not found in the asset")
	return found
}

func TestSignerScopeTransitions(t *testing.T) {
	t.Setenv(imageEnvVar, "example.test/approver:test")
	instance := testInstance()
	originalDeployment, err := getDeploymentObject(instance, "")
	require.NoError(t, err)

	// The zero-value CR (ApproveSignerNames unset) must reproduce the asset's
	// unrestricted default: no resourceNames on the signers rule.
	require.Nil(t, instance.Spec.ApproverPolicyConfig.ApproveSignerNames, "test assumes signer scope is unset by default")
	defaultObjects := getRBACObjects(instance)
	assert.Nil(t, signerRule(t, defaultObjects[0].(*rbacv1.ClusterRole)).ResourceNames,
		"default/zero-value scope must remain unrestricted")

	previous := defaultObjects[0].(*rbacv1.ClusterRole)
	unaffected := defaultObjects[1:]

	for _, tc := range []struct {
		name  string
		names []string
	}{
		{"explicit nil is unrestricted", nil},
		{"empty slice is unrestricted", []string{}},
		{"single signer scopes to one name", []string{"issuers.cert-manager.io/*"}},
		{"multiple signers scopes to all names", []string{"external.example/ns.name", "clusterissuers.cert-manager.io/*"}},
		{"reverting to empty restores unrestricted", []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			instance.Spec.ApproverPolicyConfig.ApproveSignerNames = tc.names
			objects := getRBACObjects(instance)
			role := objects[0].(*rbacv1.ClusterRole)

			found := 0
			for i, rule := range role.Rules {
				if len(rule.Resources) == 1 && rule.Resources[0] == "signers" {
					found++
					assert.ElementsMatch(t, tc.names, rule.ResourceNames)
					assert.Equal(t, []string{"approve"}, rule.Verbs)
					assert.Equal(t, []string{"cert-manager.io"}, rule.APIGroups)
				} else {
					assert.Equal(t, previous.Rules[i], rule, "non-signer rules remain intact")
				}
			}
			assert.Equal(t, 1, found, "exactly one signers rule must exist")

			// Signer scope is a ClusterRole-only concern; the ClusterRoleBinding,
			// Role, and RoleBinding must never vary with it.
			assert.Equal(t, unaffected, objects[1:], "ClusterRoleBinding, Role, and RoleBinding must be unaffected by signer scope")

			deployment, err := getDeploymentObject(instance, "")
			require.NoError(t, err)
			assert.Equal(t, originalDeployment.Spec.Template, deployment.Spec.Template, "signer scope must not roll pods")

			previous = role
		})
	}
}

func TestRBACModified(t *testing.T) {
	objects := getRBACObjects(testInstance())

	t.Run("ClusterRole", func(t *testing.T) {
		desired := objects[0].(*rbacv1.ClusterRole)
		require.NotEmpty(t, desired.Rules, "test assumes a non-empty asset")

		for _, tc := range []struct {
			name         string
			mutate       func(*rbacv1.ClusterRole)
			wantModified bool
		}{
			{"identical object", func(*rbacv1.ClusterRole) {}, false},
			{"rule added", func(r *rbacv1.ClusterRole) {
				r.Rules = append(r.Rules, rbacv1.PolicyRule{APIGroups: []string{"extra"}, Resources: []string{"extra"}, Verbs: []string{"get"}})
			}, true},
			{"rule removed", func(r *rbacv1.ClusterRole) { r.Rules = r.Rules[:len(r.Rules)-1] }, true},
			{"rule verb changed", func(r *rbacv1.ClusterRole) { r.Rules[0].Verbs = []string{"get"} }, true},
			{"label tampered", func(r *rbacv1.ClusterRole) { r.Labels["app"] = "tampered" }, true},
			{"managed label missing", func(r *rbacv1.ClusterRole) { delete(r.Labels, common.ManagedResourceLabelKey) }, true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				existing := desired.DeepCopy()
				tc.mutate(existing)
				assert.Equal(t, tc.wantModified, rbacModified(desired, existing))
			})
		}
	})

	t.Run("ClusterRoleBinding", func(t *testing.T) {
		desired := objects[1].(*rbacv1.ClusterRoleBinding)
		require.NotEmpty(t, desired.Subjects, "test assumes a non-empty asset")

		for _, tc := range []struct {
			name         string
			mutate       func(*rbacv1.ClusterRoleBinding)
			wantModified bool
		}{
			{"identical object", func(*rbacv1.ClusterRoleBinding) {}, false},
			{"roleRef changed", func(b *rbacv1.ClusterRoleBinding) { b.RoleRef.Name = "other-role" }, true},
			{"subject added", func(b *rbacv1.ClusterRoleBinding) {
				b.Subjects = append(b.Subjects, rbacv1.Subject{Kind: "ServiceAccount", Name: "extra", Namespace: operandNamespace})
			}, true},
			{"subject removed", func(b *rbacv1.ClusterRoleBinding) { b.Subjects = nil }, true},
			{"managed label missing", func(b *rbacv1.ClusterRoleBinding) { delete(b.Labels, common.ManagedResourceLabelKey) }, true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				existing := desired.DeepCopy()
				tc.mutate(existing)
				assert.Equal(t, tc.wantModified, rbacModified(desired, existing))
			})
		}
	})

	t.Run("Role", func(t *testing.T) {
		desired := objects[2].(*rbacv1.Role)
		require.NotEmpty(t, desired.Rules, "test assumes a non-empty asset")

		for _, tc := range []struct {
			name         string
			mutate       func(*rbacv1.Role)
			wantModified bool
		}{
			{"identical object", func(*rbacv1.Role) {}, false},
			{"rule resourceNames changed", func(r *rbacv1.Role) {
				r.Rules[len(r.Rules)-1].ResourceNames = []string{"other-secret"}
			}, true},
			{"rule removed", func(r *rbacv1.Role) { r.Rules = r.Rules[:len(r.Rules)-1] }, true},
			{"managed label missing", func(r *rbacv1.Role) { delete(r.Labels, common.ManagedResourceLabelKey) }, true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				existing := desired.DeepCopy()
				tc.mutate(existing)
				assert.Equal(t, tc.wantModified, rbacModified(desired, existing))
			})
		}
	})

	t.Run("RoleBinding", func(t *testing.T) {
		desired := objects[3].(*rbacv1.RoleBinding)
		require.NotEmpty(t, desired.Subjects, "test assumes a non-empty asset")

		for _, tc := range []struct {
			name         string
			mutate       func(*rbacv1.RoleBinding)
			wantModified bool
		}{
			{"identical object", func(*rbacv1.RoleBinding) {}, false},
			{"roleRef changed", func(b *rbacv1.RoleBinding) { b.RoleRef.Name = "other-role" }, true},
			{"subject namespace changed", func(b *rbacv1.RoleBinding) { b.Subjects[0].Namespace = "other-namespace" }, true},
			{"managed label missing", func(b *rbacv1.RoleBinding) { delete(b.Labels, common.ManagedResourceLabelKey) }, true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				existing := desired.DeepCopy()
				tc.mutate(existing)
				assert.Equal(t, tc.wantModified, rbacModified(desired, existing))
			})
		}
	})
}
