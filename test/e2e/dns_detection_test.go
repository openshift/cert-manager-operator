//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"os"
	"testing"

	configapiv1 "github.com/openshift/api/config/v1"
	fakeconfigv1client "github.com/openshift/client-go/config/clientset/versioned/fake"
	configv1 "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIsPublicClusterDomain(t *testing.T) {
	ctx := context.Background()

	// Fake client with publicZone set (e.g. AWS/GCP/Azure standard cluster)
	publicZoneClient := fakeconfigv1client.NewSimpleClientset(&configapiv1.DNS{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Spec: configapiv1.DNSSpec{
			BaseDomain: "ci-cluster.aws.domain.com",
			PublicZone: &configapiv1.DNSZone{ID: "Z1234567890"},
		},
	}).ConfigV1()

	// Fake client with privateZone only (e.g. BareMetal, UPI, disconnected, or private cluster)
	privateZoneClient := fakeconfigv1client.NewSimpleClientset(&configapiv1.DNS{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Spec: configapiv1.DNSSpec{
			BaseDomain:  "internal.example.com",
			PrivateZone: &configapiv1.DNSZone{ID: "Z9876543210"},
		},
	}).ConfigV1()

	tests := []struct {
		name         string
		configClient *fakeconfigv1client.Clientset
		useClient    bool
		clientToUse  configv1.ConfigV1Interface
		domain       string
		envForce     string
		customRes    string
		expected     bool
	}{
		{
			name:      "empty domain",
			domain:    "",
			expected:  false,
		},
		{
			name:      "known private suffix .boe without client",
			domain:    "apps.rhcl-mc3.lnxero1.boe",
			expected:  false,
		},
		{
			name:        "known private suffix .boe even if publicZone somehow set",
			clientToUse: publicZoneClient,
			domain:      "apps.rhcl-mc3.lnxero1.boe",
			expected:    false,
		},
		{
			name:      "known private suffix .local",
			domain:    "cluster.local",
			expected:  false,
		},
		{
			name:      "known private suffix .internal",
			domain:    "api.openshift.internal",
			expected:  false,
		},
		{
			name:        "cluster with publicZone configured in OpenShift DNS API",
			clientToUse: publicZoneClient,
			domain:      "ci-cluster.aws.domain.com",
			expected:    true,
		},
		{
			name:        "cluster with privateZone only in OpenShift DNS API (e.g. internal.example.com)",
			clientToUse: privateZoneClient,
			domain:      "internal.example.com",
			expected:    false,
		},
		{
			name:      "unmanaged cluster with no client and no resolver defaults to false",
			domain:    "unmanaged.cluster.example.com",
			expected:  false,
		},
		{
			name:      "explicit override true",
			domain:    "apps.rhcl-mc3.lnxero1.boe",
			envForce:  "true",
			expected:  true,
		},
		{
			name:        "explicit override false overrides publicZone",
			clientToUse: publicZoneClient,
			domain:      "ci-cluster.aws.domain.com",
			envForce:    "false",
			expected:    false,
		},
		{
			name:      "unreachable custom resolver returns false safely without panic",
			domain:    "custom.domain.com",
			customRes: "192.0.2.1:53",
			expected:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.envForce != "" {
				t.Setenv("E2E_FORCE_PUBLIC_DNS", tc.envForce)
			} else {
				os.Unsetenv("E2E_FORCE_PUBLIC_DNS")
			}

			if tc.customRes != "" {
				t.Setenv("E2E_PUBLIC_DNS_RESOLVER", tc.customRes)
			} else {
				os.Unsetenv("E2E_PUBLIC_DNS_RESOLVER")
			}

			result := isPublicClusterDomain(ctx, tc.clientToUse, tc.domain)
			if result != tc.expected {
				t.Errorf("isPublicClusterDomain(%q) = %v, want %v", tc.domain, result, tc.expected)
			}
		})
	}
}
