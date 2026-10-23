package istiocsr

import (
	"testing"

	configv1 "github.com/openshift/api/config/v1"

	"github.com/openshift/cert-manager-operator/pkg/tlsprofile"
)

// mustEffectiveSpec is a test helper that calls tlsprofile.EffectiveSpec and panics on error.
func mustEffectiveSpec(t *testing.T, profile *configv1.TLSSecurityProfile) *configv1.TLSProfileSpec {
	t.Helper()
	spec, err := tlsprofile.EffectiveSpec(profile)
	if err != nil {
		t.Fatalf("EffectiveSpec(%v) failed: %v", profile, err)
	}
	return spec
}

func TestClusterTLSProfileArgs(t *testing.T) {
	tests := []struct {
		name         string
		tlsConfig    func(t *testing.T) TLSConfig
		wantArgsNil  bool
		wantContains []string
	}{
		{
			name: "Honor false skips cluster TLS profile",
			tlsConfig: func(t *testing.T) TLSConfig {
				return TLSConfig{
					Adherence: configv1.TLSAdherencePolicyNoOpinion,
					Honor:     false,
				}
			},
			wantArgsNil: true,
		},
		{
			name: "LegacyAdheringComponentsOnly skips cluster TLS profile for istio-csr",
			tlsConfig: func(t *testing.T) TLSConfig {
				return TLSConfig{
					Adherence: configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly,
					Honor:     false,
				}
			},
			wantArgsNil: true,
		},
		{
			name: "Honor true with nil Spec returns nil",
			tlsConfig: func(t *testing.T) TLSConfig {
				return TLSConfig{
					Adherence: configv1.TLSAdherencePolicyStrictAllComponents,
					Honor:     true,
					Spec:      nil,
				}
			},
			wantArgsNil: true,
		},
		{
			name: "StrictAllComponents with Modern profile returns serving TLS args",
			tlsConfig: func(t *testing.T) TLSConfig {
				return TLSConfig{
					Adherence: configv1.TLSAdherencePolicyStrictAllComponents,
					Honor:     true,
					Spec:      mustEffectiveSpec(t, &configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType}),
				}
			},
			wantContains: []string{
				"--serving-tls-min-version=VersionTLS13",
				"--serving-tls-curve-preferences=X25519",
			},
		},
		{
			name: "StrictAllComponents with Intermediate profile returns serving TLS args",
			tlsConfig: func(t *testing.T) TLSConfig {
				return TLSConfig{
					Adherence: configv1.TLSAdherencePolicyStrictAllComponents,
					Honor:     true,
					Spec:      mustEffectiveSpec(t, &configv1.TLSSecurityProfile{Type: configv1.TLSProfileIntermediateType}),
				}
			},
			wantContains: []string{
				"--serving-tls-min-version=VersionTLS12",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testReconciler(t)
			r.tlsConfig = tt.tlsConfig(t)

			args := r.clusterTLSProfileArgs()

			if tt.wantArgsNil {
				if args != nil {
					t.Fatalf("expected nil args, got %#v", args)
				}
				return
			}

			for _, want := range tt.wantContains {
				if !containsArg(args, want) {
					t.Errorf("expected to find %q in args, got %#v", want, args)
				}
			}
		})
	}
}
