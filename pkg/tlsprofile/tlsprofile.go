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

// TrustManagerCipherSuiteArgKeys are trust-manager webhook flags that must not be
// set when the effective minimum TLS version is 1.3.
var TrustManagerCipherSuiteArgKeys = []string{
	"--tls-cipher-suites",
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

// TrustManagerWebhookTLSArgs returns trust-manager webhook TLS flags for the
// cluster TLS security profile. Metrics remain plain HTTP upstream and are out
// of scope.
func TrustManagerWebhookTLSArgs(spec *configv1.TLSProfileSpec) []string {
	if spec == nil {
		return []string{}
	}
	minVersion := string(spec.MinTLSVersion)
	if spec.MinTLSVersion == configv1.VersionTLS13 {
		return []string{
			"--tls-min-version=" + minVersion,
		}
	}
	ciphers := joinIANACiphers(spec.Ciphers)
	return []string{
		"--tls-min-version=" + minVersion,
		"--tls-cipher-suites=" + ciphers,
	}
}

func joinIANACiphers(openSSLNames []string) string {
	iana := libgocrypto.OpenSSLToIANACipherSuites(openSSLNames)
	return strings.Join(iana, ",")
}

// ApplyToHTTPServingInfo sets MinTLSVersion and CipherSuites on serving info from
// a resolved cluster TLS profile. For TLS 1.3, cipher suites are cleared so
// library-go defaults are not re-applied over an intentional empty list when
// callers set MinTLSVersion first; callers should set both fields together and
// rely on WithServer's SetRecommended* only filling empty values.
func ApplyToHTTPServingInfo(serving *configv1.HTTPServingInfo, spec *configv1.TLSProfileSpec) error {
	if serving == nil {
		return fmt.Errorf("HTTPServingInfo is nil")
	}
	if spec == nil {
		return fmt.Errorf("TLS profile spec is nil")
	}
	serving.MinTLSVersion = string(spec.MinTLSVersion)
	if spec.MinTLSVersion == configv1.VersionTLS13 {
		// TLS 1.3 ignores CipherSuites in Go; leave empty so defaults are not
		// forced to Intermediate TLS 1.2 suites after MinTLSVersion is set.
		// Set a single TLS 1.3 suite name placeholder? No - empty means
		// SetRecommended will fill Intermediate ciphers. To prevent that,
		// set Modern profile's TLS 1.3 cipher names explicitly when available.
		serving.CipherSuites = append([]string(nil), libgocrypto.OpenSSLToIANACipherSuites(spec.Ciphers)...)
		if len(serving.CipherSuites) == 0 {
			// Keep a non-empty list so SetRecommendedHTTPServingInfoDefaults does
			// not overwrite with Intermediate defaults; TLS 1.3 ignores these.
			serving.CipherSuites = []string{"TLS_AES_128_GCM_SHA256"}
		}
		return nil
	}
	iana := libgocrypto.OpenSSLToIANACipherSuites(spec.Ciphers)
	if len(spec.Ciphers) > 0 && len(iana) == 0 {
		return fmt.Errorf("no cipher suites after OpenSSL→IANA mapping")
	}
	serving.CipherSuites = iana
	return nil
}
