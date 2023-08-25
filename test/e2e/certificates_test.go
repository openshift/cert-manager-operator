//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"path/filepath"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/openshift/cert-manager-operator/test/library"

	v1 "github.com/cert-manager/cert-manager/pkg/apis/acme/v1"
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanagermetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ACME Certificate", Ordered, func() {
	var ctx context.Context
	var appsDomain string

	BeforeAll(func() {
		By("creating Kube clients")
		ctx = context.Background()

		baseDomain, err := library.GetClusterBaseDomain(ctx, configClient)
		Expect(err).NotTo(HaveOccurred())
		Expect(baseDomain).NotTo(BeEmpty())
		appsDomain = "apps." + baseDomain

		By("adding override args for dns-01 private zone passthrough")
		err = addOverrideArgs(certmanageroperatorclient, certmanagerControllerDeployment, []string{
			"--dns01-recursive-nameservers-only",
			"--dns01-recursive-nameservers=8.8.8.8:53,1.1.1.1:53",
		})
		Expect(err).NotTo(HaveOccurred())

		DeferCleanup(func() {
			By("resetting cert-manager state")
			err = resetCertManagerState(ctx, certmanageroperatorclient, loader)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	BeforeEach(func() {
		By("waiting for operator status to become available")
		err := verifyOperatorStatusCondition(certmanageroperatorclient,
			[]string{certManagerControllerDeploymentControllerName,
				certManagerWebhookDeploymentControllerName,
				certManagerCAInjectorDeploymentControllerName},
			validOperatorStatusConditions)
		Expect(err).NotTo(HaveOccurred(), "Operator is expected to be available")
	})

	Context("dns-01 challenge using explicit credentials", func() {
		It("should obtain a valid LetsEncrypt certificate", func() {

			By("creating a test namespace")
			ns, err := loader.CreateTestingNS("e2e-acme-explicit-dns01")
			Expect(err).NotTo(HaveOccurred())
			defer loader.DeleteTestingNS(ns.Name)

			By("obtaining AWS credentials from kube-system namespace")
			awsCredsSecret, err := loader.KubeClient.CoreV1().Secrets("kube-system").Get(ctx, "aws-creds", metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())

			awsAccessKeyID := awsCredsSecret.Data["aws_access_key_id"]
			awsSecretAccessKey := awsCredsSecret.Data["aws_secret_access_key"]

			By("copying AWS secret access key to test namespace")
			secretName := "aws-secret"
			secretKey := "aws_secret_access_key"
			awsSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: ns.Name,
				},
				Data: map[string][]byte{
					secretKey: awsSecretAccessKey,
				},
			}
			_, err = loader.KubeClient.CoreV1().Secrets(ns.Name).Create(ctx, awsSecret, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("getting AWS zone from Infrastructure object")
			infra, err := configClient.Infrastructures().Get(ctx, "cluster", metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())

			awsRegion := infra.Status.PlatformStatus.AWS.Region
			Expect(awsRegion).NotTo(Equal(""))

			By("creating new certificate Issuer")
			issuerName := "letsencrypt-dns01"
			issuer := &certmanagerv1.Issuer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      issuerName,
					Namespace: ns.Name,
				},
				Spec: certmanagerv1.IssuerSpec{
					IssuerConfig: certmanagerv1.IssuerConfig{
						ACME: &v1.ACMEIssuer{
							Server: "https://acme-staging-v02.api.letsencrypt.org/directory",
							PrivateKey: certmanagermetav1.SecretKeySelector{
								LocalObjectReference: certmanagermetav1.LocalObjectReference{
									Name: "letsencrypt-dns01-issuer",
								},
							},
							Solvers: []v1.ACMEChallengeSolver{
								{
									DNS01: &v1.ACMEChallengeSolverDNS01{
										Route53: &v1.ACMEIssuerDNS01ProviderRoute53{
											AccessKeyID: string(awsAccessKeyID),
											SecretAccessKey: certmanagermetav1.SecretKeySelector{
												LocalObjectReference: certmanagermetav1.LocalObjectReference{
													Name: secretName,
												},
												Key: secretKey,
											},
											Region: awsRegion,
										},
									},
								},
							},
						},
					},
				},
			}
			_, err = certmanagerClient.CertmanagerV1().Issuers(ns.Name).Create(ctx, issuer, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			defer certmanagerClient.CertmanagerV1().Issuers(ns.Name).Delete(ctx, issuerName, metav1.DeleteOptions{})

			By("creating new certificate")
			certDomain := "adre." + appsDomain // acronym for "ACME dns-01 Route53 Explicit", short naming to pass dns name validation
			certName := "letsencrypt-cert"
			cert := &certmanagerv1.Certificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      certName,
					Namespace: ns.Name,
				},
				Spec: certmanagerv1.CertificateSpec{
					IsCA:       false,
					CommonName: certDomain,
					SecretName: certName,
					DNSNames:   []string{certDomain},
					IssuerRef: certmanagermetav1.ObjectReference{
						Name: issuerName,
						Kind: "Issuer",
					},
				},
			}

			_, err = certmanagerClient.CertmanagerV1().Certificates(ns.Name).Create(ctx, cert, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			defer certmanagerClient.CertmanagerV1().Certificates(ns.Name).Delete(ctx, certName, metav1.DeleteOptions{})

			By("waiting for certificate to get ready")
			err = waitForCertificateReadiness(ctx, certName, ns.Name)
			Expect(err).NotTo(HaveOccurred())

			By("checking for certificate validity from secret contents")
			err = verifyCertificate(ctx, certName, ns.Name, certDomain)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("dns-01 challenge using ambient credentials", func() {
		It("should obtain a valid LetsEncrypt certificate", func() {

			By("creating a test namespace")
			ns, err := loader.CreateTestingNS("e2e-acme-ambient-dns01")
			Expect(err).NotTo(HaveOccurred())
			defer loader.DeleteTestingNS(ns.Name)

			By("creating CredentialsRequest object")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "credentials", "credentialsrequest_aws.yaml"), "")

			By("waiting for cloud secret to be available")
			err = wait.PollImmediate(PollInterval, TestTimeout, func() (bool, error) {
				_, err := loader.KubeClient.CoreV1().Secrets("cert-manager").Get(ctx, "aws-creds", metav1.GetOptions{})
				if err != nil {
					return true, nil
				}
				return false, nil
			})
			Expect(err).NotTo(HaveOccurred())

			By("setting cloud credential secret name in subscription object")
			err = patchSubscriptionWithCloudCredential(ctx, loader)
			Expect(err).NotTo(HaveOccurred())

			By("getting AWS zone from Infrastructure object")
			infra, err := configClient.Infrastructures().Get(ctx, "cluster", metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())

			awsRegion := infra.Status.PlatformStatus.AWS.Region
			Expect(awsRegion).NotTo(Equal(""))

			By("creating new certificate ClusterIssuer")
			clusterIssuerName := "letsencrypt-dns01-ambient"
			clusterIssuer := &certmanagerv1.ClusterIssuer{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterIssuerName,
				},
				Spec: certmanagerv1.IssuerSpec{
					IssuerConfig: certmanagerv1.IssuerConfig{
						ACME: &v1.ACMEIssuer{
							Server: "https://acme-staging-v02.api.letsencrypt.org/directory",
							PrivateKey: certmanagermetav1.SecretKeySelector{
								LocalObjectReference: certmanagermetav1.LocalObjectReference{
									Name: "letsencrypt-dns01-issuer",
								},
							},
							Solvers: []v1.ACMEChallengeSolver{
								{
									DNS01: &v1.ACMEChallengeSolverDNS01{
										Route53: &v1.ACMEIssuerDNS01ProviderRoute53{
											Region: awsRegion,
										},
									},
								},
							},
						},
					},
				},
			}
			_, err = certmanagerClient.CertmanagerV1().ClusterIssuers().Create(ctx, clusterIssuer, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			defer certmanagerClient.CertmanagerV1().ClusterIssuers().Delete(ctx, clusterIssuerName, metav1.DeleteOptions{})

			By("creating new certificate")
			certDomain := "adra." + appsDomain // acronym for "ACME dns-01 Route53 Ambient", short naming to pass dns name validation
			certName := "letsencrypt-cert"
			cert := &certmanagerv1.Certificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      certName,
					Namespace: ns.Name,
				},
				Spec: certmanagerv1.CertificateSpec{
					IsCA:       false,
					CommonName: certDomain,
					SecretName: certName,
					DNSNames:   []string{certDomain},
					IssuerRef: certmanagermetav1.ObjectReference{
						Name: clusterIssuerName,
						Kind: "ClusterIssuer",
					},
				},
			}

			_, err = certmanagerClient.CertmanagerV1().Certificates(ns.Name).Create(ctx, cert, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			defer certmanagerClient.CertmanagerV1().Certificates(ns.Name).Delete(ctx, certName, metav1.DeleteOptions{})

			By("waiting for certificate to get ready")
			err = waitForCertificateReadiness(ctx, certName, ns.Name)
			Expect(err).NotTo(HaveOccurred())

			By("checking for certificate validity from secret contents")
			err = verifyCertificate(ctx, certName, ns.Name, certDomain)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("http-01 challenge using ingress", func() {
		It("should obtain a valid LetsEncrypt certificate", func() {

			By("creating a test namespace")
			ns, err := loader.CreateTestingNS("e2e-acme-explicit-dns01")
			Expect(err).NotTo(HaveOccurred())
			defer loader.DeleteTestingNS(ns.Name)

			By("creating a cluster issuer")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "acme", "clusterissuer.yaml"), ns.Name)
			defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "acme", "clusterissuer.yaml"), ns.Name)

			By("creating an openshift-hello deployment")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "acme", "deployment.yaml"), ns.Name)
			defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "acme", "deployment.yaml"), ns.Name)

			By("creating a service for the deployment openshift-hello")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "acme", "service.yaml"), ns.Name)
			defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "acme", "service.yaml"), ns.Name)

			By("creating an Ingress object")
			ingressHost := "ahi." + appsDomain // acronym for "ACME http-01 Ingress", short naming to pass dns name validation
			pathType := networkingv1.PathTypePrefix
			secretName := "ingress-prod-secret"
			ingress := &networkingv1.Ingress{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "networking.k8s.io/v1",
					Kind:       "Ingress",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ingress-le-prod",
					Namespace: ns.Name,
					Annotations: map[string]string{
						"cert-manager.io/cluster-issuer":            "letsencrypt-prod",
						"acme.cert-manager.io/http01-ingress-class": "openshift-default",
					},
				},
				Spec: networkingv1.IngressSpec{
					Rules: []networkingv1.IngressRule{
						{
							Host: ingressHost,
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											Path:     "/",
											PathType: &pathType,
											Backend:  networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "hello-openshift", Port: networkingv1.ServiceBackendPort{Number: 8080}}},
										},
									},
								},
							},
						},
					},
					TLS: []networkingv1.IngressTLS{{
						Hosts:      []string{ingressHost},
						SecretName: secretName,
					}},
				},
			}
			ingress, err = loader.KubeClient.NetworkingV1().Ingresses(ingress.ObjectMeta.Namespace).Create(ctx, ingress, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			defer loader.KubeClient.NetworkingV1().Ingresses(ingress.ObjectMeta.Namespace).Delete(ctx, ingress.ObjectMeta.Name, metav1.DeleteOptions{})

			By("checking TLS certificate contents")
			err = wait.PollImmediate(PollInterval, TestTimeout, func() (bool, error) {
				secret, err := loader.KubeClient.CoreV1().Secrets(ingress.ObjectMeta.Namespace).Get(ctx, secretName, metav1.GetOptions{})
				tlsConfig, isvalid := library.GetTLSConfig(secret)
				if !isvalid {
					return false, nil
				}
				isHostCorrect, err := library.VerifyHostname(ingressHost, tlsConfig.Clone())
				if err != nil {
					return false, nil
				}
				isNotExpired, err := library.VerifyExpiry(ingressHost+":443", tlsConfig.Clone())
				if err != nil {
					return false, nil
				}

				return isHostCorrect && isNotExpired, nil
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})
})

var _ = Describe("Self-signed Certificate", Ordered, func() {
	var ctx context.Context
	var ns *corev1.Namespace

	BeforeAll(func() {
		ctx = context.Background()

		By("creating a test namespace")
		namespace, err := loader.CreateTestingNS("e2e-self-signed-certs")
		Expect(err).NotTo(HaveOccurred())
		ns = namespace

		DeferCleanup(func() {
			loader.DeleteTestingNS(ns.Name)
		})
	})

	BeforeEach(func() {
		By("waiting for operator status to become available")
		err := verifyOperatorStatusCondition(certmanageroperatorclient,
			[]string{certManagerControllerDeploymentControllerName,
				certManagerWebhookDeploymentControllerName,
				certManagerCAInjectorDeploymentControllerName},
			validOperatorStatusConditions)
		Expect(err).NotTo(HaveOccurred(), "Operator is expected to be available")
	})

	Context("with CA issued certificate", func() {
		It("should obtain a self-signed certificate", func() {

			By("creating a self-signed ClusterIssuer")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "cluster_issuer.yaml"), "")
			defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "cluster_issuer.yaml"), "")

			By("creating a certificate using the self-signed ClusterIssuer")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "certificate.yaml"), ns.Name)
			defer loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "certificate.yaml"), ns.Name)

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
			cert := &certmanagerv1.Certificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "renewable-ca-issued-cert",
					Namespace: ns.Name,
				},
				Spec: certmanagerv1.CertificateSpec{
					DNSNames:    []string{"sample-renewable-cert"},
					SecretName:  "renewable-ca-issued-cert",
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
			defer certmanagerClient.CertmanagerV1().Certificates(ns.Name).Delete(ctx, cert.Name, metav1.DeleteOptions{})

			By("waiting for certificate to get ready")
			err = waitForCertificateReadiness(ctx, "renewable-ca-issued-cert", ns.Name)
			Expect(err).NotTo(HaveOccurred())

			By("certificate was renewed atleast once")
			err = verifyCertificateRenewed(ctx, cert.Spec.SecretName, ns.Name, time.Minute+5*time.Second) // using wait period of (1min+jitter)=65s
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
