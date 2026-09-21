package tlsprofile

import (
	"context"
	"strings"
	"testing"

	"k8s.io/client-go/rest"

	configv1 "github.com/openshift/api/config/v1"
)

func TestApplyClusterProfileToHTTPServingInfo_guards(t *testing.T) {
	t.Run("nil serving", func(t *testing.T) {
		err := ApplyClusterProfileToHTTPServingInfo(context.Background(), &rest.Config{}, nil)
		if err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("nil rest config", func(t *testing.T) {
		serving := &configv1.HTTPServingInfo{}
		err := ApplyClusterProfileToHTTPServingInfo(context.Background(), nil, serving)
		if err == nil {
			t.Fatal("expected error")
		}
		if serving.MinTLSVersion != "" {
			t.Fatalf("serving should remain unchanged on error, got min=%q", serving.MinTLSVersion)
		}
	})
}

func TestApplyToHTTPServingInfo(t *testing.T) {
	intermediateSpec, err := EffectiveSpec(&configv1.TLSSecurityProfile{Type: configv1.TLSProfileIntermediateType})
	if err != nil {
		t.Fatal(err)
	}
	modernSpec, err := EffectiveSpec(&configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType})
	if err != nil {
		t.Fatal(err)
	}

	servingWithMin := func(min string) *configv1.HTTPServingInfo {
		s := &configv1.HTTPServingInfo{}
		s.MinTLSVersion = min
		return s
	}

	cases := []struct {
		name             string
		serving          *configv1.HTTPServingInfo
		spec             *configv1.TLSProfileSpec
		wantErr          string
		wantMin          string
		wantCipher       bool
		preserveMinOnErr string
	}{
		{
			name:       "intermediate",
			serving:    &configv1.HTTPServingInfo{},
			spec:       intermediateSpec,
			wantMin:    string(configv1.VersionTLS12),
			wantCipher: true,
		},
		{
			name:       "modern tls13 keeps non-empty ciphers to block defaults",
			serving:    &configv1.HTTPServingInfo{},
			spec:       modernSpec,
			wantMin:    string(configv1.VersionTLS13),
			wantCipher: true,
		},
		{
			name:    "nil serving",
			serving: nil,
			spec:    &configv1.TLSProfileSpec{MinTLSVersion: configv1.VersionTLS12},
			wantErr: "HTTPServingInfo is nil",
		},
		{
			name:             "nil spec",
			serving:          servingWithMin("unchanged"),
			spec:             nil,
			wantErr:          "TLS profile spec is nil",
			preserveMinOnErr: "unchanged",
		},
		{
			name:    "unmappable ciphers",
			serving: servingWithMin("unchanged"),
			spec: &configv1.TLSProfileSpec{
				MinTLSVersion: configv1.VersionTLS12,
				Ciphers:       []string{"NOT-A-REAL-OPENSSL-CIPHER"},
			},
			wantErr: "no cipher suites after OpenSSL",
			// MinTLSVersion is assigned before cipher mapping; only nil-spec leaves serving untouched.
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ApplyToHTTPServingInfo(tc.serving, tc.spec)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tc.wantErr)
				}
				if tc.preserveMinOnErr != "" && tc.serving != nil && tc.serving.MinTLSVersion != tc.preserveMinOnErr {
					t.Fatalf("MinTLSVersion = %q, want preserved %q", tc.serving.MinTLSVersion, tc.preserveMinOnErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.serving.MinTLSVersion != tc.wantMin {
				t.Fatalf("min version: %q, want %q", tc.serving.MinTLSVersion, tc.wantMin)
			}
			if tc.wantCipher && len(tc.serving.CipherSuites) == 0 {
				t.Fatal("expected non-empty cipher list")
			}
		})
	}
}
