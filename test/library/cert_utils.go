package library

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"time"

	v1 "k8s.io/api/core/v1"
)

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
	return len(secret.Data["ca.crt"]) != 0 && len(secret.Data["tls.crt"]) != 0 && len(secret.Data["tls.key"]) != 0, nil
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
