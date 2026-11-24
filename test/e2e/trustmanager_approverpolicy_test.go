//go:build e2e

package e2e

import (
	"context"
	"time"

	policyv1alpha1 "github.com/cert-manager/approver-policy/pkg/apis/policy/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

var _ = Describe("TrustManager approver-policy integration", Serial, Ordered,
	Label("Platform:Generic", "Feature:TrustManager", "Feature:ApproverPolicyManager", "TechPreview"), func() {
		var (
			ctx                      = context.Background()
			client                   crclient.Client
			originalGates            string
			originalOperatorLogLevel string
			originalSettingsCaptured bool
		)

		BeforeAll(func() {
			By("verifying no pre-existing TrustManager singleton")
			_, err := trustManagerClient().Get(ctx, "cluster", metav1.GetOptions{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue(), "integration suite requires no pre-existing TrustManager")

			By("capturing the Subscription settings before enabling either feature gate")
			originalGates, err = getSubscriptionEnvVar(ctx, loader, "UNSUPPORTED_ADDON_FEATURES")
			Expect(err).NotTo(HaveOccurred())
			originalOperatorLogLevel, err = getSubscriptionEnvVar(ctx, loader, "OPERATOR_LOG_LEVEL")
			Expect(err).NotTo(HaveOccurred())
			originalSettingsCaptured = true

			// Preserve the snapshot for AfterAll even if a setup helper fails
			// after changing the Subscription but before returning the client.
			var intermediateGates, intermediateOperatorLogLevel string
			client = approverPolicyManagerBeforeAll(ctx, &intermediateGates, &intermediateOperatorLogLevel, true)
			Expect(policyv1alpha1.AddToScheme(client.Scheme())).To(Succeed())

			enableOperatorFeatureGateForSuite(ctx, loader, "TrustManager", true, true, &intermediateGates, &intermediateOperatorLogLevel)
		})

		AfterAll(func() {
			if originalSettingsCaptured {
				approverPolicyManagerAfterAll(ctx, &originalGates, &originalOperatorLogLevel)
			}
		})

		BeforeEach(func() {
			By("waiting for operator status to become available")
			Expect(VerifyHealthyOperatorConditions(certmanageroperatorclient.OperatorV1alpha1())).To(Succeed())

			By("removing webhook certificate fixtures from the previous spec")
			// TrustManager's existing cleanup leaves its Certificate and Secret behind.
			// Remove them while the TrustManager CR is absent so issuance cannot reuse
			// a certificate from an earlier spec.
			Expect(crclient.IgnoreNotFound(certmanagerClient.CertmanagerV1().Certificates(operandNamespace).Delete(ctx, trustManagerCertificateName, metav1.DeleteOptions{}))).To(Succeed())
			Expect(crclient.IgnoreNotFound(k8sClientSet.CoreV1().Secrets(operandNamespace).Delete(ctx, trustManagerTLSSecretName, metav1.DeleteOptions{}))).To(Succeed())
		})

		AfterEach(func() {
			trustManagerApproverPolicyAfterEach(ctx, client)
		})

		Context("installation order", func() {
			It("should renew the webhook certificate when trust-manager is installed before approver-policy", func() {
				By("issuing the webhook certificate with the built-in approver")
				createTrustManager(ctx, newTrustManagerCR())
				Expect(waitForCertificateReadiness(ctx, trustManagerCertificateName, operandNamespace)).To(Succeed())
				verifyTrustManagerPolicyResources(ctx, Default, client, false)

				By("installing approver-policy and waiting for the automatic handoff")
				createApproverPolicyManager(ctx, newApproverPolicyManagerCR())
				waitForAutoApproverHandoff(ctx)
				Eventually(func(g Gomega) { verifyTrustManagerPolicyResources(ctx, g, client, true) }, highTimeout, fastPollInterval).Should(Succeed())

				By("requesting a fresh webhook certificate after the handoff")
				previousSecret, err := k8sClientSet.CoreV1().Secrets(operandNamespace).Get(ctx, trustManagerTLSSecretName, metav1.GetOptions{})
				Expect(err).NotTo(HaveOccurred())
				previousRequests := trustManagerCertificateRequestUIDs(ctx)
				Expect(k8sClientSet.CoreV1().Secrets(operandNamespace).Delete(ctx, trustManagerTLSSecretName, metav1.DeleteOptions{})).To(Succeed())
				request := waitForTrustManagerPolicyApproval(ctx, previousRequests)
				Eventually(func(g Gomega) {
					secret, err := k8sClientSet.CoreV1().Secrets(operandNamespace).Get(ctx, trustManagerTLSSecretName, metav1.GetOptions{})
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(secret.UID).NotTo(Equal(previousSecret.UID), "the TLS Secret must be recreated")
					g.Expect(secret.Data[corev1.TLSCertKey]).To(Equal(request.Status.Certificate), "the replacement Secret must contain the new request's certificate")
				}, highTimeout, fastPollInterval).Should(Succeed())
				Expect(waitForCertificateReadiness(ctx, trustManagerCertificateName, operandNamespace)).To(Succeed())
				Expect(pollTillDeploymentAvailable(ctx, k8sClientSet, operandNamespace, trustManagerDeploymentName)).To(Succeed())

				By("recreating deleted integration resources without editing TrustManager")
				verifyTrustManagerPolicyResourceRecreation(ctx, client)
			})

			It("should issue the webhook certificate when approver-policy is installed before trust-manager", func() {
				previousRequests := trustManagerCertificateRequestUIDs(ctx)

				By("installing approver-policy and waiting for the automatic handoff")
				createApproverPolicyManager(ctx, newApproverPolicyManagerCR())
				waitForAutoApproverHandoff(ctx)

				By("installing trust-manager with automatic policy management")
				createTrustManager(ctx, newTrustManagerCR())
				Eventually(func(g Gomega) { verifyTrustManagerPolicyResources(ctx, g, client, true) }, highTimeout, fastPollInterval).Should(Succeed())

				By("waiting for the webhook certificate to be approved and signed")
				waitForTrustManagerPolicyApproval(ctx, previousRequests)
				Expect(waitForCertificateReadiness(ctx, trustManagerCertificateName, operandNamespace)).To(Succeed())
				Expect(pollTillDeploymentAvailable(ctx, k8sClientSet, operandNamespace, trustManagerDeploymentName)).To(Succeed())

				By("recreating deleted integration resources without editing TrustManager")
				verifyTrustManagerPolicyResourceRecreation(ctx, client)
			})
		})

		Context("management state", func() {
			It("should leave approval to the administrator while Unmanaged and resume when Managed", func() {
				By("installing approver-policy and waiting for the automatic handoff")
				createApproverPolicyManager(ctx, newApproverPolicyManagerCR())
				waitForAutoApproverHandoff(ctx)
				previousRequests := trustManagerCertificateRequestUIDs(ctx)

				By("creating TrustManager with Unmanaged approval")
				tm := newTrustManagerCR().Build()
				tm.Spec.TrustManagerConfig.WebhookTLS.ApproverPolicy.ManagementState = v1alpha1.ApproverPolicyManagementStateUnmanaged
				_, err := trustManagerClient().Create(ctx, tm, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())
				request := waitForTrustManagerCertificateRequest(ctx, previousRequests)

				By("verifying the new request remains pending without degrading TrustManager")
				Consistently(func(g Gomega) {
					verifyTrustManagerPolicyResources(ctx, g, client, false)
					current, err := certmanagerClient.CertmanagerV1().CertificateRequests(operandNamespace).Get(ctx, request.Name, metav1.GetOptions{})
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(certificateRequestHasDecision(current)).To(BeFalse())
					currentTM, err := trustManagerClient().Get(ctx, "cluster", metav1.GetOptions{})
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(meta.IsStatusConditionTrue(currentTM.Status.Conditions, v1alpha1.Degraded)).To(BeFalse())
				}, 5*time.Second, fastPollInterval).Should(Succeed())

				By("resuming automatic approval without recreating either operand")
				setTrustManagerApproverPolicyState(ctx, v1alpha1.ApproverPolicyManagementStateManaged)
				waitForTrustManagerPolicyApproval(ctx, previousRequests)
				Expect(waitForCertificateReadiness(ctx, trustManagerCertificateName, operandNamespace)).To(Succeed())

				By("leaving existing policies untouched after switching back to Unmanaged")
				setTrustManagerApproverPolicyState(ctx, v1alpha1.ApproverPolicyManagementStateUnmanaged)
				Expect(approverPolicyClient().Delete(ctx, "cluster", metav1.DeleteOptions{})).To(Succeed())
				Eventually(func(g Gomega) { verifyAutoApproverState(ctx, g, false) }, highTimeout, fastPollInterval).Should(Succeed())
				Consistently(func(g Gomega) { verifyTrustManagerPolicyResources(ctx, g, client, true) }, 5*time.Second, fastPollInterval).Should(Succeed())

				By("resuming Managed cleanup once built-in approval is active")
				setTrustManagerApproverPolicyState(ctx, v1alpha1.ApproverPolicyManagementStateManaged)
				Eventually(func(g Gomega) { verifyTrustManagerPolicyResources(ctx, g, client, false) }, highTimeout, fastPollInterval).Should(Succeed())
			})
		})
	})
