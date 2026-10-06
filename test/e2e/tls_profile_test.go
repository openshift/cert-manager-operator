//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"time"

	configapiv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/cert-manager-operator/pkg/tlsprofile"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var csvSchema = schema.GroupVersionResource{
	Group:    "operators.coreos.com",
	Version:  "v1alpha1",
	Resource: "clusterserviceversions",
}

var _ = Describe("Cluster TLS security profile", Label("Platform:Generic", "Feature:TLSProfile", "TechPreview"), Ordered, func() {
	var ctx context.Context

	BeforeAll(func() {
		ctx = context.Background()
	})

	BeforeEach(func() {
		By("waiting for operator status to become available")
		err := VerifyHealthyOperatorConditions(certmanageroperatorclient.OperatorV1alpha1())
		Expect(err).NotTo(HaveOccurred(), "Operator is expected to be available")
	})

	// Journey 6 (partial): cert-manager operands always present — no TrustManager required.
	It("should configure cert-manager operand container TLS args from apiserver cluster profile", func() {
		original, err := getClusterAPIServerTLSConfig(ctx)
		if apierrors.IsNotFound(err) {
			Skip("apiserver.config.openshift.io/cluster is not available on this cluster")
		}
		Expect(err).NotTo(HaveOccurred(), "failed to read apiserver TLS configuration")

		testProfile := &configapiv1.TLSSecurityProfile{
			Type: configapiv1.TLSProfileModernType,
		}
		strictAdherence := configapiv1.TLSAdherencePolicyStrictAllComponents

		DeferCleanup(func() {
			By("[cleanup] restoring original apiserver TLS configuration")
			Eventually(func() error {
				return restoreClusterAPIServerTLSConfig(ctx, original)
			}, lowTimeout, fastPollInterval).Should(Succeed())
		})

		By("patching apiserver cluster to enforce StrictAllComponents with Modern TLS profile")
		err = updateClusterAPIServerTLSConfig(ctx, testProfile, strictAdherence)
		if isTLSAdherenceUnsupported(err) {
			Skip(fmt.Sprintf("apiserver tlsAdherence is not available on this cluster: %v", err))
		}
		Expect(err).NotTo(HaveOccurred(), "failed to patch apiserver TLS configuration")

		expectedSpec, err := tlsprofile.EffectiveSpec(testProfile)
		Expect(err).NotTo(HaveOccurred(), "failed to resolve expected TLS profile spec")

		By("verifying cert-manager operand deployments expose cluster TLS flags")
		for _, name := range []string{
			certmanagerControllerDeployment,
			certmanagerWebhookDeployment,
			certmanagerCAinjectorDeployment,
		} {
			err := verifyOperandTLSArgsMatchClusterProfile(name, expectedSpec)
			Expect(err).NotTo(HaveOccurred(), "deployment %s", name)
		}
	})

	It("should enable HTTPS metrics dynamic serving on cert-manager operands", func() {
		By("verifying metrics-dynamic-serving args and prometheus.io/scheme=https annotation")
		for _, name := range []string{
			certmanagerControllerDeployment,
			certmanagerWebhookDeployment,
			certmanagerCAinjectorDeployment,
		} {
			err := verifyOperandMetricsHTTPS(name)
			Expect(err).NotTo(HaveOccurred(), "deployment %s", name)
		}
	})

	It("should reject TLS 1.2 and accept TLS 1.3 on metrics and webhook listeners under Modern", func() {
		original := requireAPIServerTLSConfig(ctx)
		DeferCleanup(restoreAPIServerTLSConfigCleanup(ctx, original))

		modernSpec := patchStrictTLSProfile(ctx, &configapiv1.TLSSecurityProfile{
			Type: configapiv1.TLSProfileModernType,
		})
		expectCertManagerListenersMatchProfile(ctx, modernSpec)

		By("handshaking metrics and webhook sockets: TLS 1.3 succeeds, TLS 1.2 fails")
		assertModernListenerEnforcement(ctx)
	})

	It("should enforce Intermediate TLS 1.2 ciphers on metrics and webhook listeners", func() {
		original := requireAPIServerTLSConfig(ctx)
		DeferCleanup(restoreAPIServerTLSConfigCleanup(ctx, original))

		intermediateSpec := patchStrictTLSProfile(ctx, &configapiv1.TLSSecurityProfile{
			Type: configapiv1.TLSProfileIntermediateType,
		})
		expectCertManagerListenersMatchProfile(ctx, intermediateSpec)

		By("handshaking metrics and webhook sockets: TLS 1.2 + allowed ECDSA GCM succeeds; TLS 1.0 and CBC fail")
		assertIntermediateListenerEnforcement(ctx)
	})

	It("should apply Custom TLS 1.3 to cert-manager operand listeners", func() {
		original := requireAPIServerTLSConfig(ctx)
		DeferCleanup(restoreAPIServerTLSConfigCleanup(ctx, original))

		customSpec := patchStrictTLSProfile(ctx, customTLS13SecurityProfile())
		expectCertManagerListenersMatchProfile(ctx, customSpec)

		By("handshaking metrics and webhook sockets: Custom TLS 1.3 succeeds, TLS 1.2 fails")
		assertModernListenerEnforcement(ctx)
	})

	It("should claim tls-profiles feature on the operator CSV when installed via OLM", func() {
		installed, err := certManagerOperatorSubscriptionInstalled(ctx, loader)
		Expect(err).NotTo(HaveOccurred())
		if !installed {
			Skip("no OLM Subscription; CSV annotation check not applicable")
		}

		By("listing ClusterServiceVersions in the operator namespace")
		csvClient := loader.DynamicClient.Resource(csvSchema).Namespace(operatorNamespace)
		csvs, err := csvClient.List(ctx, metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(csvs.Items).NotTo(BeEmpty(), "expected at least one CSV in %s", operatorNamespace)

		found := false
		for _, csv := range csvs.Items {
			name := csv.GetName()
			if !strings.Contains(name, "cert-manager-operator") {
				continue
			}
			annotations := csv.GetAnnotations()
			Expect(annotations).To(HaveKeyWithValue("features.operators.openshift.io/tls-profiles", "true"),
				"CSV %s missing tls-profiles feature annotation", name)
			found = true
			break
		}
		Expect(found).To(BeTrue(), "no cert-manager-operator CSV found in %s", operatorNamespace)
	})

	// E2E-012 — Strict Intermediate → Legacy: every profile-managed TLS flag is gone;
	// leftover Intermediate cipher flags must not remain; live sockets accept TLS 1.2 again
	// after a prior Strict Modern restriction.
	It("should strip every profile-managed TLS flag and restore TLS 1.2 on Strict to Legacy", func() {
		original := requireAPIServerTLSConfig(ctx)
		DeferCleanup(restoreAPIServerTLSConfigCleanup(ctx, original))

		operandDeployments := []string{
			certmanagerControllerDeployment,
			certmanagerWebhookDeployment,
			certmanagerCAinjectorDeployment,
		}

		By("applying Strict Modern so listeners reject TLS 1.2")
		modernSpec := patchStrictTLSProfile(ctx, &configapiv1.TLSSecurityProfile{
			Type: configapiv1.TLSProfileModernType,
		})
		expectCertManagerListenersMatchProfile(ctx, modernSpec)
		assertModernListenerEnforcement(ctx)

		By("applying Strict Intermediate so leftover cipher flags would exist if Legacy only dropped Modern strings")
		intermediateSpec := patchStrictTLSProfile(ctx, &configapiv1.TLSSecurityProfile{
			Type: configapiv1.TLSProfileIntermediateType,
		})
		expectCertManagerListenersMatchProfile(ctx, intermediateSpec)

		By("patching apiserver to LegacyAdheringComponentsOnly with Intermediate profile still set")
		patchLegacyTLSProfile(ctx, &configapiv1.TLSSecurityProfile{
			Type: configapiv1.TLSProfileIntermediateType,
		})

		By("waiting for every profile-managed TLS flag key to be absent")
		expectProfileManagedTLSFlagsGone(ctx, operandDeployments)

		By("handshaking sockets: TLS 1.2 succeeds after Strict → Legacy (no longer TLS 1.3-only)")
		assertTLS12Accepted(ctx, certManagerTLSEndpoints())
	})

	// E2E-010 — Day-2 Modern → Intermediate on cert-manager operands.
	It("should update cert-manager operand TLS args when apiserver profile changes Modern to Intermediate", func() {
		original := requireAPIServerTLSConfig(ctx)
		DeferCleanup(restoreAPIServerTLSConfigCleanup(ctx, original))

		modernSpec := patchStrictTLSProfile(ctx, &configapiv1.TLSSecurityProfile{
			Type: configapiv1.TLSProfileModernType,
		})

		operandDeployments := []string{
			certmanagerControllerDeployment,
			certmanagerWebhookDeployment,
			certmanagerCAinjectorDeployment,
		}

		By("verifying cert-manager operands match Modern EffectiveSpec")
		for _, name := range operandDeployments {
			err := verifyOperandTLSArgsMatchClusterProfile(name, modernSpec)
			Expect(err).NotTo(HaveOccurred(), "modern profile args on %s", name)
		}

		By("patching apiserver cluster profile to Intermediate")
		intermediateSpec := patchStrictTLSProfile(ctx, &configapiv1.TLSSecurityProfile{
			Type: configapiv1.TLSProfileIntermediateType,
		})

		By("verifying cert-manager operands converge to Intermediate EffectiveSpec")
		for _, name := range operandDeployments {
			err := verifyOperandTLSArgsMatchClusterProfile(name, intermediateSpec)
			Expect(err).NotTo(HaveOccurred(), "intermediate profile args on %s", name)
		}
	})

	Context("trust-manager webhook TLS", Ordered, func() {
		var (
			tmCtx                            = context.Background()
			clientset                        *kubernetes.Clientset
			originalUnsupportedAddonFeatures string
			originalOperatorLogLevel         string
		)

		BeforeAll(trustManagerBeforeAll(tmCtx, &clientset, &originalUnsupportedAddonFeatures, &originalOperatorLogLevel))
		AfterAll(trustManagerAfterAll(tmCtx, &originalUnsupportedAddonFeatures, &originalOperatorLogLevel))
		AfterEach(trustManagerAfterEach(tmCtx))

		// Journey 1 — E2E-001
		It("should configure trust-manager webhook TLS args from StrictAllComponents Modern profile", func() {
			original := requireAPIServerTLSConfig(tmCtx)
			DeferCleanup(restoreAPIServerTLSConfigCleanup(tmCtx, original))

			createTrustManager(tmCtx, newTrustManagerCR())
			expectTrustManagerDeploymentPresent(tmCtx)

			expectedSpec := patchStrictTLSProfile(tmCtx, &configapiv1.TLSSecurityProfile{
				Type: configapiv1.TLSProfileModernType,
			})

			By("verifying trust-manager webhook TLS flags match Modern EffectiveSpec")
			err := verifyOperandTLSArgsMatchClusterProfile(trustManagerDeploymentName, expectedSpec)
			Expect(err).NotTo(HaveOccurred(), "deployment %s", trustManagerDeploymentName)

			By("verifying trust-manager deployment is Available after TLS reconcile")
			err = waitForDeploymentRollout(tmCtx, operandNamespace, trustManagerDeploymentName, lowTimeout)
			Expect(err).NotTo(HaveOccurred())
		})

		// Journey 2 — E2E-002
		It("should update trust-manager TLS args when apiserver profile changes Modern to Intermediate", func() {
			original := requireAPIServerTLSConfig(tmCtx)
			DeferCleanup(restoreAPIServerTLSConfigCleanup(tmCtx, original))

			createTrustManager(tmCtx, newTrustManagerCR())
			expectTrustManagerDeploymentPresent(tmCtx)

			modernSpec := patchStrictTLSProfile(tmCtx, &configapiv1.TLSSecurityProfile{
				Type: configapiv1.TLSProfileModernType,
			})
			err := verifyOperandTLSArgsMatchClusterProfile(trustManagerDeploymentName, modernSpec)
			Expect(err).NotTo(HaveOccurred(), "modern profile args")

			By("patching apiserver cluster profile to Intermediate")
			intermediateSpec := patchStrictTLSProfile(tmCtx, &configapiv1.TLSSecurityProfile{
				Type: configapiv1.TLSProfileIntermediateType,
			})

			By("verifying trust-manager args converge to Intermediate EffectiveSpec")
			err = verifyOperandTLSArgsMatchClusterProfile(trustManagerDeploymentName, intermediateSpec)
			Expect(err).NotTo(HaveOccurred(), "intermediate profile args")
		})

		// E2E-011 — Intermediate → Modern strips cipher flags on cert-manager operands + trust-manager.
		It("should strip TLS cipher flags when apiserver profile changes Intermediate to Modern", func() {
			original := requireAPIServerTLSConfig(tmCtx)
			DeferCleanup(restoreAPIServerTLSConfigCleanup(tmCtx, original))

			createTrustManager(tmCtx, newTrustManagerCR())
			expectTrustManagerDeploymentPresent(tmCtx)

			deployments := []string{
				certmanagerControllerDeployment,
				certmanagerWebhookDeployment,
				certmanagerCAinjectorDeployment,
				trustManagerDeploymentName,
			}

			By("applying Strict Intermediate and confirming cipher-bearing TLS args")
			intermediateSpec := patchStrictTLSProfile(tmCtx, &configapiv1.TLSSecurityProfile{
				Type: configapiv1.TLSProfileIntermediateType,
			})
			for _, name := range deployments {
				err := verifyOperandTLSArgsMatchClusterProfile(name, intermediateSpec)
				Expect(err).NotTo(HaveOccurred(), "intermediate profile args on %s", name)
			}

			By("patching apiserver cluster profile to Modern (TLS1.3)")
			modernSpec := patchStrictTLSProfile(tmCtx, &configapiv1.TLSSecurityProfile{
				Type: configapiv1.TLSProfileModernType,
			})

			By("verifying deployments converge to Modern min-version args with cipher flags stripped")
			for _, name := range deployments {
				err := verifyOperandTLSArgsMatchClusterProfile(name, modernSpec)
				Expect(err).NotTo(HaveOccurred(), "modern profile args / cipher strip on %s", name)
			}
		})

		// Journey 3 — E2E-003
		It("should apply Custom TLS profile flags to trust-manager", func() {
			original := requireAPIServerTLSConfig(tmCtx)
			DeferCleanup(restoreAPIServerTLSConfigCleanup(tmCtx, original))

			createTrustManager(tmCtx, newTrustManagerCR())
			expectTrustManagerDeploymentPresent(tmCtx)

			customProfile := &configapiv1.TLSSecurityProfile{
				Type: configapiv1.TLSProfileCustomType,
				Custom: &configapiv1.CustomTLSProfile{
					TLSProfileSpec: configapiv1.TLSProfileSpec{
						MinTLSVersion: configapiv1.VersionTLS12,
						Ciphers: []string{
							"ECDHE-ECDSA-AES128-GCM-SHA256",
							"ECDHE-RSA-AES128-GCM-SHA256",
						},
					},
				},
			}
			expectedSpec := patchStrictTLSProfile(tmCtx, customProfile)

			By("verifying trust-manager args match Custom EffectiveSpec")
			err := verifyOperandTLSArgsMatchClusterProfile(trustManagerDeploymentName, expectedSpec)
			Expect(err).NotTo(HaveOccurred(), "custom profile args")
		})

		It("should apply Custom TLS 1.3 flags to trust-manager and reject TLS 1.2 on the webhook", func() {
			original := requireAPIServerTLSConfig(tmCtx)
			DeferCleanup(restoreAPIServerTLSConfigCleanup(tmCtx, original))

			createTrustManager(tmCtx, newTrustManagerCR())
			expectTrustManagerDeploymentPresent(tmCtx)

			customSpec := patchStrictTLSProfile(tmCtx, customTLS13SecurityProfile())
			By("verifying trust-manager args match Custom TLS 1.3 EffectiveSpec without cipher flags")
			err := verifyOperandTLSArgsMatchClusterProfile(trustManagerDeploymentName, customSpec)
			Expect(err).NotTo(HaveOccurred(), "custom tls13 profile args")
			err = waitForDeploymentRollout(tmCtx, operandNamespace, trustManagerDeploymentName, lowTimeout)
			Expect(err).NotTo(HaveOccurred())
			expectTrustManagerTLSSecret(tmCtx)

			By("handshaking trust-manager webhook: TLS 1.3 succeeds, TLS 1.2 fails")
			assertModernListenerEnforcementOn(tmCtx, []certManagerTLSEndpoint{trustManagerWebhookEndpoint()})
		})

		// Journey 4 — E2E-004
		It("should keep trust-manager Certificate Ready after Modern TLS profile is applied", func() {
			original := requireAPIServerTLSConfig(tmCtx)
			DeferCleanup(restoreAPIServerTLSConfigCleanup(tmCtx, original))

			createTrustManager(tmCtx, newTrustManagerCR())
			expectTrustManagerDeploymentPresent(tmCtx)

			By("waiting for trust-manager Certificate to become ready before TLS patch")
			err := waitForCertificateReadiness(tmCtx, trustManagerCertificateName, trustManagerNamespace)
			Expect(err).NotTo(HaveOccurred())

			expectedSpec := patchStrictTLSProfile(tmCtx, &configapiv1.TLSSecurityProfile{
				Type: configapiv1.TLSProfileModernType,
			})
			err = verifyOperandTLSArgsMatchClusterProfile(trustManagerDeploymentName, expectedSpec)
			Expect(err).NotTo(HaveOccurred())

			By("re-checking Certificate readiness and TLS secret after profile application")
			err = waitForCertificateReadiness(tmCtx, trustManagerCertificateName, trustManagerNamespace)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				secret, err := k8sClientSet.CoreV1().Secrets(trustManagerNamespace).Get(tmCtx, trustManagerTLSSecretName, metav1.GetOptions{})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(secret.Data).To(HaveKey("tls.crt"))
				g.Expect(secret.Data).To(HaveKey("tls.key"))
			}, lowTimeout, fastPollInterval).Should(Succeed())
		})

		// Journey 5 — NEG-001
		It("should not apply Modern TLS flags to trust-manager under LegacyAdheringComponentsOnly", func() {
			original := requireAPIServerTLSConfig(tmCtx)
			DeferCleanup(restoreAPIServerTLSConfigCleanup(tmCtx, original))

			modernProfile := &configapiv1.TLSSecurityProfile{
				Type: configapiv1.TLSProfileModernType,
			}
			By("patching apiserver to LegacyAdheringComponentsOnly with Modern profile before TrustManager exists")
			err := updateClusterAPIServerTLSConfig(tmCtx, modernProfile, configapiv1.TLSAdherencePolicyLegacyAdheringComponentsOnly)
			if isTLSAdherenceUnsupported(err) {
				Skip(fmt.Sprintf("apiserver tlsAdherence is not available on this cluster: %v", err))
			}
			Expect(err).NotTo(HaveOccurred())

			createTrustManager(tmCtx, newTrustManagerCR())
			expectTrustManagerDeploymentPresent(tmCtx)

			modernSpec, err := tlsprofile.EffectiveSpec(modernProfile)
			Expect(err).NotTo(HaveOccurred())
			unexpected := tlsprofile.TrustManagerWebhookTLSArgs(modernSpec)
			Expect(unexpected).NotTo(BeEmpty())

			By("consistently verifying Modern Strict TLS args are absent on trust-manager")
			Consistently(func() error {
				return verifyOperandTLSArgsNotPresent(trustManagerDeploymentName, unexpected)
			}, 15*time.Second, fastPollInterval).Should(Succeed())
		})

		It("should strip every trust-manager profile TLS flag on Strict Intermediate to Legacy", func() {
			original := requireAPIServerTLSConfig(tmCtx)
			DeferCleanup(restoreAPIServerTLSConfigCleanup(tmCtx, original))

			createTrustManager(tmCtx, newTrustManagerCR())
			expectTrustManagerDeploymentPresent(tmCtx)

			By("applying Strict Intermediate so leftover cipher flags would exist")
			intermediateSpec := patchStrictTLSProfile(tmCtx, &configapiv1.TLSSecurityProfile{
				Type: configapiv1.TLSProfileIntermediateType,
			})
			err := verifyOperandTLSArgsMatchClusterProfile(trustManagerDeploymentName, intermediateSpec)
			Expect(err).NotTo(HaveOccurred(), "intermediate profile args")
			err = waitForDeploymentRollout(tmCtx, operandNamespace, trustManagerDeploymentName, lowTimeout)
			Expect(err).NotTo(HaveOccurred())
			expectTrustManagerTLSSecret(tmCtx)

			By("patching apiserver to LegacyAdheringComponentsOnly")
			patchLegacyTLSProfile(tmCtx, &configapiv1.TLSSecurityProfile{
				Type: configapiv1.TLSProfileIntermediateType,
			})

			expectProfileManagedTLSFlagsGone(tmCtx, []string{trustManagerDeploymentName})

			By("handshaking trust-manager webhook: TLS 1.2 succeeds after Strict → Legacy")
			assertTLS12Accepted(tmCtx, []certManagerTLSEndpoint{trustManagerWebhookEndpoint()})
		})

		// Journey 5 — NEG-002
		It("should retain trust-manager TLS args after operator pod restart under Strict Modern", func() {
			original := requireAPIServerTLSConfig(tmCtx)
			DeferCleanup(restoreAPIServerTLSConfigCleanup(tmCtx, original))

			createTrustManager(tmCtx, newTrustManagerCR())
			expectTrustManagerDeploymentPresent(tmCtx)

			expectedSpec := patchStrictTLSProfile(tmCtx, &configapiv1.TLSSecurityProfile{
				Type: configapiv1.TLSProfileModernType,
			})
			err := verifyOperandTLSArgsMatchClusterProfile(trustManagerDeploymentName, expectedSpec)
			Expect(err).NotTo(HaveOccurred())

			By("deleting operator controller-manager pods")
			err = deleteOperatorControllerPods(tmCtx)
			Expect(err).NotTo(HaveOccurred())

			By("waiting for operator to become healthy again")
			Eventually(func() error {
				return VerifyHealthyOperatorConditions(certmanageroperatorclient.OperatorV1alpha1())
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("verifying trust-manager TLS args still match Modern EffectiveSpec")
			err = verifyOperandTLSArgsMatchClusterProfile(trustManagerDeploymentName, expectedSpec)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})

func requireAPIServerTLSConfig(ctx context.Context) *apiserverTLSConfig {
	GinkgoHelper()
	original, err := getClusterAPIServerTLSConfig(ctx)
	if apierrors.IsNotFound(err) {
		Skip("apiserver.config.openshift.io/cluster is not available on this cluster")
	}
	Expect(err).NotTo(HaveOccurred(), "failed to read apiserver TLS configuration")
	return original
}

func restoreAPIServerTLSConfigCleanup(ctx context.Context, original *apiserverTLSConfig) func() {
	return func() {
		By("[cleanup] restoring original apiserver TLS configuration")
		Eventually(func() error {
			return restoreClusterAPIServerTLSConfig(ctx, original)
		}, lowTimeout, fastPollInterval).Should(Succeed())
	}
}

func expectCertManagerListenersMatchProfile(ctx context.Context, spec *configapiv1.TLSProfileSpec) {
	GinkgoHelper()
	operandDeployments := []string{
		certmanagerControllerDeployment,
		certmanagerWebhookDeployment,
		certmanagerCAinjectorDeployment,
	}
	By("waiting for cert-manager operand TLS args and rollout")
	for _, name := range operandDeployments {
		err := verifyOperandTLSArgsMatchClusterProfile(name, spec)
		Expect(err).NotTo(HaveOccurred(), "deployment %s", name)
		err = waitForDeploymentRollout(ctx, operandNamespace, name, lowTimeout)
		Expect(err).NotTo(HaveOccurred(), "rollout %s", name)
		err = verifyOperandMetricsHTTPS(name)
		Expect(err).NotTo(HaveOccurred(), "https metrics %s", name)
	}
	Eventually(func() error {
		_, err := k8sClientSet.CoreV1().Secrets(operandNamespace).Get(ctx, metricsCASecretName, metav1.GetOptions{})
		return err
	}, lowTimeout, fastPollInterval).Should(Succeed(), "metrics CA secret")
	Eventually(func() error {
		_, err := k8sClientSet.CoreV1().Secrets(operandNamespace).Get(ctx, webhookCASecretName, metav1.GetOptions{})
		return err
	}, lowTimeout, fastPollInterval).Should(Succeed(), "webhook CA secret")
}

func assertModernListenerEnforcement(ctx context.Context) {
	GinkgoHelper()
	assertModernListenerEnforcementOn(ctx, certManagerTLSEndpoints())
}

func assertModernListenerEnforcementOn(ctx context.Context, endpoints []certManagerTLSEndpoint) {
	GinkgoHelper()
	for _, ep := range endpoints {
		Eventually(func() error {
			return handshakeOperandPort(ctx, ep.deployment, ep.serverName, ep.caSecret, ep.port, tls13Only())
		}, lowTimeout, fastPollInterval).Should(Succeed(), "%s TLS 1.3", ep.name)

		err := handshakeOperandPort(ctx, ep.deployment, ep.serverName, ep.caSecret, ep.port, tls12Only())
		Expect(err).To(HaveOccurred(), "%s TLS 1.2 should be rejected under TLS 1.3 min version", ep.name)
	}
}

func assertIntermediateListenerEnforcement(ctx context.Context) {
	GinkgoHelper()
	allowed := tls12Cipher(tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256)
	rejected := tls12Cipher(tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256)
	for _, ep := range certManagerTLSEndpoints() {
		Eventually(func() error {
			return handshakeOperandPort(ctx, ep.deployment, ep.serverName, ep.caSecret, ep.port, allowed)
		}, lowTimeout, fastPollInterval).Should(Succeed(), "%s TLS 1.2 allowed cipher", ep.name)

		err := handshakeOperandPort(ctx, ep.deployment, ep.serverName, ep.caSecret, ep.port, rejected)
		Expect(err).To(HaveOccurred(), "%s TLS 1.2 CBC cipher should be rejected under Intermediate", ep.name)

		err = handshakeOperandPort(ctx, ep.deployment, ep.serverName, ep.caSecret, ep.port, tls10Only())
		Expect(err).To(HaveOccurred(), "%s TLS 1.0 should be rejected under Intermediate", ep.name)
	}
}

type certManagerTLSEndpoint struct {
	name       string
	deployment string
	serverName string
	caSecret   string
	port       int
}

func certManagerTLSEndpoints() []certManagerTLSEndpoint {
	return []certManagerTLSEndpoint{
		{
			name:       "controller-metrics",
			deployment: certmanagerControllerDeployment,
			serverName: metricsServerName("cert-manager"),
			caSecret:   metricsCASecretName,
			port:       metricsPort,
		},
		{
			name:       "webhook-metrics",
			deployment: certmanagerWebhookDeployment,
			serverName: metricsServerName("cert-manager-webhook"),
			caSecret:   metricsCASecretName,
			port:       metricsPort,
		},
		{
			name:       "cainjector-metrics",
			deployment: certmanagerCAinjectorDeployment,
			serverName: metricsServerName("cert-manager-cainjector"),
			caSecret:   metricsCASecretName,
			port:       metricsPort,
		},
		{
			name:       "webhook-serving",
			deployment: certmanagerWebhookDeployment,
			serverName: metricsServerName("cert-manager-webhook"),
			caSecret:   webhookCASecretName,
			port:       webhookSecurePort,
		},
	}
}

func trustManagerWebhookEndpoint() certManagerTLSEndpoint {
	return certManagerTLSEndpoint{
		name:       "trust-manager-webhook",
		deployment: trustManagerDeploymentName,
		serverName: metricsServerName(trustManagerServiceName),
		caSecret:   trustManagerTLSSecretName,
		port:       trustManagerWebhookPort,
	}
}

func customTLS13SecurityProfile() *configapiv1.TLSSecurityProfile {
	return &configapiv1.TLSSecurityProfile{
		Type: configapiv1.TLSProfileCustomType,
		Custom: &configapiv1.CustomTLSProfile{
			TLSProfileSpec: configapiv1.TLSProfileSpec{
				MinTLSVersion: configapiv1.VersionTLS13,
				Ciphers:       []string{"TLS_AES_128_GCM_SHA256"},
			},
		},
	}
}

func patchLegacyTLSProfile(ctx context.Context, profile *configapiv1.TLSSecurityProfile) {
	GinkgoHelper()
	By(fmt.Sprintf("patching apiserver cluster to LegacyAdheringComponentsOnly with %s TLS profile", profile.Type))
	err := updateClusterAPIServerTLSConfig(ctx, profile, configapiv1.TLSAdherencePolicyLegacyAdheringComponentsOnly)
	if isTLSAdherenceUnsupported(err) {
		Skip(fmt.Sprintf("apiserver tlsAdherence is not available on this cluster: %v", err))
	}
	Expect(err).NotTo(HaveOccurred(), "failed to patch apiserver TLS configuration")
}

func expectProfileManagedTLSFlagsGone(ctx context.Context, deployments []string) {
	GinkgoHelper()
	for _, name := range deployments {
		err := waitForOperandProfileTLSArgKeysAbsent(name)
		Expect(err).NotTo(HaveOccurred(), "deployment %s still has profile-managed TLS flags under Legacy", name)

		Consistently(func() error {
			return verifyOperandProfileTLSArgKeysAbsent(name)
		}, 15*time.Second, fastPollInterval).Should(Succeed(), "deployment %s", name)

		if name != trustManagerDeploymentName {
			Eventually(func() error {
				return verifyOperandMetricsHTTPS(name)
			}, lowTimeout, fastPollInterval).Should(Succeed(), "https metrics remain on %s", name)
		}

		err = waitForDeploymentRollout(ctx, operandNamespace, name, lowTimeout)
		Expect(err).NotTo(HaveOccurred(), "rollout %s", name)
	}
}

func assertTLS12Accepted(ctx context.Context, endpoints []certManagerTLSEndpoint) {
	GinkgoHelper()
	for _, ep := range endpoints {
		Eventually(func() error {
			return handshakeOperandPort(ctx, ep.deployment, ep.serverName, ep.caSecret, ep.port, tls12Only())
		}, lowTimeout, fastPollInterval).Should(Succeed(), "%s TLS 1.2 after Legacy", ep.name)
	}
}

func expectTrustManagerTLSSecret(ctx context.Context) {
	GinkgoHelper()
	Eventually(func() error {
		_, err := k8sClientSet.CoreV1().Secrets(operandNamespace).Get(ctx, trustManagerTLSSecretName, metav1.GetOptions{})
		return err
	}, lowTimeout, fastPollInterval).Should(Succeed(), "trust-manager TLS secret")
}

func patchStrictTLSProfile(ctx context.Context, profile *configapiv1.TLSSecurityProfile) *configapiv1.TLSProfileSpec {
	GinkgoHelper()
	By(fmt.Sprintf("patching apiserver cluster to StrictAllComponents with %s TLS profile", profile.Type))
	err := updateClusterAPIServerTLSConfig(ctx, profile, configapiv1.TLSAdherencePolicyStrictAllComponents)
	if isTLSAdherenceUnsupported(err) {
		Skip(fmt.Sprintf("apiserver tlsAdherence is not available on this cluster: %v", err))
	}
	Expect(err).NotTo(HaveOccurred(), "failed to patch apiserver TLS configuration")

	expectedSpec, err := tlsprofile.EffectiveSpec(profile)
	Expect(err).NotTo(HaveOccurred(), "failed to resolve expected TLS profile spec")
	return expectedSpec
}

func expectTrustManagerDeploymentPresent(ctx context.Context) {
	GinkgoHelper()
	By("waiting for trust-manager deployment to exist")
	Eventually(func() error {
		_, err := k8sClientSet.AppsV1().Deployments(operandNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
		return err
	}, lowTimeout, fastPollInterval).Should(Succeed())
}

func deleteOperatorControllerPods(ctx context.Context) error {
	dep, err := k8sClientSet.AppsV1().Deployments(operatorNamespace).Get(ctx, operatorDeploymentName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	selector, err := metav1.LabelSelectorAsSelector(dep.Spec.Selector)
	if err != nil {
		return err
	}
	return k8sClientSet.CoreV1().Pods(operatorNamespace).DeleteCollection(ctx, metav1.DeleteOptions{
		GracePeriodSeconds: ptr.To[int64](0),
	}, metav1.ListOptions{LabelSelector: selector.String()})
}
