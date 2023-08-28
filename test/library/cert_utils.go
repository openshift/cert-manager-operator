//go:build e2e
// +build e2e

package library

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
)

func GetX509Certificate(secret *v1.Secret) (*x509.Certificate, error) {
	tlsCrtBytes, ok := secret.Data["tls.crt"]
	if !ok {
		return nil, fmt.Errorf("tls.crt key not found in provided secret %v in %v namespace", secret.Name, secret.Namespace)
	}

	block, _ := pem.Decode(tlsCrtBytes)
	return x509.ParseCertificate(block.Bytes)
}

func GetTLSConfig(secret *v1.Secret) (tls.Config, bool) {
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

func GetCertIssuer(hostname string) (pkix.Name, error) {
	conn, err := tls.Dial("tcp", hostname, nil)
	conn.Close()
	return conn.ConnectionState().PeerCertificates[0].Issuer, err
}

func VerifySecretNotNull(secret *v1.Secret) (bool, error) {
	return len(secret.Data["tls.crt"]) != 0 && len(secret.Data["tls.key"]) != 0, nil
}

func VerifyHostx509Cert(secret *v1.Secret, hostname string) (bool, error) {
	// TODO: verify cert using the x509 package WIP
	cert, err := x509.ParseCertificates((secret.Data["tls.crt"]))
	if err != nil {
		return false, err
	}
	if cert[0].Verify(x509.VerifyOptions{
		DNSName: hostname,
	}); err != nil {
		return false, err
	}
	return true, err
}

func VerifyCertificate(secret *v1.Secret, commonName string) (bool, error) {
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

	// check certificate subject CN
	if cert.Subject.CommonName != commonName {
		return false, fmt.Errorf("incorrect subject CN: %v found in issued certificate", cert.Subject.CommonName)
	}

	return true, nil
}
