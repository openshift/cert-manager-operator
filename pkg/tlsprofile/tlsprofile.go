// Package tlsprofile maps OpenShift API server TLS security profile settings to
// cert-manager operand command-line flags.
package tlsprofile

import (
	"fmt"
	"strings"

	configv1 "github.com/openshift/api/config/v1"
	libgocrypto "github.com/openshift/library-go/pkg/crypto"
)

func cloneBuiltinProfileSpec(profileType configv1.TLSProfileType) *configv1.TLSProfileSpec {
	spec := *configv1.TLSProfiles[profileType]
	spec.Ciphers = append([]string(nil), spec.Ciphers...)
	return &spec
}

// EffectiveSpec resolves apiserver.config.openshift.io/cluster
// spec.tlsSecurityProfile into concrete cipher and minimum TLS version settings.
// A nil or empty profile follows API default semantics (Intermediate).
func EffectiveSpec(profile *configv1.TLSSecurityProfile) (*configv1.TLSProfileSpec, error) {
	if profile == nil || profile.Type == "" {
		return cloneBuiltinProfileSpec(configv1.TLSProfileIntermediateType), nil
	}

	switch profile.Type {
	case configv1.TLSProfileOldType:
		return cloneBuiltinProfileSpec(configv1.TLSProfileOldType), nil
	case configv1.TLSProfileIntermediateType:
		return cloneBuiltinProfileSpec(configv1.TLSProfileIntermediateType), nil
	case configv1.TLSProfileModernType:
		return cloneBuiltinProfileSpec(configv1.TLSProfileModernType), nil
	case configv1.TLSProfileCustomType:
		if profile.Custom == nil {
			return nil, fmt.Errorf("custom TLS profile is missing custom settings")
		}
		custom := profile.Custom.DeepCopy()
		return &custom.TLSProfileSpec, nil
	default:
		return nil, fmt.Errorf("unrecognized TLSSecurityProfile.Type %q", profile.Type)
	}
}

// CertManagerCipherSuiteArgKeys are operand flags that must not be set when the
// effective minimum TLS version is 1.3 (Go does not honor cipher configuration for TLS 1.3).
var CertManagerCipherSuiteArgKeys = []string{
	"--tls-cipher-suites",
	"--metrics-tls-cipher-suites",
}

// CertManagerWebhookTLSArgs returns cert-manager-webhook flags for the main HTTPS
// listener and the metrics TLS listener when TLS is enabled for metrics.
func CertManagerWebhookTLSArgs(spec *configv1.TLSProfileSpec) []string {
	if spec == nil {
		return []string{}
	}
	minVersion := string(spec.MinTLSVersion)
	if spec.MinTLSVersion == configv1.VersionTLS13 {
		return []string{
			"--tls-min-version=" + minVersion,
			"--metrics-tls-min-version=" + minVersion,
		}
	}
	ciphers := joinIANACiphers(spec.Ciphers)
	return []string{
		"--tls-min-version=" + minVersion,
		"--tls-cipher-suites=" + ciphers,
		"--metrics-tls-min-version=" + minVersion,
		"--metrics-tls-cipher-suites=" + ciphers,
	}
}

// CertManagerOperandMetricsTLSArgs returns flags for cert-manager controller and
// cainjector metrics servers when TLS is configured for metrics.
func CertManagerOperandMetricsTLSArgs(spec *configv1.TLSProfileSpec) []string {
	if spec == nil {
		return []string{}
	}
	minVersion := string(spec.MinTLSVersion)
	if spec.MinTLSVersion == configv1.VersionTLS13 {
		return []string{
			"--metrics-tls-min-version=" + minVersion,
		}
	}
	ciphers := joinIANACiphers(spec.Ciphers)
	return []string{
		"--metrics-tls-min-version=" + minVersion,
		"--metrics-tls-cipher-suites=" + ciphers,
	}
}

func joinIANACiphers(openSSLNames []string) string {
	iana := libgocrypto.OpenSSLToIANACipherSuites(openSSLNames)
	return strings.Join(iana, ",")
}

// istioCSRServingCurvePreferences is the fixed key-exchange curve preference order applied
// to the cert-manager-istio-csr gRPC serving listener. apiserver.config.openshift.io's
// TLSProfileSpec does not expose curve preferences today, so this mirrors the default
// ECDHE/TLS 1.3 group order Go and OpenShift components generally prefer (X25519 first,
// followed by the NIST P-curves). Revisit if TLSProfileSpec grows explicit curve settings.
var istioCSRServingCurvePreferences = []string{"X25519", "CurveP256", "CurveP384", "CurveP521"}

// IstioCSRServingTLSArgs returns cert-manager-istio-csr flags for the gRPC serving
// listener: --serving-tls-min-version, --serving-tls-cipher-suites, and
// --serving-tls-curve-preferences (see cert-manager/istio-csr#787, released in
// cert-manager-istio-csr v0.18.0+). Unlike the cert-manager operand flags, istio-csr
// expects cipher-suites and curve-preferences as a repeated flag (one value per
// occurrence) rather than a single comma-separated value.
//
// TLS 1.3 cipher suites are not configurable in Go, so cipher flags are omitted when
// the effective minimum version is 1.3. Curve preferences are always included because
// Go uses them for both TLS 1.2 ECDHE key exchange and TLS 1.3 group selection.
func IstioCSRServingTLSArgs(spec *configv1.TLSProfileSpec) []string {
	if spec == nil {
		return []string{}
	}
	minVersion := string(spec.MinTLSVersion)
	args := []string{"--serving-tls-min-version=" + minVersion}
	if spec.MinTLSVersion != configv1.VersionTLS13 {
		for _, cipher := range libgocrypto.OpenSSLToIANACipherSuites(spec.Ciphers) {
			args = append(args, "--serving-tls-cipher-suites="+cipher)
		}
	}
	for _, curve := range istioCSRServingCurvePreferences {
		args = append(args, "--serving-tls-curve-preferences="+curve)
	}
	return args
}
