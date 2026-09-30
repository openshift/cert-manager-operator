package tlsprofile

import (
	"reflect"
	"strings"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
)

func TestEffectiveSpec_builtinDeepCopiesCiphers(t *testing.T) {
	ref := configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
	if len(ref.Ciphers) == 0 {
		t.Fatal("expected non-empty intermediate cipher list")
	}
	origFirst := ref.Ciphers[0]

	spec, err := EffectiveSpec(&configv1.TLSSecurityProfile{Type: configv1.TLSProfileIntermediateType})
	if err != nil {
		t.Fatal(err)
	}
	spec.Ciphers[0] = "MUTATED-CIPHER-SHOULD-NOT-LEAK"

	spec2, err := EffectiveSpec(&configv1.TLSSecurityProfile{Type: configv1.TLSProfileIntermediateType})
	if err != nil {
		t.Fatal(err)
	}
	if spec2.Ciphers[0] != origFirst {
		t.Fatalf("second EffectiveSpec first cipher %q, want %q (shared backing with global?)", spec2.Ciphers[0], origFirst)
	}
	if ref.Ciphers[0] != origFirst {
		t.Fatalf("global TLSProfiles mutated: got %q want %q", ref.Ciphers[0], origFirst)
	}
}

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

func TestEffectiveSpec_unknownTypeReturnsError(t *testing.T) {
	profile := &configv1.TLSSecurityProfile{
		Type: configv1.TLSProfileType("bogus-profile-type"),
	}
	_, err := EffectiveSpec(profile)
	if err == nil {
		t.Fatal("expected error for unrecognized TLS profile type")
	}
	if !strings.Contains(err.Error(), "bogus-profile-type") {
		t.Fatalf("expected error to mention unknown type, got: %v", err)
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

func TestCertManagerWebhookTLSArgs_nilSpecReturnsEmpty(t *testing.T) {
	args := CertManagerWebhookTLSArgs(nil)
	if len(args) != 0 {
		t.Fatalf("expected empty args, got %#v", args)
	}
}

func TestCertManagerOperandMetricsTLSArgs_nilSpecReturnsEmpty(t *testing.T) {
	args := CertManagerOperandMetricsTLSArgs(nil)
	if len(args) != 0 {
		t.Fatalf("expected empty args, got %#v", args)
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

func TestCertManagerWebhookTLSArgs_tls13OmitsCipherFlags(t *testing.T) {
	spec := &configv1.TLSProfileSpec{
		Ciphers: []string{
			"TLS_AES_128_GCM_SHA256",
			"TLS_AES_256_GCM_SHA384",
			"TLS_CHACHA20_POLY1305_SHA256",
		},
		MinTLSVersion: configv1.VersionTLS13,
	}
	args := CertManagerWebhookTLSArgs(spec)
	for _, a := range args {
		if strings.HasPrefix(a, "--tls-cipher-suites") || strings.HasPrefix(a, "--metrics-tls-cipher-suites") {
			t.Fatalf("TLS 1.3 must not set cipher flags, got %q", a)
		}
	}
	want := map[string]string{
		"--tls-min-version":         "VersionTLS13",
		"--metrics-tls-min-version": "VersionTLS13",
	}
	got := map[string]string{}
	for _, a := range args {
		parts := strings.SplitN(a, "=", 2)
		if len(parts) != 2 {
			t.Fatalf("bad arg %q", a)
		}
		got[parts[0]] = parts[1]
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("arg %s: got %q want %q", k, got[k], v)
		}
	}
}

func TestCertManagerOperandMetricsTLSArgs_tls13OmitsCipherFlags(t *testing.T) {
	spec := &configv1.TLSProfileSpec{
		Ciphers: []string{
			"TLS_AES_128_GCM_SHA256",
			"TLS_AES_256_GCM_SHA384",
			"TLS_CHACHA20_POLY1305_SHA256",
		},
		MinTLSVersion: configv1.VersionTLS13,
	}
	args := CertManagerOperandMetricsTLSArgs(spec)
	if len(args) != 1 || args[0] != "--metrics-tls-min-version=VersionTLS13" {
		t.Fatalf("unexpected args: %#v", args)
	}
}

func TestIstioCSRServingTLSArgs_nilSpecReturnsEmpty(t *testing.T) {
	args := IstioCSRServingTLSArgs(nil)
	if len(args) != 0 {
		t.Fatalf("expected empty args, got %#v", args)
	}
}

func TestIstioCSRServingTLSArgs_repeatsCiphersAndCurves(t *testing.T) {
	spec := &configv1.TLSProfileSpec{
		Ciphers:       []string{"ECDHE-RSA-AES128-GCM-SHA256", "ECDHE-RSA-AES256-GCM-SHA384"},
		MinTLSVersion: configv1.VersionTLS12,
	}
	args := IstioCSRServingTLSArgs(spec)

	if args[0] != "--serving-tls-min-version=VersionTLS12" {
		t.Fatalf("unexpected first arg: %q", args[0])
	}

	var ciphers, curves []string
	for _, a := range args[1:] {
		parts := strings.SplitN(a, "=", 2)
		if len(parts) != 2 {
			t.Fatalf("bad arg %q", a)
		}
		switch parts[0] {
		case "--serving-tls-cipher-suites":
			ciphers = append(ciphers, parts[1])
		case "--serving-tls-curve-preferences":
			curves = append(curves, parts[1])
		default:
			t.Fatalf("unexpected arg key %q", parts[0])
		}
	}

	wantCiphers := []string{"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256", "TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384"}
	if !reflect.DeepEqual(ciphers, wantCiphers) {
		t.Fatalf("unexpected cipher-suite flags: got %#v, want %#v", ciphers, wantCiphers)
	}

	wantCurves := []string{"X25519", "CurveP256", "CurveP384", "CurveP521"}
	if !reflect.DeepEqual(curves, wantCurves) {
		t.Fatalf("unexpected curve-preference flags: got %#v, want %#v", curves, wantCurves)
	}
}

func TestIstioCSRServingTLSArgs_tls13OmitsCipherFlagsButKeepsCurves(t *testing.T) {
	spec := &configv1.TLSProfileSpec{
		Ciphers: []string{
			"TLS_AES_128_GCM_SHA256",
			"TLS_AES_256_GCM_SHA384",
			"TLS_CHACHA20_POLY1305_SHA256",
		},
		MinTLSVersion: configv1.VersionTLS13,
	}
	args := IstioCSRServingTLSArgs(spec)

	if args[0] != "--serving-tls-min-version=VersionTLS13" {
		t.Fatalf("unexpected first arg: %q", args[0])
	}
	for _, a := range args {
		if strings.HasPrefix(a, "--serving-tls-cipher-suites") {
			t.Fatalf("TLS 1.3 must not set cipher flags, got %q", a)
		}
	}

	var curveCount int
	for _, a := range args {
		if strings.HasPrefix(a, "--serving-tls-curve-preferences=") {
			curveCount++
		}
	}
	if curveCount != 4 {
		t.Fatalf("expected 4 curve-preference flags for TLS 1.3, got %d (args=%#v)", curveCount, args)
	}
}
