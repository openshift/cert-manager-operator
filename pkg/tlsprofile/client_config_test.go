package tlsprofile

import (
	"crypto/x509"
	"testing"
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
