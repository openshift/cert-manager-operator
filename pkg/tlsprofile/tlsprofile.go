// Package tlsprofile maps OpenShift API server TLS security profile settings to
// cert-manager operand command-line flags. Curve preferences are not yet exposed
// as cert-manager / trust-manager CLI options; operands still inherit Go's default
// curve ordering for TLS 1.2/1.3 handshakes until upstream adds explicit controls.
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

func joinIANACiphers(openSSLNames []string) string {
	iana := libgocrypto.OpenSSLToIANACipherSuites(openSSLNames)
	return strings.Join(iana, ",")
}
