package tlsprofile

import (
	"crypto/x509"
	"strings"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
)

func TestClientTLSConfig_intermediate(t *testing.T) {
	spec, err := EffectiveSpec(nil)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	cfg, err := ClientTLSConfig(spec, pool)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MinVersion == 0 {
		t.Fatal("expected MinVersion set")
	}
	if len(cfg.CipherSuites) == 0 {
		t.Fatal("expected CipherSuites set")
	}
	if len(cfg.CurvePreferences) != len(DefaultCurvePreferences) {
		t.Fatalf("curve count: got %d want %d", len(cfg.CurvePreferences), len(DefaultCurvePreferences))
	}
}

func TestClientTLSConfig_nilSpec(t *testing.T) {
	_, err := ClientTLSConfig(nil, x509.NewCertPool())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestClientTLSConfig_emptyCiphersUsesGoDefaults(t *testing.T) {
	spec := &configv1.TLSProfileSpec{
		Ciphers:       nil,
		MinTLSVersion: configv1.VersionTLS12,
	}
	cfg, err := ClientTLSConfig(spec, x509.NewCertPool())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CipherSuites != nil {
		t.Fatalf("expected nil CipherSuites when spec.Ciphers is empty, got %#v", cfg.CipherSuites)
	}
}

func TestClientTLSConfig_nonEmptyCiphersUnmappedReturnsError(t *testing.T) {
	spec := &configv1.TLSProfileSpec{
		Ciphers:       []string{"not-a-real-openssl-cipher-name-xyz"},
		MinTLSVersion: configv1.VersionTLS12,
	}
	_, err := ClientTLSConfig(spec, x509.NewCertPool())
	if err == nil {
		t.Fatal("expected error when spec.Ciphers is non-empty but maps to no IANA suites")
	}
	if !strings.Contains(err.Error(), "no cipher suites after OpenSSL→IANA mapping") {
		t.Fatalf("unexpected error: %v", err)
	}
}
