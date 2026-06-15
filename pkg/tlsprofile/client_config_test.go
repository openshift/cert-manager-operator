package tlsprofile

import (
	"crypto/tls"
	"crypto/x509"
	"strings"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
)

func TestClientTLSConfig(t *testing.T) {
	tests := []struct {
		name        string
		spec        func(t *testing.T) *configv1.TLSProfileSpec
		wantErr     bool
		errContains string
		check       func(t *testing.T, cfg *tls.Config)
	}{
		{
			name: "intermediate profile sets min version and cipher suites",
			spec: func(t *testing.T) *configv1.TLSProfileSpec {
				t.Helper()
				spec, err := EffectiveSpec(nil)
				if err != nil {
					t.Fatal(err)
				}
				return spec
			},
			check: func(t *testing.T, cfg *tls.Config) {
				t.Helper()
				if cfg.MinVersion == 0 {
					t.Fatal("expected MinVersion set")
				}
				if len(cfg.CipherSuites) == 0 {
					t.Fatal("expected CipherSuites set")
				}
				if len(cfg.CurvePreferences) != len(DefaultCurvePreferences) {
					t.Fatalf("curve count: got %d want %d", len(cfg.CurvePreferences), len(DefaultCurvePreferences))
				}
			},
		},
		{
			name: "modern profile sets TLS 1.3 min version and skips cipher suites",
			spec: func(t *testing.T) *configv1.TLSProfileSpec {
				t.Helper()
				spec, err := EffectiveSpec(&configv1.TLSSecurityProfile{
					Type: configv1.TLSProfileModernType,
				})
				if err != nil {
					t.Fatal(err)
				}
				return spec
			},
			check: func(t *testing.T, cfg *tls.Config) {
				t.Helper()
				if cfg.MinVersion != tls.VersionTLS13 {
					t.Fatalf("MinVersion: got %x want TLS 1.3", cfg.MinVersion)
				}
				if cfg.CipherSuites != nil {
					t.Fatalf("expected nil CipherSuites for TLS 1.3, got %#v", cfg.CipherSuites)
				}
				if len(cfg.CurvePreferences) != len(DefaultCurvePreferences) {
					t.Fatalf("curve count: got %d want %d", len(cfg.CurvePreferences), len(DefaultCurvePreferences))
				}
			},
		},
		{
			name: "nil spec returns error",
			spec: func(t *testing.T) *configv1.TLSProfileSpec {
				t.Helper()
				return nil
			},
			wantErr: true,
		},
		{
			name: "empty ciphers uses Go defaults",
			spec: func(t *testing.T) *configv1.TLSProfileSpec {
				t.Helper()
				return &configv1.TLSProfileSpec{
					Ciphers:       nil,
					MinTLSVersion: configv1.VersionTLS12,
				}
			},
			check: func(t *testing.T, cfg *tls.Config) {
				t.Helper()
				if cfg.CipherSuites != nil {
					t.Fatalf("expected nil CipherSuites when spec.Ciphers is empty, got %#v", cfg.CipherSuites)
				}
			},
		},
		{
			name: "TLS 1.3 min version skips cipher suite mapping",
			spec: func(t *testing.T) *configv1.TLSProfileSpec {
				t.Helper()
				return &configv1.TLSProfileSpec{
					Ciphers:       []string{"not-a-real-openssl-cipher-name-xyz"},
					MinTLSVersion: configv1.VersionTLS13,
				}
			},
			check: func(t *testing.T, cfg *tls.Config) {
				t.Helper()
				if cfg.CipherSuites != nil {
					t.Fatalf("expected nil CipherSuites for TLS 1.3, got %#v", cfg.CipherSuites)
				}
			},
		},
		{
			name: "non-empty ciphers with no IANA mapping returns error",
			spec: func(t *testing.T) *configv1.TLSProfileSpec {
				t.Helper()
				return &configv1.TLSProfileSpec{
					Ciphers:       []string{"not-a-real-openssl-cipher-name-xyz"},
					MinTLSVersion: configv1.VersionTLS12,
				}
			},
			wantErr:     true,
			errContains: "no cipher suites after OpenSSL→IANA mapping",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ClientTLSConfig(tt.spec(t), x509.NewCertPool())
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}
