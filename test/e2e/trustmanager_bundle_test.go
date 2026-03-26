//go:build e2e
// +build e2e

// This file tests end-to-end Bundle CR behavior under various TrustManager configurations.
// Tests are grouped by TrustManager configuration and exercise the full flow from
// Bundle creation through target sync verification.
//
// Group 1 — Default TrustManager (no optional features):
//   - Inline source → ConfigMap target (+ target data drift reconciliation)
//   - ConfigMap source → ConfigMap target (+ source update propagation)
//   - Secret source → ConfigMap target
//   - Multiple sources (ConfigMap + Inline) → ConfigMap target
//   - Custom metadata (labels/annotations) on target ConfigMaps
//   - Namespace selector filtering
//   - Inline source update → target re-sync
//   - Bundle deletion → target cleanup
//   - Negative: Secret target without SecretTargets enabled
//   - Negative: useDefaultCAs without DefaultCAPackage enabled
//   - Negative: ConfigMap source outside trust namespace not synced
//
// Group 2 — SecretTargets enabled:
//   - Inline source → Secret target
//   - Inline source → ConfigMap + Secret dual targets
//   - ConfigMap source → Secret target
//   - Negative: Bundle name not in authorizedSecrets list
//
// Group 3 — DefaultCAPackage enabled:
//   - useDefaultCAs → ConfigMap target
//   - useDefaultCAs + Inline → ConfigMap target (combined data)
//
// Group 4 — SecretTargets + DefaultCAPackage enabled:
//   - useDefaultCAs + Inline → ConfigMap + Secret dual targets
//
// Group 5 — Custom TrustNamespace:
//   - ConfigMap source in custom trust namespace → ConfigMap target
//   - Negative: ConfigMap source in default namespace not synced when custom trust namespace is configured
package e2e

import (
	"context"
	"crypto/x509"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	trustapi "github.com/cert-manager/trust-manager/pkg/apis/trust/v1alpha1"
	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	testutils "github.com/openshift/cert-manager-operator/pkg/controller/istiocsr"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	bundleTargetKey          = "ca-bundle.crt"
	bundleTestNamespaceLabel = "bundle-e2e-test"
	bundleSourceKey          = "ca.crt"
)

var _ = Describe("Bundle", Ordered, Label("Feature:TrustManager"), func() {
	ctx := context.TODO()

	var (
		testNS                     *corev1.Namespace
		testCertPEM1, testCertPEM2 string
	)

	BeforeAll(func() {
		By("enabling TrustManager feature gate")
		err := patchSubscriptionWithEnvVars(ctx, loader, map[string]string{
			"UNSUPPORTED_ADDON_FEATURES": "TrustManager=true",
		})
		Expect(err).ShouldNot(HaveOccurred())

		By("waiting for operator deployment rollout with feature gate")
		err = waitForDeploymentEnvVarAndRollout(ctx, operatorNamespace, operatorDeploymentName,
			"UNSUPPORTED_ADDON_FEATURES", "TrustManager=true", highTimeout)
		Expect(err).ShouldNot(HaveOccurred())

		By("generating test certificates")
		caTweak := func(cert *x509.Certificate) {
			cert.IsCA = true
			cert.KeyUsage |= x509.KeyUsageCertSign
		}
		testCertPEM1 = testutils.GenerateCertificate("e2e-test-ca-1", []string{"cert-manager-operator-e2e"}, caTweak)
		testCertPEM2 = testutils.GenerateCertificate("e2e-test-ca-2", []string{"cert-manager-operator-e2e"}, caTweak)

		By("creating test namespace for target verification")
		testNS = createNamespaceWithCleanup(ctx, "bundle-e2e-", map[string]string{bundleTestNamespaceLabel: "true"})
	})

	// ===== Group 1: Default TrustManager =====
	Context("with default TrustManager configuration", Ordered, func() {
		BeforeAll(func() { createTrustManager(ctx, newTrustManagerCR()) })
		AfterAll(func() { deleteTrustManager(ctx) })

		It("should sync inline source to ConfigMap target and restore tampered target", func() {
			bundleName := "bundle-inline-cm-" + randomStr(5)
			bundle := newBundle(bundleName).
				WithInLineSource(testCertPEM1).
				WithConfigMapTarget(bundleTargetKey).
				Build()

			createBundleWithCleanup(ctx, bundle)

			By("verifying ConfigMap target is synced in test namespace")
			err := waitForConfigMapTarget(ctx, bundleClient, bundleName, testNS.Name, bundleTargetKey, testCertPEM1, highTimeout)
			Expect(err).ShouldNot(HaveOccurred())

			verifyBundleSynced(ctx, bundleName)

			By("tampering with the target ConfigMap data")
			targetCM, err := k8sClientSet.CoreV1().ConfigMaps(testNS.Name).Get(ctx, bundleName, metav1.GetOptions{})
			Expect(err).ShouldNot(HaveOccurred())
			targetCM.Data[bundleTargetKey] = "tampered-data"
			_, err = k8sClientSet.CoreV1().ConfigMaps(testNS.Name).Update(ctx, targetCM, metav1.UpdateOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			By("verifying trust-manager restores the target ConfigMap")
			err = waitForConfigMapTarget(ctx, bundleClient, bundleName, testNS.Name, bundleTargetKey, testCertPEM1, highTimeout)
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("should sync ConfigMap source to ConfigMap target and re-sync on source update", func() {
			bundleName := "bundle-cm-src-" + randomStr(5)
			sourceCMName := "bundle-source-cm-" + randomStr(5)

			By("creating source ConfigMap in trust namespace")
			createSourceConfigMap(ctx, trustManagerNamespace, sourceCMName, bundleSourceKey, testCertPEM1)

			bundle := newBundle(bundleName).
				WithConfigMapSource(sourceCMName, bundleSourceKey).
				WithConfigMapTarget(bundleTargetKey).
				Build()

			createBundleWithCleanup(ctx, bundle)

			By("verifying ConfigMap target contains source data")
			err := waitForConfigMapTarget(ctx, bundleClient, bundleName, testNS.Name, bundleTargetKey, testCertPEM1, highTimeout)
			Expect(err).ShouldNot(HaveOccurred())

			By("updating source ConfigMap data")
			Eventually(func() error {
				current, err := k8sClientSet.CoreV1().ConfigMaps(trustManagerNamespace).Get(ctx, sourceCMName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				current.Data[bundleSourceKey] = testCertPEM2
				_, err = k8sClientSet.CoreV1().ConfigMaps(trustManagerNamespace).Update(ctx, current, metav1.UpdateOptions{})
				return err
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("verifying target reflects updated source data")
			err = waitForConfigMapTarget(ctx, bundleClient, bundleName, testNS.Name, bundleTargetKey, testCertPEM2, highTimeout)
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("should sync Secret source to ConfigMap target", func() {
			bundleName := "bundle-secret-src-" + randomStr(5)
			sourceSecretName := "bundle-source-secret-" + randomStr(5)

			By("creating source Secret in trust namespace")
			createSourceSecret(ctx, trustManagerNamespace, sourceSecretName, bundleSourceKey, testCertPEM1)

			bundle := newBundle(bundleName).
				WithSecretSource(sourceSecretName, bundleSourceKey).
				WithConfigMapTarget(bundleTargetKey).
				Build()

			createBundleWithCleanup(ctx, bundle)

			By("verifying ConfigMap target contains source Secret data")
			err := waitForConfigMapTarget(ctx, bundleClient, bundleName, testNS.Name, bundleTargetKey, testCertPEM1, highTimeout)
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("should sync multiple sources to ConfigMap target", func() {
			bundleName := "bundle-multi-src-" + randomStr(5)
			sourceCMName := "bundle-multi-cm-" + randomStr(5)

			By("creating source ConfigMap in trust namespace")
			createSourceConfigMap(ctx, trustManagerNamespace, sourceCMName, bundleSourceKey, testCertPEM1)

			bundle := newBundle(bundleName).
				WithConfigMapSource(sourceCMName, bundleSourceKey).
				WithInLineSource(testCertPEM2).
				WithConfigMapTarget(bundleTargetKey).
				Build()

			createBundleWithCleanup(ctx, bundle)

			By("verifying ConfigMap target contains data from all sources")
			err := waitForConfigMapTarget(ctx, bundleClient, bundleName, testNS.Name, bundleTargetKey, testCertPEM1, highTimeout)
			Expect(err).ShouldNot(HaveOccurred())
			err = waitForConfigMapTarget(ctx, bundleClient, bundleName, testNS.Name, bundleTargetKey, testCertPEM2, lowTimeout)
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("should apply custom metadata to target ConfigMaps", func() {
			bundleName := "bundle-meta-" + randomStr(5)
			bundle := newBundle(bundleName).
				WithInLineSource(testCertPEM1).
				WithConfigMapTarget(bundleTargetKey).
				WithTargetMetadata(
					map[string]string{"e2e-label": "test-value"},
					map[string]string{"e2e-annotation": "test-value"},
				).
				Build()

			createBundleWithCleanup(ctx, bundle)

			By("verifying target ConfigMap has custom labels and annotations")
			Eventually(func(g Gomega) {
				cm, err := k8sClientSet.CoreV1().ConfigMaps(testNS.Name).Get(ctx, bundleName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(cm.Labels).Should(HaveKeyWithValue("e2e-label", "test-value"))
				g.Expect(cm.Annotations).Should(HaveKeyWithValue("e2e-annotation", "test-value"))
			}, highTimeout, fastPollInterval).Should(Succeed())
		})

		It("should sync only to namespaces matching selector", func() {
			bundleName := "bundle-ns-sel-" + randomStr(5)
			selectorLabel := "bundle-selector-" + randomStr(5)

			By("creating a matching namespace")
			matchNS := createNamespaceWithCleanup(ctx, "bundle-match-", map[string]string{selectorLabel: "true"})

			By("creating a non-matching namespace")
			noMatchNS := createNamespaceWithCleanup(ctx, "bundle-nomatch-", nil)

			bundle := newBundle(bundleName).
				WithInLineSource(testCertPEM1).
				WithConfigMapTarget(bundleTargetKey).
				WithNamespaceSelector(map[string]string{selectorLabel: "true"}).
				Build()

			createBundleWithCleanup(ctx, bundle)

			By("verifying ConfigMap exists in matching namespace")
			err := waitForConfigMapTarget(ctx, bundleClient, bundleName, matchNS.Name, bundleTargetKey, testCertPEM1, highTimeout)
			Expect(err).ShouldNot(HaveOccurred())

			By("verifying ConfigMap does NOT exist in non-matching namespace")
			Consistently(func() bool {
				_, err := k8sClientSet.CoreV1().ConfigMaps(noMatchNS.Name).Get(ctx, bundleName, metav1.GetOptions{})
				return apierrors.IsNotFound(err)
			}, "30s", fastPollInterval).Should(BeTrue())
		})

		It("should update targets when inline source changes", func() {
			bundleName := "bundle-update-" + randomStr(5)
			bundle := newBundle(bundleName).
				WithInLineSource(testCertPEM1).
				WithConfigMapTarget(bundleTargetKey).
				Build()

			createBundleWithCleanup(ctx, bundle)

			By("verifying initial sync")
			err := waitForConfigMapTarget(ctx, bundleClient, bundleName, testNS.Name, bundleTargetKey, testCertPEM1, highTimeout)
			Expect(err).ShouldNot(HaveOccurred())

			By("updating Bundle inline source")
			Eventually(func() error {
				var current trustapi.Bundle
				if err := bundleClient.Get(ctx, crclient.ObjectKey{Name: bundleName}, &current); err != nil {
					return err
				}
				current.Spec.Sources[0].InLine = &testCertPEM2
				return bundleClient.Update(ctx, &current)
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("verifying target reflects updated data")
			err = waitForConfigMapTarget(ctx, bundleClient, bundleName, testNS.Name, bundleTargetKey, testCertPEM2, highTimeout)
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("should remove targets when Bundle is deleted", func() {
			bundleName := "bundle-delete-" + randomStr(5)
			bundle := newBundle(bundleName).
				WithInLineSource(testCertPEM1).
				WithConfigMapTarget(bundleTargetKey).
				Build()

			Expect(bundleClient.Create(ctx, bundle)).ShouldNot(HaveOccurred())

			By("verifying target is synced")
			err := waitForConfigMapTarget(ctx, bundleClient, bundleName, testNS.Name, bundleTargetKey, testCertPEM1, highTimeout)
			Expect(err).ShouldNot(HaveOccurred())

			By("deleting the Bundle")
			deleteBundle(ctx, bundleName)

			By("verifying targets are removed")
			err = waitForTargetRemoved(ctx, bundleClient, bundleName, testNS.Name, lowTimeout)
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("should report error in Bundle status when targeting Secret without SecretTargets enabled", func() {
			bundleName := "bundle-no-secret-" + randomStr(5)
			bundle := newBundle(bundleName).
				WithInLineSource(testCertPEM1).
				WithSecretTarget(bundleTargetKey).
				Build()

			createBundleWithCleanup(ctx, bundle)

			By("verifying Bundle status condition shows not synced")
			verifyBundleNeverSynced(ctx, bundleName)

			By("verifying no Secret is created in test namespace")
			_, err := k8sClientSet.CoreV1().Secrets(testNS.Name).Get(ctx, bundleName, metav1.GetOptions{})
			Expect(apierrors.IsNotFound(err)).Should(BeTrue())
		})

		It("should report error in Bundle status when using useDefaultCAs without DefaultCAPackage", func() {
			bundleName := "bundle-no-default-ca-" + randomStr(5)
			bundle := newBundle(bundleName).
				WithUseDefaultCAs().
				WithConfigMapTarget(bundleTargetKey).
				Build()

			createBundleWithCleanup(ctx, bundle)

			By("verifying Bundle status does not reach Synced=True")
			verifyBundleNeverSynced(ctx, bundleName)
		})

		It("should not sync ConfigMap source that exists outside the trust namespace", func() {
			bundleName := "bundle-wrong-ns-" + randomStr(5)
			sourceCMName := "src-wrong-ns-" + randomStr(5)

			By(fmt.Sprintf("creating source ConfigMap in test namespace %q instead of trust namespace %q", testNS.Name, trustManagerNamespace))
			createSourceConfigMap(ctx, testNS.Name, sourceCMName, bundleSourceKey, testCertPEM1)

			bundle := newBundle(bundleName).
				WithConfigMapSource(sourceCMName, bundleSourceKey).
				WithConfigMapTarget(bundleTargetKey).
				Build()

			createBundleWithCleanup(ctx, bundle)

			By("verifying Bundle does not reach Synced=True because source is not in trust namespace")
			verifyBundleNeverSynced(ctx, bundleName)

			By("verifying no target ConfigMap is created in test namespace")
			_, err := k8sClientSet.CoreV1().ConfigMaps(testNS.Name).Get(ctx, bundleName, metav1.GetOptions{})
			Expect(apierrors.IsNotFound(err)).Should(BeTrue())
		})
	})

	// ===== Group 2: SecretTargets enabled =====
	Context("with SecretTargets enabled", Ordered, func() {
		const (
			bundleSecretTarget     = "bundle-secret-tgt"
			bundleDualTarget       = "bundle-dual-tgt"
			bundleCMToSecretTarget = "bundle-cm-to-secret"
		)

		BeforeAll(func() {
			createTrustManager(ctx, newTrustManagerCR().WithSecretTargets(
				v1alpha1.SecretTargetsPolicyCustom,
				[]string{bundleSecretTarget, bundleDualTarget, bundleCMToSecretTarget},
			))
		})
		AfterAll(func() { deleteTrustManager(ctx) })

		It("should sync inline source to Secret target", func() {
			bundle := newBundle(bundleSecretTarget).
				WithInLineSource(testCertPEM1).
				WithSecretTarget(bundleTargetKey).
				Build()

			createBundleWithCleanup(ctx, bundle)

			By("verifying Secret target is synced in test namespace")
			err := waitForSecretTarget(ctx, bundleClient, bundleSecretTarget, testNS.Name, bundleTargetKey, testCertPEM1, highTimeout)
			Expect(err).ShouldNot(HaveOccurred())

			verifyBundleSynced(ctx, bundleSecretTarget)
		})

		It("should sync to both ConfigMap and Secret targets", func() {
			bundle := newBundle(bundleDualTarget).
				WithInLineSource(testCertPEM1).
				WithConfigMapTarget(bundleTargetKey).
				WithSecretTarget(bundleTargetKey).
				Build()

			createBundleWithCleanup(ctx, bundle)

			By("verifying ConfigMap target is synced")
			err := waitForConfigMapTarget(ctx, bundleClient, bundleDualTarget, testNS.Name, bundleTargetKey, testCertPEM1, highTimeout)
			Expect(err).ShouldNot(HaveOccurred())

			By("verifying Secret target is synced")
			err = waitForSecretTarget(ctx, bundleClient, bundleDualTarget, testNS.Name, bundleTargetKey, testCertPEM1, highTimeout)
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("should sync ConfigMap source to Secret target", func() {
			sourceCMName := "bundle-cm-secret-src-" + randomStr(5)

			By("creating source ConfigMap in trust namespace")
			createSourceConfigMap(ctx, trustManagerNamespace, sourceCMName, bundleSourceKey, testCertPEM1)

			bundle := newBundle(bundleCMToSecretTarget).
				WithConfigMapSource(sourceCMName, bundleSourceKey).
				WithSecretTarget(bundleTargetKey).
				Build()

			createBundleWithCleanup(ctx, bundle)

			By("verifying Secret target contains source ConfigMap data")
			err := waitForSecretTarget(ctx, bundleClient, bundleCMToSecretTarget, testNS.Name, bundleTargetKey, testCertPEM1, highTimeout)
			Expect(err).ShouldNot(HaveOccurred())

			verifyBundleSynced(ctx, bundleCMToSecretTarget)
		})

		It("should not sync Secret when Bundle name is not in authorizedSecrets list", func() {
			bundleName := "bundle-not-authorized"
			bundle := newBundle(bundleName).
				WithInLineSource(testCertPEM1).
				WithSecretTarget(bundleTargetKey).
				Build()

			createBundleWithCleanup(ctx, bundle)

			By("verifying Bundle status does not reach Synced=True")
			verifyBundleNeverSynced(ctx, bundleName)

			By(fmt.Sprintf("verifying no Secret named %q exists in test namespace", bundleName))
			_, err := k8sClientSet.CoreV1().Secrets(testNS.Name).Get(ctx, bundleName, metav1.GetOptions{})
			Expect(apierrors.IsNotFound(err)).Should(BeTrue())
		})
	})

	// ===== Group 3: DefaultCAPackage enabled =====
	Context("with DefaultCAPackage enabled", Ordered, func() {
		BeforeAll(func() {
			createTrustManager(ctx, newTrustManagerCR().WithDefaultCAPackage(v1alpha1.DefaultCAPackagePolicyEnabled))

			By("waiting for default CA package ConfigMap to be created")
			err := pollTillConfigMapAvailable(ctx, k8sClientSet, trustManagerNamespace, defaultCAPackageConfigMapName)
			Expect(err).ShouldNot(HaveOccurred())
		})
		AfterAll(func() { deleteTrustManager(ctx) })

		It("should sync useDefaultCAs source to ConfigMap target", func() {
			bundleName := "bundle-default-cas-" + randomStr(5)
			bundle := newBundle(bundleName).
				WithUseDefaultCAs().
				WithConfigMapTarget(bundleTargetKey).
				Build()

			createBundleWithCleanup(ctx, bundle)

			By("verifying ConfigMap target contains PEM certificates from default CAs")
			Eventually(func(g Gomega) {
				cm, err := k8sClientSet.CoreV1().ConfigMaps(testNS.Name).Get(ctx, bundleName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				data, ok := cm.Data[bundleTargetKey]
				g.Expect(ok).Should(BeTrue(), "target ConfigMap should contain key %q", bundleTargetKey)
				g.Expect(data).ShouldNot(BeEmpty())
				g.Expect(containsPEMCertificates(data)).Should(BeTrue(), "target data should contain valid PEM certificates")
			}, highTimeout, slowPollInterval).Should(Succeed())

			verifyBundleSynced(ctx, bundleName)
		})

		It("should include default CAs alongside explicit inline source", func() {
			bundleName := "bundle-cas-inline-" + randomStr(5)
			bundle := newBundle(bundleName).
				WithInLineSource(testCertPEM1).
				WithUseDefaultCAs().
				WithConfigMapTarget(bundleTargetKey).
				Build()

			createBundleWithCleanup(ctx, bundle)

			By("verifying target contains the inline certificate")
			err := waitForConfigMapTarget(ctx, bundleClient, bundleName, testNS.Name, bundleTargetKey, testCertPEM1, highTimeout)
			Expect(err).ShouldNot(HaveOccurred())

			By("verifying target also contains default CA certificates")
			Eventually(func(g Gomega) {
				cm, err := k8sClientSet.CoreV1().ConfigMaps(testNS.Name).Get(ctx, bundleName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				data := cm.Data[bundleTargetKey]
				g.Expect(strings.Contains(data, testCertPEM1)).Should(BeTrue(), "should contain inline cert")
				g.Expect(strings.Count(data, "-----BEGIN CERTIFICATE-----")).Should(BeNumerically(">", 1),
					"should contain multiple certificates (inline + default CAs)")
			}, highTimeout, slowPollInterval).Should(Succeed())
		})
	})

	// ===== Group 4: Combined SecretTargets + DefaultCAPackage =====
	Context("with SecretTargets and DefaultCAPackage enabled", Ordered, func() {
		const bundleCombined = "bundle-combined"

		BeforeAll(func() {
			createTrustManager(ctx, newTrustManagerCR().
				WithSecretTargets(v1alpha1.SecretTargetsPolicyCustom, []string{bundleCombined}).
				WithDefaultCAPackage(v1alpha1.DefaultCAPackagePolicyEnabled))

			By("waiting for default CA package ConfigMap to be created")
			err := pollTillConfigMapAvailable(ctx, k8sClientSet, trustManagerNamespace, defaultCAPackageConfigMapName)
			Expect(err).ShouldNot(HaveOccurred())
		})
		AfterAll(func() { deleteTrustManager(ctx) })

		It("should sync useDefaultCAs and inline sources to both ConfigMap and Secret targets", func() {
			bundle := newBundle(bundleCombined).
				WithInLineSource(testCertPEM1).
				WithUseDefaultCAs().
				WithConfigMapTarget(bundleTargetKey).
				WithSecretTarget(bundleTargetKey).
				Build()

			createBundleWithCleanup(ctx, bundle)

			By("verifying ConfigMap target contains inline cert and default CAs")
			Eventually(func(g Gomega) {
				cm, err := k8sClientSet.CoreV1().ConfigMaps(testNS.Name).Get(ctx, bundleCombined, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				data := cm.Data[bundleTargetKey]
				g.Expect(strings.Contains(data, testCertPEM1)).Should(BeTrue(), "ConfigMap should contain inline cert")
				g.Expect(strings.Count(data, "-----BEGIN CERTIFICATE-----")).Should(BeNumerically(">", 1))
			}, highTimeout, slowPollInterval).Should(Succeed())

			By("verifying Secret target contains inline cert and default CAs")
			Eventually(func(g Gomega) {
				secret, err := k8sClientSet.CoreV1().Secrets(testNS.Name).Get(ctx, bundleCombined, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				data := string(secret.Data[bundleTargetKey])
				g.Expect(strings.Contains(data, testCertPEM1)).Should(BeTrue(), "Secret should contain inline cert")
				g.Expect(strings.Count(data, "-----BEGIN CERTIFICATE-----")).Should(BeNumerically(">", 1))
			}, highTimeout, slowPollInterval).Should(Succeed())

			verifyBundleSynced(ctx, bundleCombined)
		})
	})

	// ===== Group 5: Custom TrustNamespace =====
	Context("with custom trustNamespace", Ordered, func() {
		var customTrustNS *corev1.Namespace

		BeforeAll(func() {
			By("creating custom trust namespace")
			customTrustNS = createNamespaceWithCleanup(ctx, "custom-trust-ns-", nil)

			createTrustManager(ctx, newTrustManagerCR().WithTrustNamespace(customTrustNS.Name))

			By(fmt.Sprintf("verifying deployment has --trust-namespace=%s", customTrustNS.Name))
			Eventually(func(g Gomega) {
				dep, err := k8sClientSet.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(dep.Spec.Template.Spec.Containers).ShouldNot(BeEmpty())
				g.Expect(dep.Spec.Template.Spec.Containers[0].Args).Should(
					ContainElement(fmt.Sprintf("--trust-namespace=%s", customTrustNS.Name)),
				)
			}, lowTimeout, fastPollInterval).Should(Succeed())
		})

		AfterAll(func() { deleteTrustManager(ctx) })

		It("should sync ConfigMap source from custom trust namespace to target", func() {
			bundleName := "bundle-custom-ns-" + randomStr(5)
			sourceCMName := "src-custom-ns-" + randomStr(5)

			By(fmt.Sprintf("creating source ConfigMap in custom trust namespace %q", customTrustNS.Name))
			createSourceConfigMap(ctx, customTrustNS.Name, sourceCMName, bundleSourceKey, testCertPEM1)

			bundle := newBundle(bundleName).
				WithConfigMapSource(sourceCMName, bundleSourceKey).
				WithConfigMapTarget(bundleTargetKey).
				Build()

			createBundleWithCleanup(ctx, bundle)

			By("verifying ConfigMap target is synced in test namespace")
			err := waitForConfigMapTarget(ctx, bundleClient, bundleName, testNS.Name, bundleTargetKey, testCertPEM1, highTimeout)
			Expect(err).ShouldNot(HaveOccurred())

			verifyBundleSynced(ctx, bundleName)
		})

		It("should not sync ConfigMap source from default namespace when custom trust namespace is configured", func() {
			bundleName := "bundle-default-ns-miss-" + randomStr(5)
			sourceCMName := "src-default-ns-miss-" + randomStr(5)

			By(fmt.Sprintf("creating source ConfigMap in default namespace %q (not the custom trust namespace %q)", trustManagerNamespace, customTrustNS.Name))
			createSourceConfigMap(ctx, trustManagerNamespace, sourceCMName, bundleSourceKey, testCertPEM1)

			bundle := newBundle(bundleName).
				WithConfigMapSource(sourceCMName, bundleSourceKey).
				WithConfigMapTarget(bundleTargetKey).
				Build()

			createBundleWithCleanup(ctx, bundle)

			By("verifying Bundle does not reach Synced=True because source is not in custom trust namespace")
			verifyBundleNeverSynced(ctx, bundleName)

			By("verifying no target ConfigMap is created in test namespace")
			_, err := k8sClientSet.CoreV1().ConfigMaps(testNS.Name).Get(ctx, bundleName, metav1.GetOptions{})
			Expect(apierrors.IsNotFound(err)).Should(BeTrue())
		})
	})
})
