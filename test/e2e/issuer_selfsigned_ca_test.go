//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"path/filepath"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanagermetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Selfsigned and CA Issuer", Ordered, func() {
	var ctx context.Context
	var ns *corev1.Namespace

	BeforeAll(func() {
		ctx = context.Background()

		By("creating a test namespace")
		namespace, err := loader.CreateTestingNS("e2e-self-signed-certs", false)
		Expect(err).NotTo(HaveOccurred())
		ns = namespace

		DeferCleanup(func() {
			loader.DeleteTestingNS(ns.Name, func() bool { return CurrentSpecReport().Failed() })
		})
	})

	BeforeEach(func() {
		By("waiting for operator status to become available")
		err := VerifyHealthyOperatorConditions(certmanageroperatorclient.OperatorV1alpha1())
		Expect(err).NotTo(HaveOccurred(), "Operator is expected to be available")
	})

	Context("with CA issued certificate", func() {
		It("should obtain a self-signed certificate", func() {

			By("creating a self-signed ClusterIssuer")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "cluster_issuer.yaml"), "")
			defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "cluster_issuer.yaml"), "")

			By("creating a certificate using the self-signed ClusterIssuer")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "certificate.yaml"), ns.Name)
			defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "certificate.yaml"), ns.Name)

			By("waiting for certificate to get ready")
			err := waitForCertificateReadiness(ctx, "my-selfsigned-ca", ns.Name)
			Expect(err).NotTo(HaveOccurred())

			By("checking for certificate validity from secret contents")
			err = verifyCertificate(ctx, "root-secret", ns.Name, "my-selfsigned-ca")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should obtain a certificate using CA", func() {

			By("creating CA issuer")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "issuer.yaml"), ns.Name)
			defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "issuer.yaml"), ns.Name)

			By("creating a certificate using the CA Issuer")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "ca_issued_certificate.yaml"), ns.Name)
			defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "ca_issued_certificate.yaml"), ns.Name)

			By("waiting for certificate to get ready")
			err := waitForCertificateReadiness(ctx, "my-ca-issued-cert", ns.Name)
			Expect(err).NotTo(HaveOccurred())

			By("checking for certificate validity from secret contents")
			err = verifyCertificate(ctx, "my-ca-issued-cert", ns.Name, "sample-ca-issued-cert")
			Expect(err).NotTo(HaveOccurred())
		})

		It("should obtain another certificate using CA and renew it", func() {

			By("creating CA issuer")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "issuer.yaml"), ns.Name)
			defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "issuer.yaml"), ns.Name)

			By("creating a certificate using the CA Issuer")
			certName := "renewable-ca-issued-cert"
			cert := &certmanagerv1.Certificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      certName,
					Namespace: ns.Name,
				},
				Spec: certmanagerv1.CertificateSpec{
					DNSNames:    []string{"sample-renewable-cert"},
					SecretName:  certName,
					IsCA:        false,
					Duration:    &metav1.Duration{Duration: time.Hour},
					RenewBefore: &metav1.Duration{Duration: time.Minute * 59}, // essentially becomes a renewal loop of 1min
					IssuerRef: certmanagermetav1.ObjectReference{
						Name:  "my-ca-issuer",
						Kind:  "Issuer",
						Group: "cert-manager.io",
					},
				},
			}
			cert, err := certmanagerClient.CertmanagerV1().Certificates(ns.Name).Create(ctx, cert, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			defer certmanagerClient.CertmanagerV1().Certificates(ns.Name).Delete(ctx, certName, metav1.DeleteOptions{})

			By("waiting for certificate to get ready")
			err = waitForCertificateReadiness(ctx, certName, ns.Name)
			Expect(err).NotTo(HaveOccurred())

			By("certificate was renewed atleast once")
			err = verifyCertificateRenewed(ctx, cert.Spec.SecretName, ns.Name, time.Minute+slowPollInterval) // using wait period of (1min+jitter)=65s
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
