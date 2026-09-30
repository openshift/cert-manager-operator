//go:build e2e
// +build e2e

package e2e

import (
	"context"
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

	// E2E-012 — Legacy: always-on HTTPS metrics remain; Strict profile TLS flags absent.
	It("should keep HTTPS metrics under LegacyAdheringComponentsOnly while omitting profile TLS flags", func() {
		original := requireAPIServerTLSConfig(ctx)
		DeferCleanup(restoreAPIServerTLSConfigCleanup(ctx, original))

		modernProfile := &configapiv1.TLSSecurityProfile{
			Type: configapiv1.TLSProfileModernType,
		}
		By("patching apiserver to LegacyAdheringComponentsOnly with Modern profile")
		err := updateClusterAPIServerTLSConfig(ctx, modernProfile, configapiv1.TLSAdherencePolicyLegacyAdheringComponentsOnly)
		if isTLSAdherenceUnsupported(err) {
			Skip(fmt.Sprintf("apiserver tlsAdherence is not available on this cluster: %v", err))
		}
		Expect(err).NotTo(HaveOccurred())

		operandDeployments := []string{
			certmanagerControllerDeployment,
			certmanagerWebhookDeployment,
			certmanagerCAinjectorDeployment,
		}

		By("verifying metrics-dynamic-serving HTTPS args remain present under Legacy")
		for _, name := range operandDeployments {
			Eventually(func() error {
				return verifyOperandMetricsHTTPS(name)
			}, lowTimeout, fastPollInterval).Should(Succeed(), "deployment %s", name)
		}

		modernSpec, err := tlsprofile.EffectiveSpec(modernProfile)
		Expect(err).NotTo(HaveOccurred())

		By("waiting for Modern Strict profile TLS flags to be absent, then consistently re-checking")
		for _, name := range operandDeployments {
			unexpected := expectedOperandTLSArgs(name, modernSpec)
			Expect(unexpected).NotTo(BeEmpty(), "deployment %s", name)

			err := waitForOperandTLSArgsAbsent(name, unexpected)
			Expect(err).NotTo(HaveOccurred(), "deployment %s still has profile TLS flags under Legacy", name)

			Consistently(func() error {
				return verifyOperandTLSArgsNotPresent(name, unexpected)
			}, 15*time.Second, fastPollInterval).Should(Succeed(), "deployment %s", name)
		}
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
