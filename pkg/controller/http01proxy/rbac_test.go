package http01proxy

import (
	"os"
	"slices"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"
)

const (
	clusterRolePath = "../../../config/rbac/role.yaml"
	csvPath         = "../../../bundle/manifests/cert-manager-operator.clusterserviceversion.yaml"
)

// machineConfigRules returns the rules that grant anything on MachineConfigs.
func machineConfigRules(rules []rbacv1.PolicyRule) []rbacv1.PolicyRule {
	var out []rbacv1.PolicyRule
	for _, rule := range rules {
		if slices.Contains(rule.APIGroups, "machineconfiguration.openshift.io") &&
			(slices.Contains(rule.Resources, "machineconfigs") || slices.Contains(rule.Resources, "*")) {
			out = append(out, rule)
		}
	}
	return out
}

// assertMachineConfigRBACScoped checks the operator's MachineConfig grant:
// create is the only verb without a name, because RBAC cannot scope create.
// get/update/delete are limited to machineConfigName. Nothing else is granted:
// in particular, no list or watch, which would let the operator read every
// MachineConfig, including the rendered ones.
func assertMachineConfigRBACScoped(t *testing.T, source string, rules []rbacv1.PolicyRule) {
	t.Helper()

	mcRules := machineConfigRules(rules)
	if len(mcRules) == 0 {
		t.Fatalf("%s: no MachineConfig rules found; the controller cannot create its MachineConfig", source)
	}

	var unnamedVerbs, namedVerbs []string
	for _, rule := range mcRules {
		if slices.Contains(rule.Resources, "*") || slices.Contains(rule.Verbs, "*") {
			t.Errorf("%s: wildcard MachineConfig rule %+v", source, rule)
		}
		switch {
		case len(rule.ResourceNames) == 0:
			unnamedVerbs = append(unnamedVerbs, rule.Verbs...)
		case len(rule.ResourceNames) == 1 && rule.ResourceNames[0] == machineConfigName:
			namedVerbs = append(namedVerbs, rule.Verbs...)
		default:
			t.Errorf("%s: MachineConfig rule names %v, want only %q", source, rule.ResourceNames, machineConfigName)
		}
	}

	slices.Sort(unnamedVerbs)
	slices.Sort(namedVerbs)
	if want := []string{"create"}; !slices.Equal(unnamedVerbs, want) {
		t.Errorf("%s: unscoped MachineConfig verbs = %v, want %v", source, unnamedVerbs, want)
	}
	if want := []string{"delete", "get", "update"}; !slices.Equal(namedVerbs, want) {
		t.Errorf("%s: MachineConfig verbs scoped to %q = %v, want %v", source, machineConfigName, namedVerbs, want)
	}
}

// TestMachineConfigRBACScopedInClusterRole guards the controller-gen output of
// the +kubebuilder:rbac markers in controller.go. It also pins the resource name
// to machineConfigName, so renaming the MachineConfig without updating the
// marker fails here and not with a Forbidden error on a cluster.
func TestMachineConfigRBACScopedInClusterRole(t *testing.T) {
	data, err := os.ReadFile(clusterRolePath)
	if err != nil {
		t.Fatalf("read %s: %v", clusterRolePath, err)
	}
	role := &rbacv1.ClusterRole{}
	if err := yaml.Unmarshal(data, role); err != nil {
		t.Fatalf("parse %s: %v", clusterRolePath, err)
	}
	assertMachineConfigRBACScoped(t, clusterRolePath, role.Rules)
}

// TestMachineConfigRBACScopedInCSV checks the permissions OLM actually grants,
// which come from the bundle CSV and not from config/rbac.
func TestMachineConfigRBACScopedInCSV(t *testing.T) {
	data, err := os.ReadFile(csvPath)
	if err != nil {
		t.Fatalf("read %s: %v", csvPath, err)
	}
	var csv struct {
		Spec struct {
			Install struct {
				Spec struct {
					ClusterPermissions []struct {
						ServiceAccountName string              `json:"serviceAccountName"`
						Rules              []rbacv1.PolicyRule `json:"rules"`
					} `json:"clusterPermissions"`
					Permissions []struct {
						Rules []rbacv1.PolicyRule `json:"rules"`
					} `json:"permissions"`
				} `json:"spec"`
			} `json:"install"`
		} `json:"spec"`
	}
	if err := yaml.Unmarshal(data, &csv); err != nil {
		t.Fatalf("parse %s: %v", csvPath, err)
	}

	var clusterRules []rbacv1.PolicyRule
	for _, perm := range csv.Spec.Install.Spec.ClusterPermissions {
		clusterRules = append(clusterRules, perm.Rules...)
	}
	assertMachineConfigRBACScoped(t, csvPath, clusterRules)

	// MachineConfigs are cluster-scoped. A namespaced grant would not work and
	// would mean that someone hand-edited the CSV.
	for _, perm := range csv.Spec.Install.Spec.Permissions {
		if rules := machineConfigRules(perm.Rules); len(rules) > 0 {
			t.Errorf("%s: unexpected namespaced MachineConfig rules %+v", csvPath, rules)
		}
	}
}
