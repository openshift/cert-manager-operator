//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8sresource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/openshift/cert-manager-operator/test/library"

	acmev1 "github.com/cert-manager/cert-manager/pkg/apis/acme/v1"
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanagermetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	// letsEncryptStagingServerURL is the address for the Let's Encrypt staging environment server.
	letsEncryptStagingServerURL = "https://acme-staging-v02.api.letsencrypt.org/directory"

	// acmeSolverPodLabel is the label that cert-manager uses to identify ACME solver pods.
	acmeSolverPodLabel = "acme.cert-manager.io/http01-solver"

	// acmeSolverContainerName is the name of the container in the ACME solver pod.
	acmeSolverContainerName = "acmesolver"

	// TARGET_PLATFORM is the environment variable for IBM Cloud CIS test.
	targetPlatformEnvironmentVar = "TARGET_PLATFORM"

	// CIS_CRN is the required environment variable for IBM Cloud platform.
	cisCRNEnvironmentVar = "CIS_CRN"
)

var _ = Describe("ACME Certificate", Ordered, func() {
	var ctx context.Context
	var ns *corev1.Namespace
	var appsDomain string
	var baseDomain string

	BeforeAll(func() {
		By("creating Kube clients")
		ctx = context.Background()
		var err error
		baseDomain, err = library.GetClusterBaseDomain(ctx, configClient)
		Expect(err).NotTo(HaveOccurred())
		Expect(baseDomain).NotTo(BeEmpty())
		appsDomain = "apps." + baseDomain

		By("adding required args to cert-manager controller")
		err = addOverrideArgs(certmanageroperatorclient, certmanagerControllerDeployment, []string{
			// for dns-01 private zone passthrough
			"--dns01-recursive-nameservers-only",
			"--dns01-recursive-nameservers=8.8.8.8:53,1.1.1.1:53",
			// for Issuer to use ambient credentials
			"--issuer-ambient-credentials",
			// for http-01 solver ingress
			"--acme-http01-solver-resource-limits-cpu=150m",
			"--acme-http01-solver-resource-limits-memory=200Mi",
			"--acme-http01-solver-resource-request-cpu=100m",
			"--acme-http01-solver-resource-request-memory=100Mi",
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
		err := VerifyHealthyOperatorConditions(certmanageroperatorclient.OperatorV1alpha1())
		Expect(err).NotTo(HaveOccurred(), "Operator is expected to be available")

		By("creating a test namespace")
		namespace, err := loader.CreateTestingNS("e2e-acme-certs", false)
		Expect(err).NotTo(HaveOccurred())
		ns = namespace

		DeferCleanup(func() {
			loader.DeleteTestingNS(ns.Name, func() bool { return CurrentSpecReport().Failed() })
		})
	})

	Context("dns-01 challenge with AWS Route53", Label("Platform:AWS"), func() {
		It("should obtain a valid LetsEncrypt certificate using explicit credentials", func() {

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
						ACME: &acmev1.ACMEIssuer{
							Server: letsEncryptStagingServerURL,
							PrivateKey: certmanagermetav1.SecretKeySelector{
								LocalObjectReference: certmanagermetav1.LocalObjectReference{
									Name: "letsencrypt-dns01-issuer",
								},
							},
							Solvers: []acmev1.ACMEChallengeSolver{
								{
									DNS01: &acmev1.ACMEChallengeSolverDNS01{
										Route53: &acmev1.ACMEIssuerDNS01ProviderRoute53{
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
			certDomain := fmt.Sprintf("adre-%s.%s", randomStr(3), appsDomain) // acronym for "ACME dns-01 Route53 Explicit"
			commonName := certDomain
			if len(certDomain) > 64 {
				commonName = ""
			}
			certName := "letsencrypt-cert"
			cert := &certmanagerv1.Certificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      certName,
					Namespace: ns.Name,
				},
				Spec: certmanagerv1.CertificateSpec{
					IsCA:       false,
					CommonName: commonName,
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
			err = verifyCertificate(ctx, certName, ns.Name, commonName)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should obtain a valid LetsEncrypt certificate using ambient credentials with ClusterIssuer", func() {

			By("creating CredentialsRequest object")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "credentials", "credentialsrequest_aws.yaml"), "")

			By("waiting for cloud secret to be available")
			err := wait.PollUntilContextTimeout(context.TODO(), slowPollInterval, highTimeout, true, func(context.Context) (bool, error) {
				_, err := loader.KubeClient.CoreV1().Secrets("cert-manager").Get(ctx, "aws-creds", metav1.GetOptions{})
				if err != nil {
					return false, nil
				}
				return true, nil
			})
			Expect(err).NotTo(HaveOccurred())

			By("setting cloud credential secret name in subscription object")
			err = patchSubscriptionWithEnvVars(ctx, loader, map[string]string{
				"CLOUD_CREDENTIALS_SECRET_NAME": "aws-creds",
			})
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
						ACME: &acmev1.ACMEIssuer{
							Server: letsEncryptStagingServerURL,
							PrivateKey: certmanagermetav1.SecretKeySelector{
								LocalObjectReference: certmanagermetav1.LocalObjectReference{
									Name: "letsencrypt-dns01-issuer",
								},
							},
							Solvers: []acmev1.ACMEChallengeSolver{
								{
									DNS01: &acmev1.ACMEChallengeSolverDNS01{
										Route53: &acmev1.ACMEIssuerDNS01ProviderRoute53{
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
			certDomain := fmt.Sprintf("adra-%s.%s", randomStr(3), appsDomain) // acronym for "ACME dns-01 Route53 Ambient"
			commonName := certDomain
			if len(certDomain) > 64 {
				commonName = ""
			}
			certName := "letsencrypt-cert"
			cert := &certmanagerv1.Certificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      certName,
					Namespace: ns.Name,
				},
				Spec: certmanagerv1.CertificateSpec{
					IsCA:       false,
					CommonName: commonName,
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
			err = verifyCertificate(ctx, certName, ns.Name, commonName)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should obtain a valid LetsEncrypt certificate using ambient credentials with Issuer", func() {

			By("creating CredentialsRequest object")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "credentials", "credentialsrequest_aws.yaml"), "")

			By("waiting for cloud secret to be available")
			err := wait.PollUntilContextTimeout(context.TODO(), slowPollInterval, highTimeout, true, func(context.Context) (bool, error) {
				_, err := loader.KubeClient.CoreV1().Secrets("cert-manager").Get(ctx, "aws-creds", metav1.GetOptions{})
				if err != nil {
					return false, nil
				}
				return true, nil
			})
			Expect(err).NotTo(HaveOccurred())

			By("setting cloud credential secret name in subscription object")
			err = patchSubscriptionWithEnvVars(ctx, loader, map[string]string{
				"CLOUD_CREDENTIALS_SECRET_NAME": "aws-creds",
			})
			Expect(err).NotTo(HaveOccurred())

			By("getting AWS zone from Infrastructure object")
			infra, err := configClient.Infrastructures().Get(ctx, "cluster", metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())

			awsRegion := infra.Status.PlatformStatus.AWS.Region
			Expect(awsRegion).NotTo(Equal(""))

			By("creating new certificate Issuer")
			issuerName := "letsencrypt-dns01-ambient"
			issuer := &certmanagerv1.Issuer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      issuerName,
					Namespace: ns.Name,
				},
				Spec: certmanagerv1.IssuerSpec{
					IssuerConfig: certmanagerv1.IssuerConfig{
						ACME: &acmev1.ACMEIssuer{
							Server: letsEncryptStagingServerURL,
							PrivateKey: certmanagermetav1.SecretKeySelector{
								LocalObjectReference: certmanagermetav1.LocalObjectReference{
									Name: "letsencrypt-dns01-issuer",
								},
							},
							Solvers: []acmev1.ACMEChallengeSolver{
								{
									DNS01: &acmev1.ACMEChallengeSolverDNS01{
										Route53: &acmev1.ACMEIssuerDNS01ProviderRoute53{
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
			certDomain := fmt.Sprintf("adra-%s.%s", randomStr(3), appsDomain) // acronym for "ACME dns-01 Route53 Ambient"
			commonName := certDomain
			if len(certDomain) > 64 {
				commonName = ""
			}
			certName := "letsencrypt-cert"
			cert := &certmanagerv1.Certificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      certName,
					Namespace: ns.Name,
				},
				Spec: certmanagerv1.CertificateSpec{
					IsCA:       false,
					CommonName: commonName,
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
			err = verifyCertificate(ctx, certName, ns.Name, commonName)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("dns-01 challenge with Google CloudDNS", Label("Platform:GCP"), func() {
		It("should obtain a valid LetsEncrypt certificate using explicit credentials with ClusterIssuer", func() {

			By("obtaining GCP credentials from kube-system namespace")
			gcpCredsSecret, err := loader.KubeClient.CoreV1().Secrets("kube-system").Get(ctx, "gcp-credentials", metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			gcpServiceAccount := gcpCredsSecret.Data["service_account.json"]

			By("copying GCP secret service account to test namespace")
			secretName := "gcp-secret"
			secretKey := "gcp_service_account_key.json"
			gcpSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: ns.Name,
				},
				Data: map[string][]byte{
					secretKey: gcpServiceAccount,
				},
			}
			_, err = loader.KubeClient.CoreV1().Secrets(ns.Name).Create(ctx, gcpSecret, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("getting GCP project ID from Infrastructure object")
			infra, err := configClient.Infrastructures().Get(ctx, "cluster", metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())

			gcpProjectID := infra.Status.PlatformStatus.GCP.ProjectID
			Expect(gcpProjectID).NotTo(Equal(""))

			By("creating new certificate Issuer")
			issuerName := "letsencrypt-dns01"
			issuer := &certmanagerv1.Issuer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      issuerName,
					Namespace: ns.Name,
				},
				Spec: certmanagerv1.IssuerSpec{
					IssuerConfig: certmanagerv1.IssuerConfig{
						ACME: &acmev1.ACMEIssuer{
							Server: letsEncryptStagingServerURL,
							PrivateKey: certmanagermetav1.SecretKeySelector{
								LocalObjectReference: certmanagermetav1.LocalObjectReference{
									Name: "letsencrypt-dns01-issuer",
								},
							},
							Solvers: []acmev1.ACMEChallengeSolver{
								{
									DNS01: &acmev1.ACMEChallengeSolverDNS01{
										CloudDNS: &acmev1.ACMEIssuerDNS01ProviderCloudDNS{
											Project: string(gcpProjectID),
											ServiceAccount: &certmanagermetav1.SecretKeySelector{
												LocalObjectReference: certmanagermetav1.LocalObjectReference{
													Name: secretName,
												},
												Key: secretKey,
											},
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
			certDomain := fmt.Sprintf("adgce-%s.%s", randomStr(3), appsDomain) // acronym for "ACME dns-01 Google CloudDNS Explicit"
			commonName := certDomain
			if len(certDomain) > 64 {
				commonName = ""
			}
			certName := "letsencrypt-cert"
			cert := &certmanagerv1.Certificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      certName,
					Namespace: ns.Name,
				},
				Spec: certmanagerv1.CertificateSpec{
					IsCA:       false,
					CommonName: commonName,
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
			err = verifyCertificate(ctx, certName, ns.Name, commonName)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should obtain a valid LetsEncrypt certificate using ambient credentials with ClusterIssuer", func() {

			By("Creating CredentialsRequest object")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "credentials", "credentialsrequest_gcp.yaml"), "")

			By("Waiting for cloud secret to be available")
			// The name is defined cloud credential by the testdata YAML file.
			credentialSecret := "gcp-credentials"
			err := wait.PollUntilContextTimeout(context.TODO(), slowPollInterval, highTimeout, true, func(context.Context) (bool, error) {
				_, err := loader.KubeClient.CoreV1().Secrets("cert-manager").Get(ctx, credentialSecret, metav1.GetOptions{})
				if err != nil {
					return false, nil
				}
				return true, nil
			})
			Expect(err).NotTo(HaveOccurred())

			By("Configure cert-manager to use credential, setting this credential secret name in subscription object")
			err = patchSubscriptionWithEnvVars(ctx, loader, map[string]string{
				"CLOUD_CREDENTIALS_SECRET_NAME": credentialSecret,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Getting GCP project ID from Infrastructure object")
			infra, err := configClient.Infrastructures().Get(ctx, "cluster", metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())

			gcpProjectId := infra.Status.PlatformStatus.GCP.ProjectID
			Expect(gcpProjectId).NotTo(Equal(""))

			By("Creating new certificate ClusterIssuer")
			clusterIssuerName := "acme-dns01-clouddns-ambient"
			clusterIssuer := &certmanagerv1.ClusterIssuer{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterIssuerName,
				},
				Spec: certmanagerv1.IssuerSpec{
					IssuerConfig: certmanagerv1.IssuerConfig{
						ACME: &acmev1.ACMEIssuer{
							Server: letsEncryptStagingServerURL,
							PrivateKey: certmanagermetav1.SecretKeySelector{
								LocalObjectReference: certmanagermetav1.LocalObjectReference{
									Name: "letsencrypt-dns01-issuer",
								},
							},
							Solvers: []acmev1.ACMEChallengeSolver{
								{
									DNS01: &acmev1.ACMEChallengeSolverDNS01{
										CloudDNS: &acmev1.ACMEIssuerDNS01ProviderCloudDNS{
											Project: gcpProjectId,
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

			By("Creating new certificate")
			certDomain := fmt.Sprintf("adgca-%s.%s", randomStr(3), appsDomain) // acronym for "ACME dns-01 Google CloudDNS Ambient"
			commonName := certDomain
			if len(certDomain) > 64 {
				commonName = ""
			}
			certName := "cert-with-acme-dns01-clouddns-ambient"
			cert := &certmanagerv1.Certificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      certName,
					Namespace: ns.Name,
				},
				Spec: certmanagerv1.CertificateSpec{
					IsCA:       false,
					CommonName: commonName,
					SecretName: certName,
					IssuerRef: certmanagermetav1.ObjectReference{
						Name: clusterIssuerName,
						Kind: "ClusterIssuer",
					},
					DNSNames: []string{certDomain},
				},
			}

			_, err = certmanagerClient.CertmanagerV1().Certificates(ns.Name).Create(ctx, cert, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			defer certmanagerClient.CertmanagerV1().Certificates(ns.Name).Delete(ctx, certName, metav1.DeleteOptions{})

			By("Waiting for certificate to get ready")
			err = waitForCertificateReadiness(ctx, certName, ns.Name)
			Expect(err).NotTo(HaveOccurred())

			By("checking for certificate validity from secret contents")
			err = verifyCertificate(ctx, certName, ns.Name, commonName)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("dns-01 challenge with IBM Cloud Internet Service Webhook", Label("Platform:IBM"), func() {
		// This test uses IBM Cloud Internet Services (CIS) for the DNS-01 challenge.
		// It works with both UPI / IPI installations by passing in the CRN of your CIS instance on IBM Cloud.
		It("should obtain a valid LetsEncrypt certificate using explicit credentials", func() {
			cisCRN, isCisCRN := os.LookupEnv(cisCRNEnvironmentVar)
			if targetPlatform, ok := os.LookupEnv(targetPlatformEnvironmentVar); ok && targetPlatform == "ibmcloud-upi" {
				if !isCisCRN || cisCRN == "" {
					Fail("cisCRN is required for IBM Cloud platform")
				}
			} else {
				Skip("skipping as the cluster does not use IBM Cloud CIS")
			}

			By("creating new certificate ClusterIssuer with IBM Cloud CIS webhook solver")
			clusterIssuerName := "letsencrypt-dns01-explicit-ic"
			clusterIssuer := &certmanagerv1.ClusterIssuer{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterIssuerName,
				},
				Spec: certmanagerv1.IssuerSpec{
					IssuerConfig: certmanagerv1.IssuerConfig{
						ACME: &acmev1.ACMEIssuer{
							Server: letsEncryptStagingServerURL,
							PrivateKey: certmanagermetav1.SecretKeySelector{
								LocalObjectReference: certmanagermetav1.LocalObjectReference{
									Name: "letsencrypt-dns01-issuer",
								},
							},
							Solvers: []acmev1.ACMEChallengeSolver{
								{
									DNS01: &acmev1.ACMEChallengeSolverDNS01{
										Webhook: &acmev1.ACMEIssuerDNS01ProviderWebhook{
											GroupName:  "acme.borup.work",
											SolverName: "ibmcis",
											Config: &apiextensionsv1.JSON{
												Raw: []byte(fmt.Sprintf(`{
													"apiKeySecretRef": {
														"name": "ibmcis-credentials",
														"key": "api-token"
													},
													"cisCRN": ["%s"]
												}`, cisCRN)),
											},
										},
									},
								},
							},
						},
					},
				},
			}
			_, err := certmanagerClient.CertmanagerV1().ClusterIssuers().Create(ctx, clusterIssuer, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			defer certmanagerClient.CertmanagerV1().ClusterIssuers().Delete(ctx, clusterIssuerName, metav1.DeleteOptions{})

			By("creating new certificate")
			certDomain := fmt.Sprintf("adwicis-%s.%s", randomStr(3), appsDomain) // acronym for "ACME dns-01 Webhook IBM Cloud Internet Service"
			commonName := certDomain
			if len(certDomain) > 64 {
				commonName = ""
			}
			certName := "letsencrypt-cert-ic"
			cert := &certmanagerv1.Certificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      certName,
					Namespace: ns.Name,
				},
				Spec: certmanagerv1.CertificateSpec{
					IsCA:       false,
					CommonName: commonName,
					SecretName: certName,
					IssuerRef: certmanagermetav1.ObjectReference{
						Name: clusterIssuerName,
						Kind: "ClusterIssuer",
					},
					DNSNames: []string{certDomain},
				},
			}

			_, err = certmanagerClient.CertmanagerV1().Certificates(ns.Name).Create(ctx, cert, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			defer certmanagerClient.CertmanagerV1().Certificates(ns.Name).Delete(ctx, certName, metav1.DeleteOptions{})

			By("waiting for certificate to get ready")
			err = waitForCertificateReadiness(ctx, certName, ns.Name)
			Expect(err).NotTo(HaveOccurred())

			By("checking for certificate validity from secret contents")
			err = verifyCertificate(ctx, certName, ns.Name, commonName)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("http-01 challenge using ingress", func() {
		var ingressHost string
		var secretName string

		BeforeEach(func() {
			clusterIssuerName := "letsencrypt-http01"
			ingressClassName := "openshift-default"
			secretName = "ingress-http01-secret"

			By("creating a cluster issuer")
			clusterIssuer := &certmanagerv1.ClusterIssuer{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterIssuerName,
				},
				Spec: certmanagerv1.IssuerSpec{
					IssuerConfig: certmanagerv1.IssuerConfig{
						ACME: &acmev1.ACMEIssuer{
							Server: letsEncryptStagingServerURL,
							PrivateKey: certmanagermetav1.SecretKeySelector{
								LocalObjectReference: certmanagermetav1.LocalObjectReference{
									Name: "letsencrypt-http01-issuer",
								},
							},
							Solvers: []acmev1.ACMEChallengeSolver{
								{
									HTTP01: &acmev1.ACMEChallengeSolverHTTP01{
										Ingress: &acmev1.ACMEChallengeSolverHTTP01Ingress{
											IngressClassName: &ingressClassName,
										},
									},
								},
							},
						},
					},
				},
			}
			_, err := certmanagerClient.CertmanagerV1().ClusterIssuers().Create(ctx, clusterIssuer, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("creating an hello-openshift deployment")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "acme", "deployment.yaml"), ns.Name)

			By("creating a service for the deployment hello-openshift")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "acme", "service.yaml"), ns.Name)

			By("creating an Ingress object")
			ingressHost = fmt.Sprintf("ahi-%s.%s", randomStr(3), appsDomain) // acronym for "ACME http-01 Ingress"
			pathType := networkingv1.PathTypePrefix
			ingress := &networkingv1.Ingress{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "networking.k8s.io/v1",
					Kind:       "Ingress",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ingress-http01",
					Namespace: ns.Name,
					Annotations: map[string]string{
						"cert-manager.io/cluster-issuer": clusterIssuerName,
					},
				},
				Spec: networkingv1.IngressSpec{
					IngressClassName: &ingressClassName,
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
			ingress, err = loader.KubeClient.NetworkingV1().Ingresses(ns.Name).Create(ctx, ingress, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			DeferCleanup(func() {
				certmanagerClient.CertmanagerV1().ClusterIssuers().Delete(ctx, clusterIssuerName, metav1.DeleteOptions{})
			})
		})

		It("should obtain a valid LetsEncrypt certificate", func() {
			By("checking TLS certificate contents")
			err := wait.PollUntilContextTimeout(context.TODO(), slowPollInterval, highTimeout, true, func(context.Context) (bool, error) {
				secret, err := loader.KubeClient.CoreV1().Secrets(ns.Name).Get(ctx, secretName, metav1.GetOptions{})
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

		It("should create HTTP01 solver pods with custom resource limits and requests", func() {
			By("monitoring for ACME HTTP01 solver pods with expected resource configuration")
			// These values match what's configured in BeforeAll
			expectedResources := corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("150m"),
					corev1.ResourceMemory: k8sresource.MustParse("200Mi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("100m"),
					corev1.ResourceMemory: k8sresource.MustParse("100Mi"),
				},
			}

			err := wait.PollUntilContextTimeout(ctx, fastPollInterval, lowTimeout, true, func(ctx context.Context) (bool, error) {
				pods, err := k8sClientSet.CoreV1().Pods("").List(ctx, metav1.ListOptions{
					LabelSelector: acmeSolverPodLabel,
				})
				if err != nil {
					return false, nil // Retry on transient errors
				}

				if len(pods.Items) == 0 {
					return false, nil // Keep waiting for pods to appear
				}

				// Check if any pod matches the expected resource configuration
				for _, pod := range pods.Items {
					if err := VerifyContainerResources(pod, acmeSolverContainerName, expectedResources); err == nil {
						return true, nil
					}
				}

				return false, nil // No matching pods yet, keep waiting
			})
			Expect(err).NotTo(HaveOccurred(), "should find ACME HTTP01 solver pods with expected resource configuration")

			By("waiting for certificate to get ready")
			err = waitForCertificateReadiness(ctx, secretName, ns.Name)
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
