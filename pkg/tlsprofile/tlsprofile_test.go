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

func TestEffectiveSpec_customWithNilCustomReturnsError(t *testing.T) {
	profile := &configv1.TLSSecurityProfile{
		Type:   configv1.TLSProfileCustomType,
		Custom: nil,
	}
	_, err := EffectiveSpec(profile)
	if err == nil {
		t.Fatal("expected error for custom type with nil custom settings")
	}
	if !strings.Contains(err.Error(), "missing custom settings") {
		t.Fatalf("expected error about missing custom settings, got: %v", err)
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

func TestTrustManagerWebhookTLSArgs_nilSpecReturnsEmpty(t *testing.T) {
	args := TrustManagerWebhookTLSArgs(nil)
	if len(args) != 0 {
		t.Fatalf("expected empty args, got %#v", args)
	}
}

func TestTrustManagerWebhookTLSArgs_joinsCiphers(t *testing.T) {
	spec := &configv1.TLSProfileSpec{
		Ciphers:       []string{"ECDHE-RSA-AES128-GCM-SHA256", "TLS_AES_128_GCM_SHA256"},
		MinTLSVersion: configv1.VersionTLS12,
	}
	args := TrustManagerWebhookTLSArgs(spec)
	argMap := map[string]string{}
	for _, a := range args {
		parts := strings.SplitN(a, "=", 2)
		if len(parts) != 2 {
			t.Fatalf("bad arg %q", a)
		}
		argMap[parts[0]] = parts[1]
	}
	if argMap["--tls-min-version"] != "VersionTLS12" {
		t.Fatalf("unexpected min version: %q", argMap["--tls-min-version"])
	}
	if !strings.Contains(argMap["--tls-cipher-suites"], "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256") {
		t.Fatalf("unexpected tls ciphers: %q", argMap["--tls-cipher-suites"])
	}
	if _, ok := argMap["--tls-curve-preferences"]; ok {
		t.Fatalf("curve preferences must be left unset, got %q", argMap["--tls-curve-preferences"])
	}
}

func TestTrustManagerWebhookTLSArgs_tls13OmitsCipherFlags(t *testing.T) {
	spec := &configv1.TLSProfileSpec{
		Ciphers: []string{
			"TLS_AES_128_GCM_SHA256",
			"TLS_AES_256_GCM_SHA384",
			"TLS_CHACHA20_POLY1305_SHA256",
		},
		MinTLSVersion: configv1.VersionTLS13,
	}
	args := TrustManagerWebhookTLSArgs(spec)
	for _, a := range args {
		if strings.HasPrefix(a, "--tls-cipher-suites") {
			t.Fatalf("TLS 1.3 must not set cipher flags, got %q", a)
		}
	}
	got := map[string]string{}
	for _, a := range args {
		parts := strings.SplitN(a, "=", 2)
		if len(parts) != 2 {
			t.Fatalf("bad arg %q", a)
		}
		got[parts[0]] = parts[1]
	}
	if got["--tls-min-version"] != "VersionTLS13" {
		t.Fatalf("min version: got %q", got["--tls-min-version"])
	}
	if _, ok := got["--tls-curve-preferences"]; ok {
		t.Fatalf("curve preferences must be left unset, got %q", got["--tls-curve-preferences"])
	}
	if len(got) != 1 {
		t.Fatalf("unexpected args: %#v", got)
	}
}

func TestIstioCSRServingTLSArgs_nilSpecReturnsEmpty(t *testing.T) {
	args, err := IstioCSRServingTLSArgs(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(args) != 0 {
		t.Fatalf("expected empty args, got %#v", args)
	}
}

func TestIstioCSRServingTLSArgs_repeatsCiphersAndOmitsCurves(t *testing.T) {
	spec := &configv1.TLSProfileSpec{
		Ciphers:       []string{"ECDHE-RSA-AES128-GCM-SHA256", "ECDHE-RSA-AES256-GCM-SHA384"},
		MinTLSVersion: configv1.VersionTLS12,
	}
	args, err := IstioCSRServingTLSArgs(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if args[0] != "--serving-tls-min-version=VersionTLS12" {
		t.Fatalf("unexpected first arg: %q", args[0])
	}

	var ciphers []string
	for _, a := range args[1:] {
		parts := strings.SplitN(a, "=", 2)
		if len(parts) != 2 {
			t.Fatalf("bad arg %q", a)
		}
		switch parts[0] {
		case "--serving-tls-cipher-suites":
			ciphers = append(ciphers, parts[1])
		default:
			t.Fatalf("unexpected arg key %q", parts[0])
		}
	}

	wantCiphers := []string{"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256", "TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384"}
	if !reflect.DeepEqual(ciphers, wantCiphers) {
		t.Fatalf("unexpected cipher-suite flags: got %#v, want %#v", ciphers, wantCiphers)
	}
}

func TestIstioCSRServingTLSArgs_tls13OmitsCipherAndCurveFlags(t *testing.T) {
	spec := &configv1.TLSProfileSpec{
		Ciphers: []string{
			"TLS_AES_128_GCM_SHA256",
			"TLS_AES_256_GCM_SHA384",
			"TLS_CHACHA20_POLY1305_SHA256",
		},
		MinTLSVersion: configv1.VersionTLS13,
	}
	args, err := IstioCSRServingTLSArgs(spec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if args[0] != "--serving-tls-min-version=VersionTLS13" {
		t.Fatalf("unexpected first arg: %q", args[0])
	}
	if len(args) != 1 {
		t.Fatalf("TLS 1.3 must set only the minimum version, got %#v", args)
	}
}

func TestIstioCSRServingTLSArgs_emptyCiphersOmitsCipherFlags(t *testing.T) {
	args, err := IstioCSRServingTLSArgs(&configv1.TLSProfileSpec{
		MinTLSVersion: configv1.VersionTLS12,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(args) != 1 || args[0] != "--serving-tls-min-version=VersionTLS12" {
		t.Fatalf("expected only the min-version flag, got %#v", args)
	}
}

func TestIstioCSRServingTLSArgs_unmappableCiphersReturnsError(t *testing.T) {
	args, err := IstioCSRServingTLSArgs(&configv1.TLSProfileSpec{
		MinTLSVersion: configv1.VersionTLS12,
		Ciphers:       []string{"not-a-real-openssl-cipher-name-xyz"},
	})
	if err == nil {
		t.Fatal("expected error when listed ciphers map to no IANA names")
	}
	if !strings.Contains(err.Error(), "no cipher suites after OpenSSL→IANA mapping") {
		t.Fatalf("unexpected error: %v", err)
	}
	if args != nil {
		t.Fatalf("expected no args, got %#v", args)
	}
}

func TestIstioCSRServingTLSArgs_tls13SkipsUnmappableCipherCheck(t *testing.T) {
	args, err := IstioCSRServingTLSArgs(&configv1.TLSProfileSpec{
		MinTLSVersion: configv1.VersionTLS13,
		Ciphers:       []string{"not-a-real-openssl-cipher-name-xyz"},
	})
	if err != nil {
		t.Fatalf("TLS 1.3 must omit cipher mapping, got %v", err)
	}
	if len(args) != 1 || args[0] != "--serving-tls-min-version=VersionTLS13" {
		t.Fatalf("expected only the min-version flag, got %#v", args)
	}
}

func TestIstioCSRServingTLSArgs_rejectsMinVersionBelowTLS12(t *testing.T) {
	for _, version := range []configv1.TLSProtocolVersion{configv1.VersionTLS10, configv1.VersionTLS11} {
		t.Run(string(version), func(t *testing.T) {
			args, err := IstioCSRServingTLSArgs(&configv1.TLSProfileSpec{
				MinTLSVersion: version,
				Ciphers:       []string{"ECDHE-RSA-AES128-GCM-SHA256"},
			})
			if err == nil {
				t.Fatal("expected error for minimum TLS version below 1.2")
			}
			if !strings.Contains(err.Error(), "Old/TLS 1.0-1.1 profiles are not supported by istio-csr") {
				t.Fatalf("unexpected error: %v", err)
			}
			if args != nil {
				t.Fatalf("expected no args, got %#v", args)
			}
		})
	}

	oldSpec, err := EffectiveSpec(&configv1.TLSSecurityProfile{Type: configv1.TLSProfileOldType})
	if err != nil {
		t.Fatalf("Old profile: %v", err)
	}
	if _, err := IstioCSRServingTLSArgs(oldSpec); err == nil {
		t.Fatal("expected Old profile to be rejected")
	}
}
