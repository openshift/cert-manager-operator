package certmanager

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

func TestValidateComponentName(t *testing.T) {
	tests := []struct {
		name          string
		componentName v1alpha1.ComponentName
		expectError   bool
	}{
		{
			name:          "CoreController is valid",
			componentName: v1alpha1.CoreController,
			expectError:   false,
		},
		{
			name:          "CAInjector is valid",
			componentName: v1alpha1.CAInjector,
			expectError:   false,
		},
		{
			name:          "Webhook is valid",
			componentName: v1alpha1.Webhook,
			expectError:   false,
		},
		{
			name:          "empty string is invalid",
			componentName: "",
			expectError:   true,
		},
		{
			name:          "unknown component name is invalid",
			componentName: "UnknownComponent",
			expectError:   true,
		},
		{
			name:          "lowercase controller is invalid",
			componentName: "controller",
			expectError:   true,
		},
	}

	c := &CertManagerNetworkPolicyUserDefinedController{}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := c.validateComponentName(tc.componentName)
			if tc.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "unsupported component name")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGetPodSelectorForComponent(t *testing.T) {
	tests := []struct {
		name           string
		componentName  v1alpha1.ComponentName
		expectedLabels map[string]string
	}{
		{
			name:          "CoreController returns cert-manager app label",
			componentName: v1alpha1.CoreController,
			expectedLabels: map[string]string{
				"app": "cert-manager",
			},
		},
		{
			name:          "CAInjector returns cainjector app label",
			componentName: v1alpha1.CAInjector,
			expectedLabels: map[string]string{
				"app": "cainjector",
			},
		},
		{
			name:          "Webhook returns webhook app label",
			componentName: v1alpha1.Webhook,
			expectedLabels: map[string]string{
				"app": "webhook",
			},
		},
		{
			name:          "unknown component returns default label",
			componentName: "Unknown",
			expectedLabels: map[string]string{
				"app.kubernetes.io/name": "cert-manager",
			},
		},
	}

	c := &CertManagerNetworkPolicyUserDefinedController{}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			selector := c.getPodSelectorForComponent(tc.componentName)
			require.Equal(t, tc.expectedLabels, selector.MatchLabels)
		})
	}
}

func TestValidateNetworkPolicyConfig(t *testing.T) {
	tests := []struct {
		name        string
		certManager *v1alpha1.CertManager
		expectError bool
		errContains string
	}{
		{
			name: "valid config with single policy passes",
			certManager: &v1alpha1.CertManager{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: v1alpha1.CertManagerSpec{
					NetworkPolicies: []v1alpha1.NetworkPolicy{
						{
							Name:          "allow-egress",
							ComponentName: v1alpha1.CoreController,
						},
					},
				},
			},
			expectError: false,
		},
		{
			name: "valid config with multiple policies passes",
			certManager: &v1alpha1.CertManager{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: v1alpha1.CertManagerSpec{
					NetworkPolicies: []v1alpha1.NetworkPolicy{
						{
							Name:          "allow-egress-controller",
							ComponentName: v1alpha1.CoreController,
						},
						{
							Name:          "allow-egress-webhook",
							ComponentName: v1alpha1.Webhook,
						},
						{
							Name:          "allow-egress-cainjector",
							ComponentName: v1alpha1.CAInjector,
						},
					},
				},
			},
			expectError: false,
		},
		{
			name: "empty network policies list passes",
			certManager: &v1alpha1.CertManager{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: v1alpha1.CertManagerSpec{
					NetworkPolicies: []v1alpha1.NetworkPolicy{},
				},
			},
			expectError: false,
		},
		{
			name: "nil network policies list passes",
			certManager: &v1alpha1.CertManager{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec:       v1alpha1.CertManagerSpec{},
			},
			expectError: false,
		},
		{
			name: "invalid component name fails",
			certManager: &v1alpha1.CertManager{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: v1alpha1.CertManagerSpec{
					NetworkPolicies: []v1alpha1.NetworkPolicy{
						{
							Name:          "bad-policy",
							ComponentName: "InvalidComponent",
						},
					},
				},
			},
			expectError: true,
			errContains: "invalid component name",
		},
		{
			name: "empty policy name fails",
			certManager: &v1alpha1.CertManager{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: v1alpha1.CertManagerSpec{
					NetworkPolicies: []v1alpha1.NetworkPolicy{
						{
							Name:          "",
							ComponentName: v1alpha1.CoreController,
						},
					},
				},
			},
			expectError: true,
			errContains: "name cannot be empty",
		},
		{
			name: "second policy with invalid component fails",
			certManager: &v1alpha1.CertManager{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: v1alpha1.CertManagerSpec{
					NetworkPolicies: []v1alpha1.NetworkPolicy{
						{
							Name:          "good-policy",
							ComponentName: v1alpha1.CoreController,
						},
						{
							Name:          "bad-policy",
							ComponentName: "BadComponent",
						},
					},
				},
			},
			expectError: true,
			errContains: "network policy at index 1",
		},
	}

	c := &CertManagerNetworkPolicyUserDefinedController{}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := c.validateNetworkPolicyConfig(tc.certManager)
			if tc.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCreateUserNetworkPolicy(t *testing.T) {
	tests := []struct {
		name            string
		userPolicy      v1alpha1.NetworkPolicy
		expectedName    string
		expectedLabels  map[string]string
		expectedPodLabels map[string]string
	}{
		{
			name: "creates policy for CoreController",
			userPolicy: v1alpha1.NetworkPolicy{
				Name:          "allow-dns",
				ComponentName: v1alpha1.CoreController,
			},
			expectedName: "cert-manager-user-allow-dns",
			expectedLabels: map[string]string{
				networkPolicyOwnerLabel: "cert-manager",
			},
			expectedPodLabels: map[string]string{
				"app": "cert-manager",
			},
		},
		{
			name: "creates policy for Webhook",
			userPolicy: v1alpha1.NetworkPolicy{
				Name:          "allow-api",
				ComponentName: v1alpha1.Webhook,
			},
			expectedName: "cert-manager-user-allow-api",
			expectedLabels: map[string]string{
				networkPolicyOwnerLabel: "cert-manager",
			},
			expectedPodLabels: map[string]string{
				"app": "webhook",
			},
		},
		{
			name: "creates policy for CAInjector",
			userPolicy: v1alpha1.NetworkPolicy{
				Name:          "allow-egress",
				ComponentName: v1alpha1.CAInjector,
			},
			expectedName: "cert-manager-user-allow-egress",
			expectedLabels: map[string]string{
				networkPolicyOwnerLabel: "cert-manager",
			},
			expectedPodLabels: map[string]string{
				"app": "cainjector",
			},
		},
	}

	c := &CertManagerNetworkPolicyUserDefinedController{}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			policy := c.createUserNetworkPolicy(tc.userPolicy)

			require.Equal(t, tc.expectedName, policy.Name)
			require.Equal(t, certManagerNamespace, policy.Namespace)
			require.Equal(t, tc.expectedLabels, policy.Labels)
			require.Equal(t, tc.expectedPodLabels, policy.Spec.PodSelector.MatchLabels)
		})
	}
}
