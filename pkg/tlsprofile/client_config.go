package tlsprofile

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"

	configv1 "github.com/openshift/api/config/v1"
	libgocrypto "github.com/openshift/library-go/pkg/crypto"
)

// DefaultCurvePreferences is the explicit key-exchange curve order for TLS clients
// (and can be reused for servers) when the cluster TLS profile API does not yet expose
// curve preferences. When openshift/api extends TLSProfileSpec with curves, map those
// fields here instead of using this default.
var DefaultCurvePreferences = []tls.CurveID{
	tls.X25519,
	tls.CurveP256,
	tls.CurveP384,
	tls.CurveP521,
}

// ClientTLSConfig returns a tls.Config for outbound HTTPS/TLS clients using the same
// minimum protocol version, cipher suites, and curve preferences as the given resolved
// TLS profile spec (typically from apiserver.config.openshift.io/cluster).
func ClientTLSConfig(spec *configv1.TLSProfileSpec, rootCAs *x509.CertPool) (*tls.Config, error) {
	if spec == nil {
		return nil, fmt.Errorf("TLS profile spec is nil")
	}
	minVer, err := libgocrypto.TLSVersion(string(spec.MinTLSVersion))
	if err != nil {
		return nil, fmt.Errorf("min TLS version: %w", err)
	}
	tlsConfig := &tls.Config{
		RootCAs:            rootCAs,
		MinVersion:         minVer,
		CurvePreferences:   append([]tls.CurveID(nil), DefaultCurvePreferences...),
		InsecureSkipVerify: false,
	}
	if minVer == tls.VersionTLS13 {
		return tlsConfig, nil
	}
	iana := libgocrypto.OpenSSLToIANACipherSuites(spec.Ciphers)
	if len(spec.Ciphers) > 0 && len(iana) == 0 {
		return nil, fmt.Errorf("no cipher suites after OpenSSL→IANA mapping")
	}
	cipherIDs, err := cipherSuiteIDsFromIANANames(iana)
	if err != nil {
		return nil, err
	}
	tlsConfig.CipherSuites = cipherIDs
	return tlsConfig, nil
}

func cipherSuiteIDsFromIANANames(names []string) ([]uint16, error) {
	if len(names) == 0 {
		return nil, nil
	}
	out := make([]uint16, 0, len(names))
	for _, name := range names {
		id, err := libgocrypto.CipherSuite(name)
		if err != nil {
			return nil, fmt.Errorf("cipher suite %q: %w", name, err)
		}
		out = append(out, id)
	}
	return out, nil
}
