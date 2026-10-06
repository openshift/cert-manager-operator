//go:build e2e
// +build e2e

package e2e

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"os/exec"
	"regexp"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	metricsCASecretName     = "cert-manager-metrics-ca"
	webhookCASecretName     = "cert-manager-webhook-ca"
	metricsPort             = 9402
	webhookSecurePort       = 10250
	trustManagerWebhookPort = 6443
)

var portForwardAddrRE = regexp.MustCompile(`Forwarding from 127\.0\.0\.1:(\d+)`)

type tlsProbe struct {
	minVersion   uint16
	maxVersion   uint16
	cipherSuites []uint16
}

func tls13Only() tlsProbe {
	return tlsProbe{minVersion: tls.VersionTLS13, maxVersion: tls.VersionTLS13}
}

func tls12Only() tlsProbe {
	return tlsProbe{minVersion: tls.VersionTLS12, maxVersion: tls.VersionTLS12}
}

func tls12Cipher(id uint16) tlsProbe {
	return tlsProbe{
		minVersion:   tls.VersionTLS12,
		maxVersion:   tls.VersionTLS12,
		cipherSuites: []uint16{id},
	}
}

func tls10Only() tlsProbe {
	return tlsProbe{minVersion: tls.VersionTLS10, maxVersion: tls.VersionTLS10}
}

func handshakeTLS(addr, serverName string, roots *x509.CertPool, probe tlsProbe) error {
	cfg := &tls.Config{
		RootCAs:            roots,
		ServerName:         serverName,
		MinVersion:         probe.minVersion,
		MaxVersion:         probe.maxVersion,
		CipherSuites:       probe.cipherSuites,
		InsecureSkipVerify: false,
	}
	d := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: 8 * time.Second},
		Config:    cfg,
	}
	conn, err := d.Dial("tcp", addr)
	if err != nil {
		return err
	}
	return conn.Close()
}

func certPoolFromTLSSecret(ctx context.Context, namespace, secretName string) (*x509.CertPool, error) {
	secret, err := k8sClientSet.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	pemBytes := secret.Data[corev1.ServiceAccountRootCAKey]
	if len(pemBytes) == 0 {
		pemBytes = secret.Data["ca.crt"]
	}
	if len(pemBytes) == 0 {
		pemBytes = secret.Data[corev1.TLSCertKey]
	}
	if len(pemBytes) == 0 {
		return nil, fmt.Errorf("secret %s/%s has no ca.crt or tls.crt", namespace, secretName)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemBytes) {
		return nil, fmt.Errorf("secret %s/%s: failed to parse CA PEM", namespace, secretName)
	}
	return pool, nil
}

func readyPodName(ctx context.Context, namespace, deploymentName string) (string, error) {
	deploy, err := k8sClientSet.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	selector, err := metav1.LabelSelectorAsSelector(deploy.Spec.Selector)
	if err != nil {
		return "", err
	}
	pods, err := k8sClientSet.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return "", err
	}
	for i := range pods.Items {
		if isPodReady(&pods.Items[i]) {
			return pods.Items[i].Name, nil
		}
	}
	return "", fmt.Errorf("no ready pod for deployment %s/%s", namespace, deploymentName)
}

func kubeCLI() string {
	if _, err := exec.LookPath("oc"); err == nil {
		return "oc"
	}
	return "kubectl"
}

func startPortForward(ctx context.Context, namespace, podName string, remotePort int) (localAddr string, stop func(), err error) {
	cli := kubeCLI()
	cmd := exec.CommandContext(ctx, cli, "port-forward", "--address=127.0.0.1", "-n", namespace, "pod/"+podName, fmt.Sprintf(":%d", remotePort))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", nil, err
	}
	if err := cmd.Start(); err != nil {
		return "", nil, fmt.Errorf("start %s port-forward: %w", cli, err)
	}

	localPort, scanErr := waitPortForwardLocalPort(io.MultiReader(stdout, stderr), 20*time.Second)
	if scanErr != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return "", nil, scanErr
	}

	stop = func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
	return fmt.Sprintf("127.0.0.1:%d", localPort), stop, nil
}

func waitPortForwardLocalPort(r io.Reader, timeout time.Duration) (int, error) {
	errCh := make(chan error, 1)
	portCh := make(chan int, 1)
	go func() {
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			m := portForwardAddrRE.FindStringSubmatch(scanner.Text())
			if len(m) == 2 {
				var p int
				if _, err := fmt.Sscanf(m[1], "%d", &p); err == nil && p > 0 {
					portCh <- p
					return
				}
			}
		}
		if err := scanner.Err(); err != nil {
			errCh <- err
			return
		}
		errCh <- fmt.Errorf("port-forward ended before advertising a local port")
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case p := <-portCh:
		return p, nil
	case err := <-errCh:
		return 0, err
	case <-timer.C:
		return 0, fmt.Errorf("timed out waiting for port-forward bind")
	}
}

func withPortForward(ctx context.Context, namespace, podName string, remotePort int, fn func(localAddr string) error) error {
	addr, stop, err := startPortForward(ctx, namespace, podName, remotePort)
	if err != nil {
		return err
	}
	defer stop()

	deadline := time.Now().Add(15 * time.Second)
	for {
		conn, dialErr := net.DialTimeout("tcp", addr, 2*time.Second)
		if dialErr == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("port-forward %s not ready: %w", addr, dialErr)
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fn(addr)
}

func handshakeOperandPort(ctx context.Context, deploymentName, serverName, caSecret string, port int, probe tlsProbe) error {
	podName, err := readyPodName(ctx, operandNamespace, deploymentName)
	if err != nil {
		return err
	}
	roots, err := certPoolFromTLSSecret(ctx, operandNamespace, caSecret)
	if err != nil {
		return err
	}
	return withPortForward(ctx, operandNamespace, podName, port, func(localAddr string) error {
		var last error
		for i := 0; i < 5; i++ {
			last = handshakeTLS(localAddr, serverName, roots, probe)
			if last == nil {
				return nil
			}
			time.Sleep(300 * time.Millisecond)
		}
		return last
	})
}

func metricsServerName(serviceName string) string {
	return serviceName + "." + operandNamespace + ".svc"
}

func TestHandshakeTLSRejectsLowerProtocol(t *testing.T) {
	ca, certPEM, keyPEM := mustSelfSignedServer(t)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{mustX509KeyPair(t, certPEM, keyPEM)},
		MinVersion:   tls.VersionTLS13,
		MaxVersion:   tls.VersionTLS13,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 1)
				_, _ = c.Read(buf)
			}(c)
		}
	}()

	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		t.Fatal("ca pem")
	}
	if err := handshakeTLS(ln.Addr().String(), "tls-probe.test", roots, tls13Only()); err != nil {
		t.Fatalf("TLS 1.3: %v", err)
	}
	if err := handshakeTLS(ln.Addr().String(), "tls-probe.test", roots, tls12Only()); err == nil {
		t.Fatal("expected TLS 1.2 handshake to fail against TLS 1.3-only server")
	}
}

func mustSelfSignedServer(t *testing.T) (caPEM, certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "tls-probe.test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{"tls-probe.test"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyBytes, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyBytes})
	return certPEM, certPEM, keyPEM
}

func mustX509KeyPair(t *testing.T, certPEM, keyPEM []byte) tls.Certificate {
	t.Helper()
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}
