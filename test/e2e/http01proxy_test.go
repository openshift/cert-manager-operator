//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

var _ = Describe("HTTP01 Proxy [apigroup:operator.openshift.io]", Label("Platform:Generic"), Ordered, func() {
	var (
		ctx                                context.Context
		originalUnsupportedAddonFeatures   string
		unsupportedAddonFeaturesEnvVarName = "UNSUPPORTED_ADDON_FEATURES"
	)

	BeforeAll(func() {
		ctx = context.Background()

		By("capturing original UNSUPPORTED_ADDON_FEATURES value")
		original, err := getSubscriptionEnvVar(ctx, loader, unsupportedAddonFeaturesEnvVarName)
		Expect(err).NotTo(HaveOccurred(), "failed to get original UNSUPPORTED_ADDON_FEATURES")
		originalUnsupportedAddonFeatures = original

		By("enabling HTTP01Proxy feature gate via subscription env var")
		err = patchSubscriptionWithEnvVars(ctx, loader, map[string]string{
			unsupportedAddonFeaturesEnvVarName: "HTTP01Proxy=true",
		})
		Expect(err).NotTo(HaveOccurred(), "failed to enable HTTP01Proxy feature gate")

		By("waiting for operator to restart with feature gate enabled")
		err = waitForDeploymentEnvVarAndRollout(ctx, operatorNamespace, operatorDeploymentName,
			unsupportedAddonFeaturesEnvVarName, "HTTP01Proxy=true", highTimeout)
		Expect(err).NotTo(HaveOccurred(), "operator did not roll out after enabling HTTP01Proxy feature gate")

		By("waiting for operator to become available after restart")
		err = VerifyHealthyOperatorConditions(certmanageroperatorclient.OperatorV1alpha1())
		Expect(err).NotTo(HaveOccurred(), "operator not healthy after enabling HTTP01Proxy feature gate")
	})

	AfterAll(func() {
		By("restoring original UNSUPPORTED_ADDON_FEATURES value")
		err := patchSubscriptionWithEnvVars(ctx, loader, map[string]string{
			unsupportedAddonFeaturesEnvVarName: originalUnsupportedAddonFeatures,
		})
		if err != nil {
			fmt.Fprintf(GinkgoWriter, "failed to restore UNSUPPORTED_ADDON_FEATURES during cleanup: %v\n", err)
			return
		}

		By("waiting for operator to roll out after restoring feature gates")
		if originalUnsupportedAddonFeatures == "" {
			err = waitForDeploymentEnvVarRemovedAndRollout(ctx, operatorNamespace, operatorDeploymentName,
				unsupportedAddonFeaturesEnvVarName, highTimeout)
		} else {
			err = waitForDeploymentEnvVarAndRollout(ctx, operatorNamespace, operatorDeploymentName,
				unsupportedAddonFeaturesEnvVarName, originalUnsupportedAddonFeatures, highTimeout)
		}
		if err != nil {
			fmt.Fprintf(GinkgoWriter, "operator did not roll out after restoring feature gates: %v\n", err)
		}
	})

	BeforeEach(func() {
		By("waiting for operator status to become available")
		err := VerifyHealthyOperatorConditions(certmanageroperatorclient.OperatorV1alpha1())
		Expect(err).NotTo(HaveOccurred(), "operator is expected to be available")
	})

	Context("on a non-baremetal cluster", func() {
		It("should reject HTTP01Proxy CR with unsupported platform condition", func() {
			proxy := &v1alpha1.HTTP01Proxy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default",
					Namespace: operatorNamespace,
				},
				Spec: v1alpha1.HTTP01ProxySpec{
					Mode: v1alpha1.HTTP01ProxyModeDefault,
				},
			}

			By("creating HTTP01Proxy CR")
			_, err := certmanageroperatorclient.OperatorV1alpha1().HTTP01Proxies(operatorNamespace).Create(ctx, proxy, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create HTTP01Proxy CR")

			DeferCleanup(func(ctx context.Context) {
				By("deleting HTTP01Proxy CR")
				err := certmanageroperatorclient.OperatorV1alpha1().HTTP01Proxies(operatorNamespace).Delete(ctx, "default", metav1.DeleteOptions{})
				if err != nil && !apierrors.IsNotFound(err) {
					fmt.Fprintf(GinkgoWriter, "failed to delete HTTP01Proxy CR during cleanup: %v\n", err)
				}

				By("waiting for HTTP01Proxy CR to be fully removed")
				_ = wait.PollUntilContextTimeout(ctx, fastPollInterval, lowTimeout, true, func(ctx context.Context) (bool, error) {
					_, getErr := certmanageroperatorclient.OperatorV1alpha1().HTTP01Proxies(operatorNamespace).Get(ctx, "default", metav1.GetOptions{})
					return apierrors.IsNotFound(getErr), nil
				})
			})

			By("waiting for Degraded=True and Ready=False conditions")
			Eventually(func(g Gomega) {
				fetched, getErr := certmanageroperatorclient.OperatorV1alpha1().HTTP01Proxies(operatorNamespace).Get(ctx, "default", metav1.GetOptions{})
				g.Expect(getErr).NotTo(HaveOccurred())

				degraded := meta.FindStatusCondition(fetched.Status.Conditions, v1alpha1.Degraded)
				g.Expect(degraded).NotTo(BeNil(), "Degraded condition not found on HTTP01Proxy")
				g.Expect(degraded.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(degraded.Reason).To(Equal(v1alpha1.ReasonFailed))
				g.Expect(degraded.Message).To(ContainSubstring("not supported"))

				ready := meta.FindStatusCondition(fetched.Status.Conditions, v1alpha1.Ready)
				g.Expect(ready).NotTo(BeNil(), "Ready condition not found on HTTP01Proxy")
				g.Expect(ready.Status).To(Equal(metav1.ConditionFalse))
			}, lowTimeout, fastPollInterval).Should(Succeed())
		})
	})
})
