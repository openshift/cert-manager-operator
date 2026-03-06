//go:build e2e
// +build e2e

package e2e

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	operatorclientv1alpha1 "github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned/typed/operator/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

const (
	trustManagerNamespace          = "cert-manager"
	trustManagerServiceAccountName = "trust-manager"
	trustManagerCommonName         = "cert-manager-trust-manager"
)

var _ = Describe("TrustManager", Ordered, Label("Feature:TrustManager"), func() {
	ctx := context.TODO()
	var clientset *kubernetes.Clientset

	trustManagerClient := func() operatorclientv1alpha1.TrustManagerInterface {
		return certmanageroperatorclient.OperatorV1alpha1().TrustManagers()
	}

	waitForTrustManagerReady := func() v1alpha1.TrustManagerStatus {
		By("waiting for TrustManager CR to be ready")
		status, err := pollTillTrustManagerAvailable(ctx, trustManagerClient(), "cluster")
		Expect(err).Should(BeNil())

		return status
	}

	BeforeAll(func() {
		var err error
		clientset, err = kubernetes.NewForConfig(cfg)
		Expect(err).Should(BeNil())

		By("enabling TrustManager feature gate via subscription")
		err = patchSubscriptionWithEnvVars(ctx, loader, map[string]string{
			"UNSUPPORTED_ADDON_FEATURES": "TrustManager=true",
		})
		Expect(err).NotTo(HaveOccurred())

		By("waiting for operator deployment to rollout with TrustManager feature enabled")
		err = waitForDeploymentEnvVarAndRollout(ctx, operatorNamespace, operatorDeploymentName, "UNSUPPORTED_ADDON_FEATURES", "TrustManager=true", lowTimeout)
		Expect(err).NotTo(HaveOccurred())
	})

	BeforeEach(func() {
		By("waiting for operator status to become available")
		err := VerifyHealthyOperatorConditions(certmanageroperatorclient.OperatorV1alpha1())
		Expect(err).NotTo(HaveOccurred(), "Operator is expected to be available")
	})

	AfterEach(func() {
		By("cleaning up TrustManager CR if it exists")
		_ = trustManagerClient().Delete(ctx, "cluster", metav1.DeleteOptions{})

		By("waiting for TrustManager CR to be deleted")
		Eventually(func() bool {
			_, err := trustManagerClient().Get(ctx, "cluster", metav1.GetOptions{})
			return apierrors.IsNotFound(err)
		}, lowTimeout, fastPollInterval).Should(BeTrue())
	})

	// Note: Currently, the TrustManager controller only reconciles ServiceAccounts.
	// Additional tests for Deployments, RBAC, ConfigMaps, etc. should be added
	// as those resources are implemented.

	Context("basic reconciliation", func() {
		It("should create TrustManager CR and reconcile ServiceAccount", func() {
			By("creating TrustManager CR with default settings")
			_, err := trustManagerClient().Create(ctx, &v1alpha1.TrustManager{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: v1alpha1.TrustManagerSpec{
					TrustManagerConfig: v1alpha1.TrustManagerConfig{},
				},
			}, metav1.CreateOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			status := waitForTrustManagerReady()
			Expect(status.TrustNamespace).Should(Equal("cert-manager"))

			By("verifying ServiceAccount is created with correct labels")
			sa, err := clientset.CoreV1().ServiceAccounts(trustManagerNamespace).Get(ctx, trustManagerServiceAccountName, metav1.GetOptions{})
			Expect(err).ShouldNot(HaveOccurred())
			verifyTrustManagerManagedLabels(sa.Labels)

			By("modifying ServiceAccount labels externally")
			sa.Labels["app.kubernetes.io/instance"] = "modified-value"
			_, err = clientset.CoreV1().ServiceAccounts(trustManagerNamespace).Update(ctx, sa, metav1.UpdateOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			By("verifying controller reconciles and restores correct labels")
			Eventually(func(g Gomega) {
				sa, err := clientset.CoreV1().ServiceAccounts(trustManagerNamespace).Get(ctx, trustManagerServiceAccountName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(sa.Labels).Should(HaveKeyWithValue("app.kubernetes.io/instance", trustManagerCommonName))
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("deleting ServiceAccount externally")
			err = clientset.CoreV1().ServiceAccounts(trustManagerNamespace).Delete(ctx, trustManagerServiceAccountName, metav1.DeleteOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			By("verifying controller reconciles and recreates ServiceAccount")
			Eventually(func(g Gomega) {
				sa, err := clientset.CoreV1().ServiceAccounts(trustManagerNamespace).Get(ctx, trustManagerServiceAccountName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				verifyTrustManagerManagedLabels(sa.Labels)
			}, lowTimeout, fastPollInterval).Should(Succeed())
		})

		It("should reflect spec values in status", func() {
			customNamespace := "custom-trust-ns"

			By("creating custom trust namespace")
			_, err := clientset.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: customNamespace},
			}, metav1.CreateOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			defer func() {
				By("cleaning up custom trust namespace")
				_ = clientset.CoreV1().Namespaces().Delete(ctx, customNamespace, metav1.DeleteOptions{})
			}()

			By("creating TrustManager CR with custom configuration")
			_, err = trustManagerClient().Create(ctx, &v1alpha1.TrustManager{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: v1alpha1.TrustManagerSpec{
					TrustManagerConfig: v1alpha1.TrustManagerConfig{
						TrustNamespace: customNamespace,
						SecretTargets: v1alpha1.SecretTargetsConfig{
							Policy:            v1alpha1.SecretTargetsPolicyCustom,
							AuthorizedSecrets: []string{"secret1", "secret2"},
						},
						DefaultCAPackage: v1alpha1.DefaultCAPackageConfig{
							Policy: v1alpha1.DefaultCAPackagePolicyEnabled,
						},
						FilterExpiredCertificates: v1alpha1.FilterExpiredCertificatesPolicyEnabled,
					},
				},
			}, metav1.CreateOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			status := waitForTrustManagerReady()

			By("verifying spec values are reflected in status")
			// Expect(status.TrustManagerImage).ShouldNot(BeEmpty()) // TODO: uncomment this when we will implement deployment resource
			Expect(status.TrustNamespace).Should(Equal(customNamespace))
			Expect(status.SecretTargetsPolicy).Should(Equal(v1alpha1.SecretTargetsPolicyCustom))
			Expect(status.DefaultCAPackagePolicy).Should(Equal(v1alpha1.DefaultCAPackagePolicyEnabled))
			Expect(status.FilterExpiredCertificatesPolicy).Should(Equal(v1alpha1.FilterExpiredCertificatesPolicyEnabled))
		})
	})

	Context("singleton validation", func() {
		It("should reject TrustManager with name other than 'cluster'", func() {
			By("attempting to create TrustManager with invalid name")
			_, err := trustManagerClient().Create(ctx, &v1alpha1.TrustManager{
				ObjectMeta: metav1.ObjectMeta{Name: "invalid-name"},
				Spec: v1alpha1.TrustManagerSpec{
					TrustManagerConfig: v1alpha1.TrustManagerConfig{},
				},
			}, metav1.CreateOptions{})
			Expect(err).Should(HaveOccurred())
			// CEL validation error: TrustManager is a singleton, .metadata.name must be 'cluster'
			Expect(err.Error()).Should(ContainSubstring("TrustManager is a singleton"))
			Expect(err.Error()).Should(ContainSubstring(".metadata.name must be 'cluster'"))
		})
	})
})

// pollTillTrustManagerAvailable polls the TrustManager object and returns its status
// once the TrustManager is available, otherwise returns a time-out error
func pollTillTrustManagerAvailable(ctx context.Context, client operatorclientv1alpha1.TrustManagerInterface, trustManagerName string) (v1alpha1.TrustManagerStatus, error) {
	var trustManagerStatus v1alpha1.TrustManagerStatus

	err := wait.PollUntilContextTimeout(ctx, slowPollInterval, highTimeout, true, func(context.Context) (bool, error) {
		trustManager, err := client.Get(ctx, trustManagerName, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		trustManagerStatus = trustManager.Status

		// Check ready condition
		readyCondition := meta.FindStatusCondition(trustManagerStatus.Conditions, v1alpha1.Ready)
		if readyCondition == nil {
			return false, nil
		}

		// Check for degraded condition
		degradedCondition := meta.FindStatusCondition(trustManagerStatus.Conditions, v1alpha1.Degraded)
		if degradedCondition != nil && degradedCondition.Status == metav1.ConditionTrue {
			return false, nil // Return false to keep polling, not an error
		}

		return readyCondition.Status == metav1.ConditionTrue, nil
	})

	return trustManagerStatus, err
}

// verifyTrustManagerManagedLabels verifies that the resource has all the expected
// labels for resources managed by the TrustManager controller.
func verifyTrustManagerManagedLabels(labels map[string]string) {
	Expect(labels).Should(HaveKeyWithValue("app", trustManagerCommonName))
	Expect(labels).Should(HaveKeyWithValue("app.kubernetes.io/name", trustManagerCommonName))
	Expect(labels).Should(HaveKeyWithValue("app.kubernetes.io/instance", trustManagerCommonName))
	Expect(labels).Should(HaveKeyWithValue("app.kubernetes.io/managed-by", "cert-manager-operator"))
	Expect(labels).Should(HaveKeyWithValue("app.kubernetes.io/part-of", "cert-manager-operator"))
	Expect(labels).Should(HaveKey("app.kubernetes.io/version")) // Version comes from env var
}
