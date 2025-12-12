//go:build e2e

package library

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
)

func GetX509Certificate(secret *corev1.Secret) (*x509.Certificate, error) {
	tlsCrtBytes, ok := secret.Data["tls.crt"]
	if !ok {
		return nil, fmt.Errorf("tls.crt key not found in provided secret %v in %v namespace", secret.Name, secret.Namespace)
	}

	block, _ := pem.Decode(tlsCrtBytes)
	return x509.ParseCertificate(block.Bytes)
}

func GetTLSConfig(secret *corev1.Secret) (tls.Config, bool) {
	roots := x509.NewCertPool()
	ok := roots.AppendCertsFromPEM([]byte(secret.Data["tls.crt"]))
	if !ok {
		return tls.Config{}, ok
	}
	return tls.Config{
		RootCAs:    roots,
		ClientCAs:  roots,
		ClientAuth: tls.RequireAndVerifyClientCert,
		Certificates: []tls.Certificate{
			{
				Certificate: [][]byte{
					[]byte(secret.Data["tls.crt"]),
				},
				PrivateKey: []byte(secret.Data["tls.key"]),
			},
		}}, ok
}

func VerifyHostname(hostname string, tlsConfig *tls.Config) (bool, error) {
	conn, err := tls.Dial("tcp", hostname+":443", tlsConfig.Clone())
	if err != nil {
		return false, err
	}

	err = conn.VerifyHostname(hostname)
	if err != nil {
		return false, err
	}
	conn.Close()
	return true, nil
}

func VerifyExpiry(hostname string, tlsConfig *tls.Config) (bool, error) {
	expirytime, err := GetCertExpiry(hostname, tlsConfig.Clone())
	if err != nil {
		return false, err
	}
	return expirytime.After(time.Now()), err
}

func GetCertExpiry(hostname string, tlsConfig *tls.Config) (time.Time, error) {
	conn, err := tls.Dial("tcp", hostname, tlsConfig.Clone())
	if err != nil {
		return time.Time{}, err
	}
	return conn.ConnectionState().PeerCertificates[0].NotAfter, err
}

func VerifySecretNotNull(secret *corev1.Secret) (bool, error) {
	return len(secret.Data["tls.crt"]) != 0 && len(secret.Data["tls.key"]) != 0, nil
}

func VerifyCertificate(secret *corev1.Secret, commonName string) (bool, error) {
	// check certificate is not null
	isNotNull, err := VerifySecretNotNull(secret)
	if !isNotNull {
		return false, err
	}

	_, ok := GetTLSConfig(secret)
	if !ok {
		return false, fmt.Errorf("failed to read certifcate from secret %v in %v namespace", secret.Name, secret.Namespace)
	}

	cert, err := GetX509Certificate(secret)

	// check certificate expiry
	if err != nil {
		return false, err
	}
	if !cert.NotAfter.After(time.Now()) {
		return false, fmt.Errorf("certificate has expired at %v", cert.NotAfter)
	}

	// Skip identity verification if commonName is empty
	if commonName == "" {
		return true, nil
	}

	// Check certificate subject CN or DNS names (Subject Alternative Names)
	cnMatches := cert.Subject.CommonName == commonName
	dnsMatches := false
	for _, dns := range cert.DNSNames {
		if dns == commonName {
			dnsMatches = true
			break
		}
	}

	if !cnMatches && !dnsMatches {
		return false, fmt.Errorf("commonName '%s' not found in certificate CN (%s) or DNSNames %v",
			commonName, cert.Subject.CommonName, cert.DNSNames)
	}

	return true, nil
}
