//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

var _ = Describe("ACME Issuer HTTP01 solver", Ordered, func() {
	var ctx context.Context
	var cancel context.CancelFunc
	var ns *corev1.Namespace
	var appsDomain string
	var baseDomain string

	BeforeAll(func() {
		ctx = context.Background()
		By("getting cluster base domain")
		var err error
		baseDomain, err = library.GetClusterBaseDomain(ctx, configClient)
		Expect(err).NotTo(HaveOccurred(), "failed to get cluster base domain")
		Expect(baseDomain).NotTo(BeEmpty(), "base domain should not be empty")
		appsDomain = "apps." + baseDomain

		By("adding required args to cert-manager controller")
		err = addOverrideArgs(certmanageroperatorclient, certmanagerControllerDeployment, []string{
			// for http-01 solver ingress
			"--acme-http01-solver-resource-limits-cpu=150m",
			"--acme-http01-solver-resource-limits-memory=200Mi",
			"--acme-http01-solver-resource-request-cpu=100m",
			"--acme-http01-solver-resource-request-memory=100Mi",
		})
		Expect(err).NotTo(HaveOccurred())

		proxy, err := configClient.Proxies().Get(ctx, "cluster", metav1.GetOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			Expect(err).NotTo(HaveOccurred(), "failed to get cluster proxy config")
		}
		if err == nil && proxy.Spec.TrustedCA.Name != "" {
			By("creating trusted CA ConfigMap for HTTPS proxy")
			trustedCA := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "trusted-ca",
					Namespace: "cert-manager",
					Labels: map[string]string{
						"config.openshift.io/inject-trusted-cabundle": "true",
					},
				},
			}
			_, err = loader.KubeClient.CoreV1().ConfigMaps("cert-manager").Create(ctx, trustedCA, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			DeferCleanup(func(cleanupCtx context.Context) {
				loader.KubeClient.CoreV1().ConfigMaps("cert-manager").Delete(cleanupCtx, "trusted-ca", metav1.DeleteOptions{})
			})

			By("setting trusted CA ConfigMap name via subscription env var")
			err = patchSubscriptionWithEnvVars(ctx, loader, map[string]string{
				"TRUSTED_CA_CONFIGMAP_NAME": "trusted-ca",
			})
			Expect(err).NotTo(HaveOccurred(), "failed to patch subscription with 'TRUSTED_CA_CONFIGMAP_NAME' environment variable")

			DeferCleanup(func(cleanupCtx context.Context) {
				By("removing 'TRUSTED_CA_CONFIGMAP_NAME' from subscription")
				patchSubscriptionWithEnvVars(cleanupCtx, loader, map[string]string{
					"TRUSTED_CA_CONFIGMAP_NAME": "",
				})
			})

			By("waiting for operator deployment to restart with trusted CA configuration")
			err = waitForDeploymentEnvVarAndRollout(ctx, operatorNamespace, operatorDeploymentName, "TRUSTED_CA_CONFIGMAP_NAME", "trusted-ca", lowTimeout)
			Expect(err).NotTo(HaveOccurred())
		}

		DeferCleanup(func(ctx context.Context) {
			By("Resetting cert-manager state")
			err := resetCertManagerState(ctx, certmanageroperatorclient, loader)
			if err != nil {
				fmt.Fprintf(GinkgoWriter, "failed to reset cert-manager state during cleanup: %v\n", err)
			}
		})
	})

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 15*time.Minute)
		DeferCleanup(cancel)

		By("waiting for operator status to become available")
		err := VerifyHealthyOperatorConditions(certmanageroperatorclient.OperatorV1alpha1())
		Expect(err).NotTo(HaveOccurred(), "Operator is expected to be available")

		By("creating a test namespace")
		namespace, err := loader.CreateTestingNS("e2e-acme-http01", false)
		Expect(err).NotTo(HaveOccurred(), "failed to create test namespace")
		ns = namespace

		DeferCleanup(func() {
			loader.DeleteTestingNS(ns.Name, func() bool { return CurrentSpecReport().Failed() })
		})
	})

	Context("with Ingress annotation", func() {
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
			Expect(err).NotTo(HaveOccurred(), "failed to create ClusterIssuer")

			DeferCleanup(func(ctx context.Context) {
				certmanagerClient.CertmanagerV1().ClusterIssuers().Delete(ctx, clusterIssuerName, metav1.DeleteOptions{})
			})

			By("creating hello-openshift deployment")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "acme", "deployment.yaml"), ns.Name)

			By("creating service for hello-openshift deployment")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "acme", "service.yaml"), ns.Name)

			By("creating Ingress object")
			ingressHost = fmt.Sprintf("ahi-%s.%s", randomStr(3), appsDomain) // acronym for "ACME http-01 Ingress"
			pathType := networkingv1.PathTypePrefix
			ingress := &networkingv1.Ingress{
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
											Backend: networkingv1.IngressBackend{
												Service: &networkingv1.IngressServiceBackend{
													Name: "hello-openshift",
													Port: networkingv1.ServiceBackendPort{
														Number: 8080,
													},
												},
											},
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
			_, err = loader.KubeClient.NetworkingV1().Ingresses(ns.Name).Create(ctx, ingress, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create Ingress")
		})

		It("should obtain a valid certificate", func() {

			By("checking TLS certificate contents")
			err := wait.PollUntilContextTimeout(ctx, slowPollInterval, highTimeout, true, func(context.Context) (bool, error) {
				secret, err := loader.KubeClient.CoreV1().Secrets(ns.Name).Get(ctx, secretName, metav1.GetOptions{})
				if err != nil {
					return false, nil // keep polling until the Secret exists
				}

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
			Expect(err).NotTo(HaveOccurred(), "timeout waiting for valid TLS certificate")
		})

		It("should create solver pods with custom resource limits and requests", func() {

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
			Expect(err).NotTo(HaveOccurred(), "timeout waiting for certificate to become ready")
		})
	})

	Context("with Certificate object", Label("TechPreview"), func() {

		It("should obtain a valid certificate", func() {
			var err error
			issuerName := "letsencrypt-http01-proxy"
			secretName := "cert-from-" + issuerName

			By("creating HTTP01 issuer")
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
									Name: issuerName + "-acme",
								},
							},
							Solvers: []acmev1.ACMEChallengeSolver{
								{
									HTTP01: &acmev1.ACMEChallengeSolverHTTP01{
										Ingress: &acmev1.ACMEChallengeSolverHTTP01Ingress{},
									},
								},
							},
						},
					},
				},
			}
			_, err = certmanagerClient.CertmanagerV1().Issuers(ns.Name).Create(ctx, issuer, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create Issuer")

			By("creating certificate")
			dnsName := "test-https-proxy-" + randomStr(3) + "." + appsDomain
			cert := &certmanagerv1.Certificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: ns.Name,
				},
				Spec: certmanagerv1.CertificateSpec{
					DNSNames:   []string{dnsName},
					SecretName: secretName,
					IssuerRef: certmanagermetav1.ObjectReference{
						Name: issuerName,
						Kind: "Issuer",
					},
				},
			}
			_, err = certmanagerClient.CertmanagerV1().Certificates(ns.Name).Create(ctx, cert, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create Certificate")

			By("waiting for certificate to become ready")
			err = waitForCertificateReadiness(ctx, secretName, ns.Name)
			Expect(err).NotTo(HaveOccurred(), "timeout waiting for certificate to become ready")

			By("verifying certificate")
			err = verifyCertificate(ctx, secretName, ns.Name, dnsName)
			Expect(err).NotTo(HaveOccurred(), "certificate verification failed")
		})

		It("should select appropriate solver based on selector configuration", func() {
			const (
				testDomain          = "test-example.com"
				http01SelectorLabel = "use-http01-solver"
				azureDNSSecretName  = "dummy-azuredns-config"
				route53SecretName   = "dummy-route53-config"
				clusterIssuerName   = "acme-multiple-solvers"
			)

			By("creating dummy secret for Azure DNS-01 solver")
			azureDNSSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      azureDNSSecretName,
					Namespace: "cert-manager",
				},
				StringData: map[string]string{
					"client-secret": "dummy-client-secret",
				},
			}
			_, err := loader.KubeClient.CoreV1().Secrets("cert-manager").Create(ctx, azureDNSSecret, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create Azure DNS secret")

			DeferCleanup(func(ctx context.Context) {
				err := loader.KubeClient.CoreV1().Secrets("cert-manager").Delete(ctx, azureDNSSecretName, metav1.DeleteOptions{})
				if err != nil {
					fmt.Fprintf(GinkgoWriter, "failed to delete Azure DNS secret during cleanup: %v\n", err)
				}
			})

			By("creating dummy secret for Route53 DNS-01 solver")
			route53Secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      route53SecretName,
					Namespace: "cert-manager",
				},
				StringData: map[string]string{
					"secret-access-key": "dummy-secret-key",
				},
			}
			_, err = loader.KubeClient.CoreV1().Secrets("cert-manager").Create(ctx, route53Secret, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create Route53 secret")

			DeferCleanup(func(ctx context.Context) {
				err := loader.KubeClient.CoreV1().Secrets("cert-manager").Delete(ctx, route53SecretName, metav1.DeleteOptions{})
				if err != nil {
					fmt.Fprintf(GinkgoWriter, "failed to delete Route53 secret during cleanup: %v\n", err)
				}
			})

			By("creating ClusterIssuer with multiple solvers: HTTP-01, DNS-01 (Azure), DNS-01 (Route53)")
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
									Name: "acme-account-key",
								},
							},
							Solvers: []acmev1.ACMEChallengeSolver{
								// Solver 1: HTTP-01 with label selector
								{
									Selector: &acmev1.CertificateDNSNameSelector{
										MatchLabels: map[string]string{
											http01SelectorLabel: "true",
										},
										DNSZones: []string{testDomain},
									},
									HTTP01: &acmev1.ACMEChallengeSolverHTTP01{
										Ingress: &acmev1.ACMEChallengeSolverHTTP01Ingress{},
									},
								},
								// Solver 2: DNS-01 (Azure) with specific dnsNames selector
								{
									Selector: &acmev1.CertificateDNSNameSelector{
										DNSNames: []string{"test-2." + testDomain},
									},
									DNS01: &acmev1.ACMEChallengeSolverDNS01{
										AzureDNS: &acmev1.ACMEIssuerDNS01ProviderAzureDNS{
											ClientID: "aaaa-aaaa-aaaa-aaaa",
											ClientSecret: &certmanagermetav1.SecretKeySelector{
												LocalObjectReference: certmanagermetav1.LocalObjectReference{
													Name: azureDNSSecretName,
												},
												Key: "client-secret",
											},
											SubscriptionID:    "bbbb-bbbb-bbbb-bbbb",
											TenantID:          "cccc-cccc-cccc-cccc",
											ResourceGroupName: "dummy-rg",
										},
									},
								},
								// Solver 3: DNS-01 (Route53) with dnsZones selector
								{
									Selector: &acmev1.CertificateDNSNameSelector{
										DNSZones: []string{testDomain},
									},
									DNS01: &acmev1.ACMEChallengeSolverDNS01{
										Route53: &acmev1.ACMEIssuerDNS01ProviderRoute53{
											Region:       "us-east-1",
											AccessKeyID:  "DUMMYKEYID",
											HostedZoneID: "DUMMYZONEID",
											SecretAccessKey: certmanagermetav1.SecretKeySelector{
												LocalObjectReference: certmanagermetav1.LocalObjectReference{
													Name: route53SecretName,
												},
												Key: "secret-access-key",
											},
										},
									},
								},
							},
						},
					},
				},
			}
			_, err = certmanagerClient.CertmanagerV1().ClusterIssuers().Create(ctx, clusterIssuer, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create ClusterIssuer")

			DeferCleanup(func(ctx context.Context) {
				err := certmanagerClient.CertmanagerV1().ClusterIssuers().Delete(ctx, clusterIssuerName, metav1.DeleteOptions{})
				if err != nil {
					fmt.Fprintf(GinkgoWriter, "failed to delete ClusterIssuer during cleanup: %v\n", err)
				}
			})

			By("waiting for ClusterIssuer to become ready")
			err = waitForClusterIssuerReadiness(ctx, clusterIssuerName)
			Expect(err).NotTo(HaveOccurred(), "timeout waiting for ClusterIssuer to become ready")

			By("creating certificate with label to match HTTP-01 solver")
			http01CertName := "cert-http01-label-selector"
			http01Cert := &certmanagerv1.Certificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      http01CertName,
					Namespace: ns.Name,
					Labels: map[string]string{
						http01SelectorLabel: "true",
					},
				},
				Spec: certmanagerv1.CertificateSpec{
					SecretName: http01CertName,
					IssuerRef: certmanagermetav1.ObjectReference{
						Kind: "ClusterIssuer",
						Name: clusterIssuerName,
					},
					DNSNames: []string{"test-1." + testDomain},
				},
			}
			_, err = certmanagerClient.CertmanagerV1().Certificates(ns.Name).Create(ctx, http01Cert, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create certificate with HTTP-01 label selector")

			DeferCleanup(func(ctx context.Context) {
				err := certmanagerClient.CertmanagerV1().Certificates(ns.Name).Delete(ctx, http01CertName, metav1.DeleteOptions{})
				if err != nil {
					fmt.Fprintf(GinkgoWriter, "failed to delete certificate during cleanup: %v\n", err)
				}
			})

			verifyChallengeSelector := func(description string, matchFunc func(*acmev1.Challenge) bool) {
				err := wait.PollUntilContextTimeout(ctx, fastPollInterval, lowTimeout, true, func(ctx context.Context) (bool, error) {
					challenges, err := certmanagerClient.AcmeV1().Challenges(ns.Name).List(ctx, metav1.ListOptions{})
					if err != nil || len(challenges.Items) == 0 {
						return false, nil
					}
					for _, challenge := range challenges.Items {
						if matchFunc(&challenge) {
							return true, nil
						}
					}
					return false, nil
				})
				Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("timeout waiting for challenge with %s", description))
			}

			By("verifying challenge uses HTTP-01 solver with label selector")
			verifyChallengeSelector("HTTP-01 solver and label selector", func(challenge *acmev1.Challenge) bool {
				return challenge.Spec.Solver.Selector != nil &&
					challenge.Spec.Solver.Selector.MatchLabels != nil &&
					challenge.Spec.Solver.Selector.MatchLabels[http01SelectorLabel] == "true"
			})

			By("creating certificate with specific dnsName to match Azure DNS-01 solver")
			azureCertName := "cert-azure-dnsname-selector"
			azureDNSName := "test-2." + testDomain
			azureCert := &certmanagerv1.Certificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      azureCertName,
					Namespace: ns.Name,
				},
				Spec: certmanagerv1.CertificateSpec{
					SecretName: azureCertName,
					IssuerRef: certmanagermetav1.ObjectReference{
						Kind: "ClusterIssuer",
						Name: clusterIssuerName,
					},
					DNSNames: []string{azureDNSName},
				},
			}
			_, err = certmanagerClient.CertmanagerV1().Certificates(ns.Name).Create(ctx, azureCert, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create certificate with Azure dnsName selector")

			DeferCleanup(func(ctx context.Context) {
				err := certmanagerClient.CertmanagerV1().Certificates(ns.Name).Delete(ctx, azureCertName, metav1.DeleteOptions{})
				if err != nil {
					fmt.Fprintf(GinkgoWriter, "failed to delete certificate during cleanup: %v\n", err)
				}
			})

			By("verifying challenge uses Azure DNS-01 solver with dnsName selector")
			verifyChallengeSelector("Azure DNS-01 solver and dnsName selector", func(challenge *acmev1.Challenge) bool {
				if challenge.Spec.Solver.Selector == nil || len(challenge.Spec.Solver.Selector.DNSNames) == 0 {
					return false
				}
				return slices.Contains(challenge.Spec.Solver.Selector.DNSNames, azureDNSName)
			})

			By("creating certificate with wildcard domain to match Route53 DNS-01 solver by dnsZone")
			route53CertName := "cert-route53-dnszone-selector"
			route53Cert := &certmanagerv1.Certificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      route53CertName,
					Namespace: ns.Name,
				},
				Spec: certmanagerv1.CertificateSpec{
					SecretName: route53CertName,
					IssuerRef: certmanagermetav1.ObjectReference{
						Kind: "ClusterIssuer",
						Name: clusterIssuerName,
					},
					DNSNames: []string{
						"test-3." + testDomain,
						"*.test-3." + testDomain,
					},
				},
			}
			_, err = certmanagerClient.CertmanagerV1().Certificates(ns.Name).Create(ctx, route53Cert, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create certificate with Route53 dnsZone selector")

			DeferCleanup(func(ctx context.Context) {
				err := certmanagerClient.CertmanagerV1().Certificates(ns.Name).Delete(ctx, route53CertName, metav1.DeleteOptions{})
				if err != nil {
					fmt.Fprintf(GinkgoWriter, "failed to delete certificate during cleanup: %v\n", err)
				}
			})

			By("verifying challenge uses Route53 DNS-01 solver with dnsZone selector")
			verifyChallengeSelector("Route53 DNS-01 solver and dnsZone selector", func(challenge *acmev1.Challenge) bool {
				if challenge.Spec.Solver.DNS01 == nil || challenge.Spec.Solver.DNS01.Route53 == nil {
					return false
				}
				if challenge.Spec.Solver.Selector == nil || len(challenge.Spec.Solver.Selector.DNSZones) == 0 {
					return false
				}
				return slices.Contains(challenge.Spec.Solver.Selector.DNSZones, testDomain)
			})
		})
	})
})
