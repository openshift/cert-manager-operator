package tlsprofile

import (
	"strings"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
)

func TestEffectiveSpec_nilUsesIntermediate(t *testing.T) {
	spec, err := EffectiveSpec(nil)
	if err != nil {
		t.Fatal(err)
	}
	if spec.MinTLSVersion != configv1.VersionTLS12 {
		t.Fatalf("expected intermediate min TLS 1.2, got %q", spec.MinTLSVersion)
	}
	if len(spec.Ciphers) == 0 {
		t.Fatal("expected non-empty cipher list")
	}
}

func TestEffectiveSpec_custom(t *testing.T) {
	profile := &configv1.TLSSecurityProfile{
		Type: configv1.TLSProfileCustomType,
		Custom: &configv1.CustomTLSProfile{
			TLSProfileSpec: configv1.TLSProfileSpec{
				Ciphers:       []string{"TLS_AES_128_GCM_SHA256", "ECDHE-RSA-AES128-GCM-SHA256"},
				MinTLSVersion: configv1.VersionTLS12,
			},
		},
	}
	spec, err := EffectiveSpec(profile)
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Ciphers) != 2 {
		t.Fatalf("cipher count: %d", len(spec.Ciphers))
	}
}

func TestCertManagerWebhookTLSArgs_joinsCiphers(t *testing.T) {
	spec := &configv1.TLSProfileSpec{
		Ciphers:       []string{"ECDHE-RSA-AES128-GCM-SHA256", "TLS_AES_128_GCM_SHA256"},
		MinTLSVersion: configv1.VersionTLS12,
	}
	args := CertManagerWebhookTLSArgs(spec)
	argMap := map[string]string{}
	for _, a := range args {
		parts := strings.SplitN(a, "=", 2)
		if len(parts) != 2 {
			t.Fatalf("bad arg %q", a)
		}
		argMap[parts[0]] = parts[1]
	}
	if !strings.Contains(argMap["--tls-cipher-suites"], "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256") {
		t.Fatalf("unexpected tls ciphers: %q", argMap["--tls-cipher-suites"])
	}
}
