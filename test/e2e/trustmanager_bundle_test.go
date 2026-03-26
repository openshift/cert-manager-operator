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
//   - Negative: ConfigMap + Secret sources outside trust namespace not synced
//
// Group 2 — SecretTargets enabled:
//   - Inline source → Secret target
//   - Inline source → ConfigMap + Secret dual targets
//   - ConfigMap source → Secret target
//   - Secret target data drift reconciliation (tamper → restore)
//   - Negative: Bundle name not in authorizedSecrets list
//   - Transition: Enabled → Disabled → existing synced Bundle reports SecretTargetsDisabled
//
// Group 3 — DefaultCAPackage enabled:
//   - useDefaultCAs → ConfigMap target
//   - useDefaultCAs + Inline → ConfigMap target (combined data)
//   - Unintended drift: package ConfigMap tampered → operator restores, Bundle targets unaffected
//   - Intended drift: CNO CA bundle update via Proxy CR → propagated through to Bundle target ConfigMaps
//
// Group 4 — SecretTargets + DefaultCAPackage enabled:
//   - useDefaultCAs + Inline → ConfigMap + Secret dual targets
//
// Group 5 — Custom TrustNamespace:
//   - ConfigMap source in custom trust namespace → ConfigMap target
//   - Secret source in custom trust namespace → ConfigMap target
//   - Negative: ConfigMap + Secret sources in default namespace not synced when custom trust namespace is configured
//
// Group 6 — FilterExpiredCertificates enabled:
//   - ConfigMap source with valid + expired certs → only valid cert in ConfigMap target
//   - Transition to Disabled → same Bundle re-syncs with both certs in target
package e2e

import (
	"context"
	"crypto/x509"
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	trustapi "github.com/cert-manager/trust-manager/pkg/apis/trust/v1alpha1"
	configopenshiftv1 "github.com/openshift/api/config/v1"
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

var _ = Describe("Bundle", Ordered, Label("Platform:Generic", "Feature:TrustManager", "TechPreview"), func() {
	ctx := context.TODO()

	var (
		testNS                                     *corev1.Namespace
		testCertPEM1, testCertPEM2, expiredCertPEM string

		originalUnsupportedAddonFeatures string
		originalOperatorLogLevel         string
	)

	BeforeAll(func() {
		By("capturing original UNSUPPORTED_ADDON_FEATURES from subscription before patching")
		var err error
		originalUnsupportedAddonFeatures, err = getSubscriptionEnvVar(ctx, loader, "UNSUPPORTED_ADDON_FEATURES")
		Expect(err).ShouldNot(HaveOccurred())

		By("capturing original OPERATOR_LOG_LEVEL from subscription before patching")
		originalOperatorLogLevel, err = getSubscriptionEnvVar(ctx, loader, "OPERATOR_LOG_LEVEL")
		Expect(err).ShouldNot(HaveOccurred())

		By("enabling TrustManager feature gate via subscription")
		err = patchSubscriptionWithEnvVars(ctx, loader, map[string]string{
			"UNSUPPORTED_ADDON_FEATURES": "TrustManager=true",
			"OPERATOR_LOG_LEVEL":         "4",
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

		expiredCATweak := func(cert *x509.Certificate) {
			cert.IsCA = true
			cert.KeyUsage |= x509.KeyUsageCertSign
			cert.NotBefore = time.Now().Add(-48 * time.Hour)
			cert.NotAfter = time.Now().Add(-24 * time.Hour)
		}
		expiredCertPEM = testutils.GenerateCertificate("e2e-expired-ca", []string{"cert-manager-operator-e2e"}, expiredCATweak)

		By("creating test namespace for target verification")
		testNS = createNamespaceWithCleanup(ctx, "bundle-e2e-", map[string]string{bundleTestNamespaceLabel: "true"})
	})

	AfterAll(func() {
		By("restoring UNSUPPORTED_ADDON_FEATURES and OPERATOR_LOG_LEVEL on subscription to pre-suite values")
		err := patchSubscriptionWithEnvVars(ctx, loader, map[string]string{
			"UNSUPPORTED_ADDON_FEATURES": originalUnsupportedAddonFeatures,
			"OPERATOR_LOG_LEVEL":         originalOperatorLogLevel,
		})
		Expect(err).ShouldNot(HaveOccurred())
		if originalUnsupportedAddonFeatures == "" {
			By("waiting for operator deployment to rollout after removing UNSUPPORTED_ADDON_FEATURES")
			err = waitForDeploymentEnvVarRemovedAndRollout(ctx, operatorNamespace, operatorDeploymentName, "UNSUPPORTED_ADDON_FEATURES", lowTimeout)
		} else {
			By("waiting for operator deployment to rollout with restored UNSUPPORTED_ADDON_FEATURES")
			err = waitForDeploymentEnvVarAndRollout(ctx, operatorNamespace, operatorDeploymentName, "UNSUPPORTED_ADDON_FEATURES", originalUnsupportedAddonFeatures, lowTimeout)
		}
		Expect(err).ShouldNot(HaveOccurred())
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

		It("should not sync sources that exist outside the trust namespace", func() {
			bundleName := "bundle-wrong-ns-" + randomStr(5)
			sourceCMName := "src-wrong-ns-" + randomStr(5)
			sourceSecretName := "src-secret-wrong-ns-" + randomStr(5)

			By(fmt.Sprintf("creating source ConfigMap and Secret in test namespace %q instead of trust namespace %q", testNS.Name, trustManagerNamespace))
			createSourceConfigMap(ctx, testNS.Name, sourceCMName, bundleSourceKey, testCertPEM1)
			createSourceSecret(ctx, testNS.Name, sourceSecretName, bundleSourceKey, testCertPEM2)

			bundle := newBundle(bundleName).
				WithConfigMapSource(sourceCMName, bundleSourceKey).
				WithSecretSource(sourceSecretName, bundleSourceKey).
				WithConfigMapTarget(bundleTargetKey).
				Build()

			createBundleWithCleanup(ctx, bundle)

			By("verifying Bundle does not reach Synced=True because sources are not in trust namespace")
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
			bundleSecretDrift      = "bundle-secret-drift"
			bundleSecretDisable    = "bundle-secret-disable"
		)

		BeforeAll(func() {
			createTrustManager(ctx, newTrustManagerCR().WithSecretTargets(
				v1alpha1.SecretTargetsPolicyCustom,
				[]string{bundleSecretTarget, bundleDualTarget, bundleCMToSecretTarget, bundleSecretDrift, bundleSecretDisable},
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

		It("should restore Secret target when tampered", func() {
			bundle := newBundle(bundleSecretDrift).
				WithInLineSource(testCertPEM1).
				WithSecretTarget(bundleTargetKey).
				Build()

			createBundleWithCleanup(ctx, bundle)

			By("verifying Secret target is synced")
			err := waitForSecretTarget(ctx, bundleClient, bundleSecretDrift, testNS.Name, bundleTargetKey, testCertPEM1, highTimeout)
			Expect(err).ShouldNot(HaveOccurred())

			By("tampering with the target Secret data")
			targetSecret, err := k8sClientSet.CoreV1().Secrets(testNS.Name).Get(ctx, bundleSecretDrift, metav1.GetOptions{})
			Expect(err).ShouldNot(HaveOccurred())
			targetSecret.Data[bundleTargetKey] = []byte("tampered-data")
			_, err = k8sClientSet.CoreV1().Secrets(testNS.Name).Update(ctx, targetSecret, metav1.UpdateOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			By("verifying trust-manager restores the target Secret")
			err = waitForSecretTarget(ctx, bundleClient, bundleSecretDrift, testNS.Name, bundleTargetKey, testCertPEM1, highTimeout)
			Expect(err).ShouldNot(HaveOccurred())
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

		It("should report SecretTargetsDisabled on existing synced Bundle after disabling secretTargets", func() {
			bundle := newBundle(bundleSecretDisable).
				WithInLineSource(testCertPEM1).
				WithSecretTarget(bundleTargetKey).
				Build()

			createBundleWithCleanup(ctx, bundle)

			By("verifying Secret target syncs while feature is enabled")
			err := waitForSecretTarget(ctx, bundleClient, bundleSecretDisable, testNS.Name, bundleTargetKey, testCertPEM1, highTimeout)
			Expect(err).ShouldNot(HaveOccurred())
			verifyBundleSynced(ctx, bundleSecretDisable)

			By("disabling secretTargets on TrustManager CR")
			Eventually(func() error {
				tm, err := trustManagerClient().Get(ctx, "cluster", metav1.GetOptions{})
				if err != nil {
					return err
				}
				tm.Spec.TrustManagerConfig.SecretTargets = v1alpha1.SecretTargetsConfig{
					Policy: v1alpha1.SecretTargetsPolicyDisabled,
				}
				_, err = trustManagerClient().Update(ctx, tm, metav1.UpdateOptions{})
				return err
			}, lowTimeout, fastPollInterval).Should(Succeed())

			waitForTrustManagerReady(ctx)

			By("verifying Bundle status transitions to Synced=False")
			err = waitForBundleCondition(ctx, bundleClient, bundleSecretDisable, trustapi.BundleConditionSynced, metav1.ConditionFalse, highTimeout)
			Expect(err).ShouldNot(HaveOccurred())
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
		AfterAll(func() {
			deleteTrustManager(ctx)
			// The operator does not delete managed ConfigMaps when the feature is
			// disabled or the CR is removed. Clean up explicitly so subsequent
			// tests that assert absence of this ConfigMap start from a clean state.
			_ = k8sClientSet.CoreV1().ConfigMaps(trustManagerNamespace).Delete(ctx, defaultCAPackageConfigMapName, metav1.DeleteOptions{})
		})

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

		It("should reconcile unintended package ConfigMap drift and maintain Bundle target integrity", func() {
			bundleName := "bundle-drift-unintended-" + randomStr(5)
			bundle := newBundle(bundleName).
				WithUseDefaultCAs().
				WithConfigMapTarget(bundleTargetKey).
				Build()

			createBundleWithCleanup(ctx, bundle)

			By("verifying Bundle target is synced with default CAs")
			Eventually(func(g Gomega) {
				cm, err := k8sClientSet.CoreV1().ConfigMaps(testNS.Name).Get(ctx, bundleName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				data, ok := cm.Data[bundleTargetKey]
				g.Expect(ok).Should(BeTrue())
				g.Expect(containsPEMCertificates(data)).Should(BeTrue())
			}, highTimeout, slowPollInterval).Should(Succeed())

			var originalPkgData string
			By("capturing original package ConfigMap data")
			Eventually(func(g Gomega) {
				cm, err := k8sClientSet.CoreV1().ConfigMaps(trustManagerNamespace).Get(ctx, defaultCAPackageConfigMapName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				originalPkgData = cm.Data["cert-manager-package-openshift.json"]
				g.Expect(originalPkgData).ShouldNot(BeEmpty())
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("tampering with the package ConfigMap data")
			cm, err := k8sClientSet.CoreV1().ConfigMaps(trustManagerNamespace).Get(ctx, defaultCAPackageConfigMapName, metav1.GetOptions{})
			Expect(err).ShouldNot(HaveOccurred())
			cm.Data["cert-manager-package-openshift.json"] = `{"name":"tampered","bundle":"bad","version":"0"}`
			_, err = k8sClientSet.CoreV1().ConfigMaps(trustManagerNamespace).Update(ctx, cm, metav1.UpdateOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			By("verifying operator restores the original package ConfigMap data")
			Eventually(func(g Gomega) {
				cm, err := k8sClientSet.CoreV1().ConfigMaps(trustManagerNamespace).Get(ctx, defaultCAPackageConfigMapName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(cm.Data["cert-manager-package-openshift.json"]).Should(Equal(originalPkgData))
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("verifying Bundle target still contains valid default CA certificates")
			Eventually(func(g Gomega) {
				cm, err := k8sClientSet.CoreV1().ConfigMaps(testNS.Name).Get(ctx, bundleName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				data, ok := cm.Data[bundleTargetKey]
				g.Expect(ok).Should(BeTrue())
				g.Expect(containsPEMCertificates(data)).Should(BeTrue(),
					"Bundle target should still contain valid PEM certificates after drift recovery")
			}, highTimeout, slowPollInterval).Should(Succeed())
		})

		It("should propagate CNO CA bundle update through to Bundle target ConfigMaps", func() {
			const openshiftConfigNS = "openshift-config"
			const userCABundleName = "user-ca-bundle"

			By("checking if cluster has external control plane (HyperShift) — Proxy is immutable there")
			infra, err := configClient.Infrastructures().Get(ctx, "cluster", metav1.GetOptions{})
			Expect(err).ShouldNot(HaveOccurred())
			if infra.Status.ControlPlaneTopology == configopenshiftv1.ExternalTopologyMode {
				Skip("Proxy/cluster is immutable on HostedCluster (external control plane)")
			}

			bundleName := "bundle-cno-drift-" + randomStr(5)
			bundle := newBundle(bundleName).
				WithUseDefaultCAs().
				WithConfigMapTarget(bundleTargetKey).
				Build()

			createBundleWithCleanup(ctx, bundle)

			By("verifying Bundle target is synced with default CAs")
			Eventually(func(g Gomega) {
				cm, err := k8sClientSet.CoreV1().ConfigMaps(testNS.Name).Get(ctx, bundleName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				data, ok := cm.Data[bundleTargetKey]
				g.Expect(ok).Should(BeTrue())
				g.Expect(containsPEMCertificates(data)).Should(BeTrue())
			}, highTimeout, slowPollInterval).Should(Succeed())

			// --- Capture baseline state ---

			var originalHash string
			By("reading original hash annotation from pod template")
			Eventually(func(g Gomega) {
				dep, err := k8sClientSet.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(dep.Spec.Template.Annotations).Should(HaveKey(defaultCAPackageHashAnnotation))
				originalHash = dep.Spec.Template.Annotations[defaultCAPackageHashAnnotation]
				g.Expect(originalHash).ShouldNot(BeEmpty())
			}, lowTimeout, fastPollInterval).Should(Succeed())

			var originalInjectedData string
			By("reading original CNO-injected CA bundle data")
			Eventually(func(g Gomega) {
				cm, err := k8sClientSet.CoreV1().ConfigMaps(operatorNamespace).Get(ctx, trustedCABundleConfigMapName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				originalInjectedData = cm.Data[trustedCABundleKey]
			}, lowTimeout, fastPollInterval).Should(Succeed())

			var originalTargetData string
			By("capturing initial Bundle target data")
			cm, err := k8sClientSet.CoreV1().ConfigMaps(testNS.Name).Get(ctx, bundleName, metav1.GetOptions{})
			Expect(err).ShouldNot(HaveOccurred())
			originalTargetData = cm.Data[bundleTargetKey]

			// --- Save original Proxy and user-ca-bundle state for cleanup ---

			By("saving original Proxy trustedCA and user-ca-bundle state")
			proxy, err := configClient.Proxies().Get(ctx, "cluster", metav1.GetOptions{})
			Expect(err).ShouldNot(HaveOccurred())
			originalTrustedCAName := proxy.Spec.TrustedCA.Name

			existingUserCA, userCAErr := k8sClientSet.CoreV1().ConfigMaps(openshiftConfigNS).Get(ctx, userCABundleName, metav1.GetOptions{})
			userCABundleExisted := userCAErr == nil
			var originalUserCAData string
			if userCABundleExisted {
				originalUserCAData = existingUserCA.Data["ca-bundle.crt"]
			}

			DeferCleanup(func() {
				By("[cleanup] restoring original Proxy trustedCA reference")
				if originalTrustedCAName != userCABundleName {
					Eventually(func() error {
						p, err := configClient.Proxies().Get(ctx, "cluster", metav1.GetOptions{})
						if err != nil {
							return err
						}
						p.Spec.TrustedCA.Name = originalTrustedCAName
						_, err = configClient.Proxies().Update(ctx, p, metav1.UpdateOptions{})
						return err
					}, lowTimeout, fastPollInterval).Should(Succeed())
				}

				By("[cleanup] restoring original user-ca-bundle ConfigMap")
				if userCABundleExisted {
					Eventually(func() error {
						cm, err := k8sClientSet.CoreV1().ConfigMaps(openshiftConfigNS).Get(ctx, userCABundleName, metav1.GetOptions{})
						if err != nil {
							return err
						}
						cm.Data["ca-bundle.crt"] = originalUserCAData
						_, err = k8sClientSet.CoreV1().ConfigMaps(openshiftConfigNS).Update(ctx, cm, metav1.UpdateOptions{})
						return err
					}, lowTimeout, fastPollInterval).Should(Succeed())
				} else {
					_ = k8sClientSet.CoreV1().ConfigMaps(openshiftConfigNS).Delete(ctx, userCABundleName, metav1.DeleteOptions{})
				}

				By("[cleanup] waiting for CNO to restore the injected CA bundle")
				Eventually(func(g Gomega) {
					cm, err := k8sClientSet.CoreV1().ConfigMaps(operatorNamespace).Get(ctx, trustedCABundleConfigMapName, metav1.GetOptions{})
					g.Expect(err).ShouldNot(HaveOccurred())
					g.Expect(cm.Data[trustedCABundleKey]).Should(Equal(originalInjectedData),
						"injected CA bundle should be restored to original state")
				}, highTimeout, slowPollInterval).Should(Succeed())
			})

			// --- Modify the cluster-wide trust bundle via Proxy CR ---

			testCACert := testutils.GenerateCertificate(
				"e2e-proxy-test-ca",
				[]string{"cert-manager-operator-e2e"},
				func(cert *x509.Certificate) {
					cert.IsCA = true
					cert.KeyUsage |= x509.KeyUsageCertSign
				},
			)

			By("creating/updating user-ca-bundle ConfigMap in openshift-config namespace")
			if userCABundleExisted {
				Eventually(func() error {
					cm, err := k8sClientSet.CoreV1().ConfigMaps(openshiftConfigNS).Get(ctx, userCABundleName, metav1.GetOptions{})
					if err != nil {
						return err
					}
					cm.Data["ca-bundle.crt"] = cm.Data["ca-bundle.crt"] + "\n" + testCACert
					_, err = k8sClientSet.CoreV1().ConfigMaps(openshiftConfigNS).Update(ctx, cm, metav1.UpdateOptions{})
					return err
				}, lowTimeout, fastPollInterval).Should(Succeed())
			} else {
				_, err := k8sClientSet.CoreV1().ConfigMaps(openshiftConfigNS).Create(ctx, &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      userCABundleName,
						Namespace: openshiftConfigNS,
					},
					Data: map[string]string{
						"ca-bundle.crt": testCACert,
					},
				}, metav1.CreateOptions{})
				Expect(err).ShouldNot(HaveOccurred())
			}

			if originalTrustedCAName != userCABundleName {
				By("updating Proxy/cluster to reference user-ca-bundle")
				Eventually(func() error {
					p, err := configClient.Proxies().Get(ctx, "cluster", metav1.GetOptions{})
					if err != nil {
						return err
					}
					p.Spec.TrustedCA.Name = userCABundleName
					_, err = configClient.Proxies().Update(ctx, p, metav1.UpdateOptions{})
					return err
				}, lowTimeout, fastPollInterval).Should(Succeed())
			}

			// --- Wait for CNO propagation ---

			By("waiting for CNO to propagate updated CA bundle to operator namespace")
			Eventually(func(g Gomega) {
				cm, err := k8sClientSet.CoreV1().ConfigMaps(operatorNamespace).Get(ctx, trustedCABundleConfigMapName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(cm.Data[trustedCABundleKey]).ShouldNot(Equal(originalInjectedData),
					"CNO should have updated the injected CA bundle")
			}, highTimeout, slowPollInterval).Should(Succeed())

			// --- Verify operator propagated the change ---

			By("verifying pod template hash annotation changed (triggers rolling restart)")
			Eventually(func(g Gomega) {
				dep, err := k8sClientSet.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(dep.Spec.Template.Annotations).Should(HaveKey(defaultCAPackageHashAnnotation))
				g.Expect(dep.Spec.Template.Annotations[defaultCAPackageHashAnnotation]).ShouldNot(Equal(originalHash),
					"hash annotation should have changed after CA bundle update")
			}, highTimeout, fastPollInterval).Should(Succeed())

			waitForTrustManagerReady(ctx)

			// --- Verify Bundle targets reflect the updated CA bundle ---

			By("verifying Bundle target ConfigMap reflects the updated CA bundle with injected test cert")
			Eventually(func(g Gomega) {
				cm, err := k8sClientSet.CoreV1().ConfigMaps(testNS.Name).Get(ctx, bundleName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				data := cm.Data[bundleTargetKey]
				g.Expect(data).ShouldNot(Equal(originalTargetData),
					"target ConfigMap data should have changed after CA bundle update")
				g.Expect(data).Should(ContainSubstring(testCACert),
					"target ConfigMap should contain the injected test CA certificate")
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
		AfterAll(func() {
			deleteTrustManager(ctx)
			// The operator does not delete managed ConfigMaps when the feature is
			// disabled or the CR is removed. Clean up explicitly so subsequent
			// tests that assert absence of this ConfigMap start from a clean state.
			_ = k8sClientSet.CoreV1().ConfigMaps(trustManagerNamespace).Delete(ctx, defaultCAPackageConfigMapName, metav1.DeleteOptions{})
		})

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

		It("should sync Secret source from custom trust namespace to target", func() {
			bundleName := "bundle-secret-custom-ns-" + randomStr(5)
			sourceSecretName := "src-secret-custom-ns-" + randomStr(5)

			By(fmt.Sprintf("creating source Secret in custom trust namespace %q", customTrustNS.Name))
			createSourceSecret(ctx, customTrustNS.Name, sourceSecretName, bundleSourceKey, testCertPEM1)

			bundle := newBundle(bundleName).
				WithSecretSource(sourceSecretName, bundleSourceKey).
				WithConfigMapTarget(bundleTargetKey).
				Build()

			createBundleWithCleanup(ctx, bundle)

			By("verifying ConfigMap target is synced in test namespace")
			err := waitForConfigMapTarget(ctx, bundleClient, bundleName, testNS.Name, bundleTargetKey, testCertPEM1, highTimeout)
			Expect(err).ShouldNot(HaveOccurred())

			verifyBundleSynced(ctx, bundleName)
		})

		It("should not sync sources from default namespace when custom trust namespace is configured", func() {
			bundleName := "bundle-default-ns-miss-" + randomStr(5)
			sourceCMName := "src-default-ns-miss-" + randomStr(5)
			sourceSecretName := "src-secret-default-miss-" + randomStr(5)

			By(fmt.Sprintf("creating source ConfigMap and Secret in default namespace %q (not the custom trust namespace %q)", trustManagerNamespace, customTrustNS.Name))
			createSourceConfigMap(ctx, trustManagerNamespace, sourceCMName, bundleSourceKey, testCertPEM1)
			createSourceSecret(ctx, trustManagerNamespace, sourceSecretName, bundleSourceKey, testCertPEM2)

			bundle := newBundle(bundleName).
				WithConfigMapSource(sourceCMName, bundleSourceKey).
				WithSecretSource(sourceSecretName, bundleSourceKey).
				WithConfigMapTarget(bundleTargetKey).
				Build()

			createBundleWithCleanup(ctx, bundle)

			By("verifying Bundle does not reach Synced=True because sources are not in custom trust namespace")
			verifyBundleNeverSynced(ctx, bundleName)

			By("verifying no target ConfigMap is created in test namespace")
			_, err := k8sClientSet.CoreV1().ConfigMaps(testNS.Name).Get(ctx, bundleName, metav1.GetOptions{})
			Expect(apierrors.IsNotFound(err)).Should(BeTrue())
		})
	})

	// ===== Group 6: FilterExpiredCertificates =====
	Context("with FilterExpiredCertificates enabled", Ordered, func() {
		var (
			filterBundleName string
			sourceCMName     string
		)

		BeforeAll(func() {
			createTrustManager(ctx, newTrustManagerCR().
				WithFilterExpiredCertificates(v1alpha1.FilterExpiredCertificatesPolicyEnabled))

			sourceCMName = "filter-src-cm-" + randomStr(5)
			filterBundleName = "bundle-filter-expired-" + randomStr(5)
			combinedPEM := testCertPEM1 + expiredCertPEM

			By("creating source ConfigMap with valid + expired certs in trust namespace")
			createSourceConfigMap(ctx, trustManagerNamespace, sourceCMName, bundleSourceKey, combinedPEM)

			bundle := newBundle(filterBundleName).
				WithConfigMapSource(sourceCMName, bundleSourceKey).
				WithConfigMapTarget(bundleTargetKey).
				Build()

			createBundleWithCleanup(ctx, bundle)
		})
		AfterAll(func() { deleteTrustManager(ctx) })

		It("should exclude expired certificates from ConfigMap target when using ConfigMap source", func() {
			By("verifying target contains the valid certificate")
			err := waitForConfigMapTarget(ctx, bundleClient, filterBundleName, testNS.Name, bundleTargetKey, testCertPEM1, highTimeout)
			Expect(err).ShouldNot(HaveOccurred())

			By("verifying target does NOT contain the expired certificate")
			Eventually(func(g Gomega) {
				cm, err := k8sClientSet.CoreV1().ConfigMaps(testNS.Name).Get(ctx, filterBundleName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				data := cm.Data[bundleTargetKey]
				g.Expect(strings.Contains(data, strings.TrimSpace(testCertPEM1))).Should(BeTrue(), "should contain valid cert")
				g.Expect(strings.Contains(data, strings.TrimSpace(expiredCertPEM))).Should(BeFalse(), "should not contain expired cert")
			}, highTimeout, fastPollInterval).Should(Succeed())

			verifyBundleSynced(ctx, filterBundleName)
		})

		It("should re-sync same Bundle with expired certs included after disabling filter", func() {
			By("disabling filterExpiredCertificates on TrustManager CR")
			Eventually(func() error {
				tm, err := trustManagerClient().Get(ctx, "cluster", metav1.GetOptions{})
				if err != nil {
					return err
				}
				tm.Spec.TrustManagerConfig.FilterExpiredCertificates = v1alpha1.FilterExpiredCertificatesPolicyDisabled
				_, err = trustManagerClient().Update(ctx, tm, metav1.UpdateOptions{})
				return err
			}, lowTimeout, fastPollInterval).Should(Succeed())

			waitForTrustManagerReady(ctx)

			By("verifying the same Bundle's target now includes the expired certificate")
			Eventually(func(g Gomega) {
				cm, err := k8sClientSet.CoreV1().ConfigMaps(testNS.Name).Get(ctx, filterBundleName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				data := cm.Data[bundleTargetKey]
				g.Expect(strings.Contains(data, strings.TrimSpace(testCertPEM1))).Should(BeTrue(), "should contain valid cert")
				g.Expect(strings.Contains(data, strings.TrimSpace(expiredCertPEM))).Should(BeTrue(), "should contain expired cert after disabling filter")
			}, highTimeout, fastPollInterval).Should(Succeed())

			verifyBundleSynced(ctx, filterBundleName)
		})
	})
})
