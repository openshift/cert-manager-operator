//go:build e2e
// +build e2e

package library

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	configv1 "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
)

func (d DynamicResourceLoader) CreateTestingNS(namespacePrefix string, noSuffix bool) (*corev1.Namespace, error) {
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"e2e-test": "true",
				"operator": "openshift-cert-manager-operator",
			},
		},
	}

	if noSuffix {
		namespace.ObjectMeta.Name = namespacePrefix
	} else {
		namespace.ObjectMeta.GenerateName = fmt.Sprintf("%v-", namespacePrefix)
	}

	var got *corev1.Namespace
	if err := wait.PollImmediate(1*time.Second, 30*time.Second, func() (bool, error) {
		var err error
		got, err = d.KubeClient.CoreV1().Namespaces().Create(context.Background(), namespace, metav1.CreateOptions{})
		if err != nil {
			log.Printf("Error creating namespace: %v", err)
			return false, nil
		}
		return true, nil
	}); err != nil {
		return nil, err
	}
	return got, nil
}

func (d DynamicResourceLoader) DeleteTestingNS(name string, shouldDumpEvents func() bool) (bool, error) {
	ctx := context.Background()
	if shouldDumpEvents() {
		d.DumpEventsInNamespace(name)
	}

	err := d.KubeClient.CoreV1().Namespaces().Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		log.Printf("Error deleting namespace %v, err: %v", name, err)
	}

	if err := wait.PollImmediate(1*time.Second, 30*time.Second, func() (bool, error) {
		// Poll until namespace is deleted
		_, err := d.KubeClient.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
		if err != nil && k8serrors.IsNotFound(err) {
			return true, nil
		}
		return false, nil
	}); err != nil {
		log.Printf("Timed out after 30s waiting for namespace %v to become deleted", name)
		return false, err
	}
	return false, nil
}

func (d DynamicResourceLoader) DumpEventsInNamespace(name string) {
	log.Printf("Dumping events in namespace %s...", name)
	events, err := d.KubeClient.CoreV1().Events(name).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		log.Printf("Error listing events in namespace %s: %v", name, err)
		return
	}

	for _, e := range events.Items {
		log.Printf("At %v - event for %v %v: %v %v: %v", e.FirstTimestamp, e.InvolvedObject.Kind, e.InvolvedObject.Name, e.Source, e.Reason, e.Message)
	}
}

func GetClusterBaseDomain(ctx context.Context, configClient configv1.ConfigV1Interface) (string, error) {
	dns, err := configClient.DNSes().Get(ctx, "cluster", metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return dns.Spec.BaseDomain, nil
}

func ValidateCertificate(certPem string, expectedCommonName string) error {
	block, _ := pem.Decode([]byte(certPem))
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return err
	}

	if cert.Issuer.CommonName != expectedCommonName {
		return fmt.Errorf("expected common name %v, got %v", expectedCommonName, cert.Subject.CommonName)
	}

	now := time.Now()
	if now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
		return fmt.Errorf("certificate is not valid yet")
	}

	return nil
}

func FetchFileFromURL(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to GET the URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("received non-200 response: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	return string(body), nil
}

func IsEmptyString(key interface{}) bool {
	if key == nil {
		return true
	}

	if key.(string) == "" {
		return true
	}

	return false
}

func GenerateCSR(csrTemplate *x509.CertificateRequest) (string, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", fmt.Errorf("failed to generate private key: %w", err)
	}

	csrBytes, err := x509.CreateCertificateRequest(rand.Reader, csrTemplate, privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to create CSR: %w", err)
	}

	csrPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE REQUEST",
		Bytes: csrBytes,
	})

	escapedCSR := strings.ReplaceAll(string(csrPEM), "\n", "\\n")

	return escapedCSR, nil
}
