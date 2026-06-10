// Package tlsprofile maps OpenShift API server TLS security profile settings to
// cert-manager and cert-manager-istio-csr operand command-line flags.
package tlsprofile

import (
	"fmt"
	"strings"

	configv1 "github.com/openshift/api/config/v1"
	libgocrypto "github.com/openshift/library-go/pkg/crypto"
)

// EffectiveSpec resolves apiserver.config.openshift.io/cluster
// spec.tlsSecurityProfile into concrete cipher and minimum TLS version settings.
// A nil or empty profile follows API default semantics (Intermediate).
func EffectiveSpec(profile *configv1.TLSSecurityProfile) (*configv1.TLSProfileSpec, error) {
	if profile == nil || profile.Type == "" {
		spec := *configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
		return &spec, nil
	}

	switch profile.Type {
	case configv1.TLSProfileOldType:
		spec := *configv1.TLSProfiles[configv1.TLSProfileOldType]
		return &spec, nil
	case configv1.TLSProfileIntermediateType:
		spec := *configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
		return &spec, nil
	case configv1.TLSProfileModernType:
		spec := *configv1.TLSProfiles[configv1.TLSProfileModernType]
		return &spec, nil
	case configv1.TLSProfileCustomType:
		if profile.Custom == nil {
			return nil, fmt.Errorf("custom TLS profile is missing custom settings")
		}
		custom := profile.Custom.DeepCopy()
		return &custom.TLSProfileSpec, nil
	default:
		spec := *configv1.TLSProfiles[configv1.TLSProfileIntermediateType]
		return &spec, nil
	}
}

// CertManagerWebhookTLSArgs returns cert-manager-webhook flags for the main HTTPS
// listener and the metrics TLS listener when TLS is enabled for metrics.
func CertManagerWebhookTLSArgs(spec *configv1.TLSProfileSpec) []string {
	ciphers := joinIANACiphers(spec.Ciphers)
	minVersion := string(spec.MinTLSVersion)
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
	ciphers := joinIANACiphers(spec.Ciphers)
	minVersion := string(spec.MinTLSVersion)
	return []string{
		"--metrics-tls-min-version=" + minVersion,
		"--metrics-tls-cipher-suites=" + ciphers,
	}
}

// IstioCSRServingGRPCArgs returns cert-manager-istio-csr flags for the gRPC serving
// listener (see upstream --serving-tls-*). Cipher names use the same IANA-style
// identifiers as Kubernetes component-base TLS flags; curve preferences use names
// accepted by istio-csr (X25519, CurveP256, etc.).
func IstioCSRServingGRPCArgs(spec *configv1.TLSProfileSpec) []string {
	ciphers := joinIANACiphers(spec.Ciphers)
	minVersion := string(spec.MinTLSVersion)
	out := []string{
		"--serving-tls-min-version=" + minVersion,
		"--serving-tls-cipher-suites=" + ciphers,
	}
	for _, curve := range istioCSRGRPCCurvePreferenceNames() {
		out = append(out, "--serving-tls-curve-preferences="+curve)
	}
	return out
}

func istioCSRGRPCCurvePreferenceNames() []string {
	// Align with pkg/tlsprofile.DefaultCurvePreferences / openshift library-go TLS defaults.
	return []string{"X25519", "CurveP256", "CurveP384", "CurveP521"}
}

func joinIANACiphers(openSSLNames []string) string {
	iana := libgocrypto.OpenSSLToIANACipherSuites(openSSLNames)
	return strings.Join(iana, ",")
}
