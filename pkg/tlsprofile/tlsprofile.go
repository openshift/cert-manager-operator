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
