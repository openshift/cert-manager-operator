//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"fmt"
	"slices"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	operatorclientv1alpha1 "github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned/typed/operator/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

const (
	trustManagerNamespace          = "cert-manager"
	trustManagerServiceAccountName = "trust-manager"
	trustManagerCommonName         = "cert-manager-trust-manager"

	trustManagerDeploymentName     = "trust-manager"
	trustManagerServiceName        = "trust-manager"
	trustManagerMetricsServiceName = "trust-manager-metrics"

	trustManagerClusterRoleName        = "trust-manager"
	trustManagerClusterRoleBindingName = "trust-manager"
	trustManagerRoleName               = "trust-manager"
	trustManagerRoleBindingName        = "trust-manager"

	trustManagerLeaderElectionRoleName        = "trust-manager:leaderelection"
	trustManagerLeaderElectionRoleBindingName = "trust-manager:leaderelection"

	trustManagerIssuerName      = "trust-manager"
	trustManagerCertificateName = "trust-manager"
	trustManagerTLSSecretName   = "trust-manager-tls"

	trustManagerWebhookConfigName = "trust-manager"

	// DefaultCAPackage constants
	defaultCAPackageConfigMapName  = "trust-manager-default-ca-package"
	defaultCAPackageVolumeName     = "packages"
	defaultCAPackageMountPath      = "/packages"
	defaultCAPackageLocation       = defaultCAPackageMountPath + "/cert-manager-package-openshift.json"
	defaultCAPackageHashAnnotation = "operator.openshift.io/default-ca-package-hash"

	trustedCABundleConfigMapName = "cert-manager-operator-trusted-ca-bundle"
	trustedCABundleKey           = "ca-bundle.crt"
)

var _ = Describe("TrustManager", Ordered, Label("Platform:Generic", "Feature:TrustManager"), func() {
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

	createTrustManager := func(b *trustManagerCRBuilder) {
		_, err := trustManagerClient().Create(ctx, b.Build(), metav1.CreateOptions{})
		Expect(err).ShouldNot(HaveOccurred())
		waitForTrustManagerReady()
	}

	BeforeAll(func() {
		var err error
		clientset, err = kubernetes.NewForConfig(cfg)
		Expect(err).Should(BeNil())

		By("enabling TrustManager feature gate via subscription")
		err = patchSubscriptionWithEnvVars(ctx, loader, map[string]string{
			"UNSUPPORTED_ADDON_FEATURES": "TrustManager=true",
			"OPERATOR_LOG_LEVEL":         "4",
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
			return errors.IsNotFound(err)
		}, lowTimeout, fastPollInterval).Should(BeTrue())
	})

	// -------------------------------------------------------------------------
	// Resource creation and verification
	// -------------------------------------------------------------------------

	Context("resource creation", func() {
		It("should create all resources managed by the controller with correct labels", func() {
			createTrustManager(newTrustManagerCR())

			// Namespace-scoped resources
			By("verifying ServiceAccount")
			Eventually(func(g Gomega) {
				sa, err := clientset.CoreV1().ServiceAccounts(trustManagerNamespace).Get(ctx, trustManagerServiceAccountName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				verifyTrustManagerManagedLabels(sa.Labels)
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("verifying Deployment")
			Eventually(func(g Gomega) {
				dep, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				verifyTrustManagerManagedLabels(dep.Labels)
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("verifying webhook Service")
			Eventually(func(g Gomega) {
				svc, err := clientset.CoreV1().Services(trustManagerNamespace).Get(ctx, trustManagerServiceName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				verifyTrustManagerManagedLabels(svc.Labels)
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("verifying metrics Service")
			Eventually(func(g Gomega) {
				svc, err := clientset.CoreV1().Services(trustManagerNamespace).Get(ctx, trustManagerMetricsServiceName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				verifyTrustManagerManagedLabels(svc.Labels)
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("verifying trust namespace Role")
			Eventually(func(g Gomega) {
				role, err := clientset.RbacV1().Roles(trustManagerNamespace).Get(ctx, trustManagerRoleName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				verifyTrustManagerManagedLabels(role.Labels)
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("verifying trust namespace RoleBinding")
			Eventually(func(g Gomega) {
				rb, err := clientset.RbacV1().RoleBindings(trustManagerNamespace).Get(ctx, trustManagerRoleBindingName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				verifyTrustManagerManagedLabels(rb.Labels)
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("verifying leader election Role")
			Eventually(func(g Gomega) {
				role, err := clientset.RbacV1().Roles(trustManagerNamespace).Get(ctx, trustManagerLeaderElectionRoleName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				verifyTrustManagerManagedLabels(role.Labels)
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("verifying leader election RoleBinding")
			Eventually(func(g Gomega) {
				rb, err := clientset.RbacV1().RoleBindings(trustManagerNamespace).Get(ctx, trustManagerLeaderElectionRoleBindingName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				verifyTrustManagerManagedLabels(rb.Labels)
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("verifying Issuer")
			Eventually(func(g Gomega) {
				issuer, err := certmanagerClient.CertmanagerV1().Issuers(trustManagerNamespace).Get(ctx, trustManagerIssuerName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				verifyTrustManagerManagedLabels(issuer.Labels)
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("verifying Certificate")
			Eventually(func(g Gomega) {
				cert, err := certmanagerClient.CertmanagerV1().Certificates(trustManagerNamespace).Get(ctx, trustManagerCertificateName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				verifyTrustManagerManagedLabels(cert.Labels)
			}, lowTimeout, fastPollInterval).Should(Succeed())

			// Cluster-scoped resources
			By("verifying ClusterRole")
			Eventually(func(g Gomega) {
				cr, err := clientset.RbacV1().ClusterRoles().Get(ctx, trustManagerClusterRoleName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				verifyTrustManagerManagedLabels(cr.Labels)
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("verifying ClusterRoleBinding")
			Eventually(func(g Gomega) {
				crb, err := clientset.RbacV1().ClusterRoleBindings().Get(ctx, trustManagerClusterRoleBindingName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				verifyTrustManagerManagedLabels(crb.Labels)
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("verifying ValidatingWebhookConfiguration")
			Eventually(func(g Gomega) {
				vwc, err := clientset.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(ctx, trustManagerWebhookConfigName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				verifyTrustManagerManagedLabels(vwc.Labels)
			}, lowTimeout, fastPollInterval).Should(Succeed())
		})
	})

	// -------------------------------------------------------------------------
	// Resource deletion and recreation (reconciliation)
	// -------------------------------------------------------------------------

	Context("resource deletion and recreation", func() {
		It("should recreate resources managed by the controller when deleted externally", func() {
			createTrustManager(newTrustManagerCR())

			// Namespace-scoped resources
			By("deleting and verifying recreation of ServiceAccount")
			verifyTrustManagerResourceRecreation(func() error {
				return clientset.CoreV1().ServiceAccounts(trustManagerNamespace).Delete(ctx, trustManagerServiceAccountName, metav1.DeleteOptions{})
			}, func() error {
				_, err := clientset.CoreV1().ServiceAccounts(trustManagerNamespace).Get(ctx, trustManagerServiceAccountName, metav1.GetOptions{})
				return err
			})

			By("deleting and verifying recreation of webhook Service")
			verifyTrustManagerResourceRecreation(func() error {
				return clientset.CoreV1().Services(trustManagerNamespace).Delete(ctx, trustManagerServiceName, metav1.DeleteOptions{})
			}, func() error {
				_, err := clientset.CoreV1().Services(trustManagerNamespace).Get(ctx, trustManagerServiceName, metav1.GetOptions{})
				return err
			})

			By("deleting and verifying recreation of metrics Service")
			verifyTrustManagerResourceRecreation(func() error {
				return clientset.CoreV1().Services(trustManagerNamespace).Delete(ctx, trustManagerMetricsServiceName, metav1.DeleteOptions{})
			}, func() error {
				_, err := clientset.CoreV1().Services(trustManagerNamespace).Get(ctx, trustManagerMetricsServiceName, metav1.GetOptions{})
				return err
			})

			By("deleting and verifying recreation of trust namespace Role")
			verifyTrustManagerResourceRecreation(func() error {
				return clientset.RbacV1().Roles(trustManagerNamespace).Delete(ctx, trustManagerRoleName, metav1.DeleteOptions{})
			}, func() error {
				_, err := clientset.RbacV1().Roles(trustManagerNamespace).Get(ctx, trustManagerRoleName, metav1.GetOptions{})
				return err
			})

			By("deleting and verifying recreation of trust namespace RoleBinding")
			verifyTrustManagerResourceRecreation(func() error {
				return clientset.RbacV1().RoleBindings(trustManagerNamespace).Delete(ctx, trustManagerRoleBindingName, metav1.DeleteOptions{})
			}, func() error {
				_, err := clientset.RbacV1().RoleBindings(trustManagerNamespace).Get(ctx, trustManagerRoleBindingName, metav1.GetOptions{})
				return err
			})

			By("deleting and verifying recreation of leader election Role")
			verifyTrustManagerResourceRecreation(func() error {
				return clientset.RbacV1().Roles(trustManagerNamespace).Delete(ctx, trustManagerLeaderElectionRoleName, metav1.DeleteOptions{})
			}, func() error {
				_, err := clientset.RbacV1().Roles(trustManagerNamespace).Get(ctx, trustManagerLeaderElectionRoleName, metav1.GetOptions{})
				return err
			})

			By("deleting and verifying recreation of leader election RoleBinding")
			verifyTrustManagerResourceRecreation(func() error {
				return clientset.RbacV1().RoleBindings(trustManagerNamespace).Delete(ctx, trustManagerLeaderElectionRoleBindingName, metav1.DeleteOptions{})
			}, func() error {
				_, err := clientset.RbacV1().RoleBindings(trustManagerNamespace).Get(ctx, trustManagerLeaderElectionRoleBindingName, metav1.GetOptions{})
				return err
			})

			By("deleting and verifying recreation of Issuer")
			verifyTrustManagerResourceRecreation(func() error {
				return certmanagerClient.CertmanagerV1().Issuers(trustManagerNamespace).Delete(ctx, trustManagerIssuerName, metav1.DeleteOptions{})
			}, func() error {
				_, err := certmanagerClient.CertmanagerV1().Issuers(trustManagerNamespace).Get(ctx, trustManagerIssuerName, metav1.GetOptions{})
				return err
			})

			By("deleting and verifying recreation of Certificate")
			verifyTrustManagerResourceRecreation(func() error {
				return certmanagerClient.CertmanagerV1().Certificates(trustManagerNamespace).Delete(ctx, trustManagerCertificateName, metav1.DeleteOptions{})
			}, func() error {
				_, err := certmanagerClient.CertmanagerV1().Certificates(trustManagerNamespace).Get(ctx, trustManagerCertificateName, metav1.GetOptions{})
				return err
			})

			By("deleting and verifying recreation of Deployment")
			verifyTrustManagerResourceRecreation(func() error {
				return clientset.AppsV1().Deployments(trustManagerNamespace).Delete(ctx, trustManagerDeploymentName, metav1.DeleteOptions{})
			}, func() error {
				_, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
				return err
			})

			// Cluster-scoped resources
			By("deleting and verifying recreation of ClusterRole")
			verifyTrustManagerResourceRecreation(func() error {
				return clientset.RbacV1().ClusterRoles().Delete(ctx, trustManagerClusterRoleName, metav1.DeleteOptions{})
			}, func() error {
				_, err := clientset.RbacV1().ClusterRoles().Get(ctx, trustManagerClusterRoleName, metav1.GetOptions{})
				return err
			})

			By("deleting and verifying recreation of ClusterRoleBinding")
			verifyTrustManagerResourceRecreation(func() error {
				return clientset.RbacV1().ClusterRoleBindings().Delete(ctx, trustManagerClusterRoleBindingName, metav1.DeleteOptions{})
			}, func() error {
				_, err := clientset.RbacV1().ClusterRoleBindings().Get(ctx, trustManagerClusterRoleBindingName, metav1.GetOptions{})
				return err
			})

			By("deleting and verifying recreation of ValidatingWebhookConfiguration")
			verifyTrustManagerResourceRecreation(func() error {
				return clientset.AdmissionregistrationV1().ValidatingWebhookConfigurations().Delete(ctx, trustManagerWebhookConfigName, metav1.DeleteOptions{})
			}, func() error {
				_, err := clientset.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(ctx, trustManagerWebhookConfigName, metav1.GetOptions{})
				return err
			})
		})
	})

	// -------------------------------------------------------------------------
	// Label drift reconciliation
	// -------------------------------------------------------------------------

	Context("label drift reconciliation", func() {
		It("should restore labels when modified externally on managed resources", func() {
			createTrustManager(newTrustManagerCR())

			By("modifying ServiceAccount labels externally")
			sa, err := clientset.CoreV1().ServiceAccounts(trustManagerNamespace).Get(ctx, trustManagerServiceAccountName, metav1.GetOptions{})
			Expect(err).ShouldNot(HaveOccurred())
			sa.Labels["app.kubernetes.io/instance"] = "modified-value"
			_, err = clientset.CoreV1().ServiceAccounts(trustManagerNamespace).Update(ctx, sa, metav1.UpdateOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			By("verifying controller restores ServiceAccount labels")
			Eventually(func(g Gomega) {
				sa, err := clientset.CoreV1().ServiceAccounts(trustManagerNamespace).Get(ctx, trustManagerServiceAccountName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(sa.Labels).Should(HaveKeyWithValue("app.kubernetes.io/instance", trustManagerCommonName))
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("modifying ClusterRole labels externally")
			cr, err := clientset.RbacV1().ClusterRoles().Get(ctx, trustManagerClusterRoleName, metav1.GetOptions{})
			Expect(err).ShouldNot(HaveOccurred())
			cr.Labels["app"] = "tampered"
			_, err = clientset.RbacV1().ClusterRoles().Update(ctx, cr, metav1.UpdateOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			By("verifying controller restores ClusterRole labels")
			Eventually(func(g Gomega) {
				cr, err := clientset.RbacV1().ClusterRoles().Get(ctx, trustManagerClusterRoleName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(cr.Labels).Should(HaveKeyWithValue("app", trustManagerCommonName))
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("modifying Deployment pod template labels externally")
			dep, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
			Expect(err).ShouldNot(HaveOccurred())
			dep.Spec.Template.Labels["app.kubernetes.io/name"] = "tampered"
			_, err = clientset.AppsV1().Deployments(trustManagerNamespace).Update(ctx, dep, metav1.UpdateOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			By("verifying controller restores Deployment pod template labels")
			Eventually(func(g Gomega) {
				dep, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(dep.Spec.Template.Labels).Should(HaveKeyWithValue("app.kubernetes.io/name", trustManagerCommonName))
			}, lowTimeout, fastPollInterval).Should(Succeed())
		})
	})

	// -------------------------------------------------------------------------
	// Managed label removal reconciliation
	// -------------------------------------------------------------------------

	Context("managed label removal reconciliation", func() {
		It("should restore the managed label when removed externally from resources", func() {
			createTrustManager(newTrustManagerCR())

			// The "app" label is the managed resource label used by the predicate
			// to filter watch events. Removing it tests that the predicate checks
			// both old and new objects on updates, so the event is not silently dropped.

			By("removing managed label from ServiceAccount")
			sa, err := clientset.CoreV1().ServiceAccounts(trustManagerNamespace).Get(ctx, trustManagerServiceAccountName, metav1.GetOptions{})
			Expect(err).ShouldNot(HaveOccurred())
			delete(sa.Labels, "app")
			_, err = clientset.CoreV1().ServiceAccounts(trustManagerNamespace).Update(ctx, sa, metav1.UpdateOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			By("verifying controller restores managed label on ServiceAccount")
			Eventually(func(g Gomega) {
				sa, err := clientset.CoreV1().ServiceAccounts(trustManagerNamespace).Get(ctx, trustManagerServiceAccountName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(sa.Labels).Should(HaveKeyWithValue("app", trustManagerCommonName))
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("removing managed label from Deployment")
			dep, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
			Expect(err).ShouldNot(HaveOccurred())
			delete(dep.Labels, "app")
			_, err = clientset.AppsV1().Deployments(trustManagerNamespace).Update(ctx, dep, metav1.UpdateOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			By("verifying controller restores managed label on Deployment")
			Eventually(func(g Gomega) {
				dep, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(dep.Labels).Should(HaveKeyWithValue("app", trustManagerCommonName))
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("removing managed label from ClusterRole")
			cr, err := clientset.RbacV1().ClusterRoles().Get(ctx, trustManagerClusterRoleName, metav1.GetOptions{})
			Expect(err).ShouldNot(HaveOccurred())
			delete(cr.Labels, "app")
			_, err = clientset.RbacV1().ClusterRoles().Update(ctx, cr, metav1.UpdateOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			By("verifying controller restores managed label on ClusterRole")
			Eventually(func(g Gomega) {
				cr, err := clientset.RbacV1().ClusterRoles().Get(ctx, trustManagerClusterRoleName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(cr.Labels).Should(HaveKeyWithValue("app", trustManagerCommonName))
			}, lowTimeout, fastPollInterval).Should(Succeed())
		})
	})

	// -------------------------------------------------------------------------
	// Deployment configuration
	// -------------------------------------------------------------------------

	Context("deployment configuration", func() {
		It("should have deployment available with correct configuration", func() {
			createTrustManager(newTrustManagerCR())

			By("waiting for trust-manager deployment to become available")
			err := pollTillDeploymentAvailable(ctx, clientset, trustManagerNamespace, trustManagerDeploymentName)
			Expect(err).ShouldNot(HaveOccurred())

			dep, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(dep.Spec.Template.Spec.Containers).ShouldNot(BeEmpty())

			By("verifying deployment container args contain expected flags")
			verifyTrustManagerDefaultContainerArgs(dep.Spec.Template.Spec.Containers[0].Args)

			By("verifying deployment references correct ServiceAccount")
			Expect(dep.Spec.Template.Spec.ServiceAccountName).Should(Equal(trustManagerServiceAccountName))

			By("verifying TLS secret volume references the correct secret")
			verifyTrustManagerTLSVolume(dep.Spec.Template.Spec.Volumes)
		})

		It("should update deployment args when log level changes", func() {
			createTrustManager(newTrustManagerCR())

			err := pollTillDeploymentAvailable(ctx, clientset, trustManagerNamespace, trustManagerDeploymentName)
			Expect(err).ShouldNot(HaveOccurred())

			By("updating TrustManager CR with new log level")
			Eventually(func() error {
				tm, err := trustManagerClient().Get(ctx, "cluster", metav1.GetOptions{})
				if err != nil {
					return err
				}
				tm.Spec.TrustManagerConfig.LogLevel = 3
				_, err = trustManagerClient().Update(ctx, tm, metav1.UpdateOptions{})
				return err
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("verifying deployment args are updated with new log level")
			Eventually(func(g Gomega) {
				dep, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(dep.Spec.Template.Spec.Containers).ShouldNot(BeEmpty())
				g.Expect(dep.Spec.Template.Spec.Containers[0].Args).Should(ContainElement("--log-level=3"))
			}, lowTimeout, fastPollInterval).Should(Succeed())
		})

		It("should apply custom resource requirements to deployment", func() {
			createTrustManager(newTrustManagerCR().WithResources(corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("50m"),
					corev1.ResourceMemory: resource.MustParse("64Mi"),
				},
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("200m"),
					corev1.ResourceMemory: resource.MustParse("256Mi"),
				},
			}))

			By("verifying deployment has custom resource requirements")
			Eventually(func(g Gomega) {
				dep, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(dep.Spec.Template.Spec.Containers).ShouldNot(BeEmpty())
				container := dep.Spec.Template.Spec.Containers[0]
				g.Expect(container.Resources.Requests.Cpu().String()).Should(Equal("50m"))
				g.Expect(container.Resources.Requests.Memory().String()).Should(Equal("64Mi"))
				g.Expect(container.Resources.Limits.Cpu().String()).Should(Equal("200m"))
				g.Expect(container.Resources.Limits.Memory().String()).Should(Equal("256Mi"))
			}, lowTimeout, fastPollInterval).Should(Succeed())
		})

		It("should apply custom tolerations to deployment", func() {
			createTrustManager(newTrustManagerCR().WithTolerations([]corev1.Toleration{
				{
					Key:      "test-key",
					Operator: corev1.TolerationOpEqual,
					Value:    "test-value",
					Effect:   corev1.TaintEffectNoSchedule,
				},
			}))

			By("verifying deployment has custom tolerations")
			Eventually(func(g Gomega) {
				dep, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())

				var found bool
				for _, t := range dep.Spec.Template.Spec.Tolerations {
					if t.Key == "test-key" && t.Value == "test-value" && t.Effect == corev1.TaintEffectNoSchedule {
						found = true
						break
					}
				}
				g.Expect(found).Should(BeTrue(), "custom toleration not found on deployment")
			}, lowTimeout, fastPollInterval).Should(Succeed())
		})

		It("should apply custom nodeSelector to deployment", func() {
			createTrustManager(newTrustManagerCR().WithNodeSelector(map[string]string{
				"test-node-label": "test-value",
			}))

			By("verifying deployment has custom nodeSelector")
			Eventually(func(g Gomega) {
				dep, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(dep.Spec.Template.Spec.NodeSelector).Should(HaveKeyWithValue("test-node-label", "test-value"))
			}, lowTimeout, fastPollInterval).Should(Succeed())
		})

		It("should apply custom affinity to deployment", func() {
			createTrustManager(newTrustManagerCR().WithAffinity(&corev1.Affinity{
				PodAntiAffinity: &corev1.PodAntiAffinity{
					PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
						{
							Weight: 100,
							PodAffinityTerm: corev1.PodAffinityTerm{
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"app": trustManagerCommonName,
									},
								},
								TopologyKey: "kubernetes.io/hostname",
							},
						},
					},
				},
			}))

			By("verifying deployment has custom affinity")
			Eventually(func(g Gomega) {
				dep, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(dep.Spec.Template.Spec.Affinity).ShouldNot(BeNil())
				g.Expect(dep.Spec.Template.Spec.Affinity.PodAntiAffinity).ShouldNot(BeNil())
				terms := dep.Spec.Template.Spec.Affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution
				g.Expect(terms).Should(HaveLen(1))
				g.Expect(terms[0].Weight).Should(Equal(int32(100)))
				g.Expect(terms[0].PodAffinityTerm.TopologyKey).Should(Equal("kubernetes.io/hostname"))
			}, lowTimeout, fastPollInterval).Should(Succeed())
		})

		It("should set --trust-namespace on deployment for custom trust namespace", func() {
			By("creating custom trust namespace")
			customTrustNS := createUniqueNamespace("custom-trust-ns")
			createAndDestroyTestNamespace(ctx, clientset, customTrustNS)

			createTrustManager(newTrustManagerCR().WithTrustNamespace(customTrustNS))

			By("verifying deployment has correct --trust-namespace arg")
			Eventually(func(g Gomega) {
				deployment, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(len(deployment.Spec.Template.Spec.Containers)).Should(BeNumerically(">", 0))
				g.Expect(deployment.Spec.Template.Spec.Containers[0].Args).Should(ContainElement(fmt.Sprintf("--trust-namespace=%s", customTrustNS)))
			}, lowTimeout, fastPollInterval).Should(Succeed())
		})

		It("should add secret-targets-enabled arg when secretTargets policy is Custom", func() {
			createTrustManager(newTrustManagerCR().WithSecretTargets(v1alpha1.SecretTargetsPolicyCustom, []string{"test-secret"}))

			By("verifying deployment args contain --secret-targets-enabled=true")
			Eventually(func(g Gomega) {
				dep, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(dep.Spec.Template.Spec.Containers).ShouldNot(BeEmpty())
				g.Expect(dep.Spec.Template.Spec.Containers[0].Args).Should(ContainElement("--secret-targets-enabled=true"))
			}, lowTimeout, fastPollInterval).Should(Succeed())
		})

		It("should not have secret-targets-enabled arg when secretTargets is Disabled", func() {
			createTrustManager(newTrustManagerCR())

			By("verifying deployment args do not contain --secret-targets-enabled=true")
			Eventually(func(g Gomega) {
				dep, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(dep.Spec.Template.Spec.Containers).ShouldNot(BeEmpty())
				g.Expect(dep.Spec.Template.Spec.Containers[0].Args).ShouldNot(ContainElement("--secret-targets-enabled=true"))
			}, lowTimeout, fastPollInterval).Should(Succeed())
		})

		// TODO: Add test for other deployment configuration options
		// (e.g. filter expired certificates policy; custom trust namespace is covered above.)
	})

	// -------------------------------------------------------------------------
	// Default CA package configuration
	// -------------------------------------------------------------------------

	Context("default CA package configuration", func() {
		It("should reconcile deployment when default CA package policy transitions between Disabled and Enabled", func() {
			createTrustManager(newTrustManagerCR())

			By("verifying no default CA package ConfigMap exists")
			Eventually(func(g Gomega) {
				_, err := clientset.CoreV1().ConfigMaps(trustManagerNamespace).Get(ctx, defaultCAPackageConfigMapName, metav1.GetOptions{})
				g.Expect(errors.IsNotFound(err)).Should(BeTrue(), "expected ConfigMap to not exist")
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("verifying deployment does not have --default-package-location arg")
			Eventually(func(g Gomega) {
				dep, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(dep.Spec.Template.Spec.Containers).ShouldNot(BeEmpty())
				g.Expect(dep.Spec.Template.Spec.Containers[0].Args).ShouldNot(
					ContainElement(ContainSubstring("--default-package-location")),
				)
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("verifying deployment does not have ConfigMap-backed CA package volume")
			Eventually(func(g Gomega) {
				dep, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				for _, v := range dep.Spec.Template.Spec.Volumes {
					if v.Name == defaultCAPackageVolumeName {
						g.Expect(v.ConfigMap).Should(BeNil(), "expected no ConfigMap-backed volume %q when disabled", defaultCAPackageVolumeName)
					}
				}
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("verifying deployment pod template does not have CA bundle hash annotation")
			Eventually(func(g Gomega) {
				dep, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				if dep.Spec.Template.Annotations != nil {
					g.Expect(dep.Spec.Template.Annotations).ShouldNot(HaveKey(defaultCAPackageHashAnnotation))
				}
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("updating TrustManager CR to enable default CA package")
			Eventually(func() error {
				tm, err := trustManagerClient().Get(ctx, "cluster", metav1.GetOptions{})
				if err != nil {
					return err
				}
				tm.Spec.TrustManagerConfig.DefaultCAPackage.Policy = v1alpha1.DefaultCAPackagePolicyEnabled
				_, err = trustManagerClient().Update(ctx, tm, metav1.UpdateOptions{})
				return err
			}, lowTimeout, fastPollInterval).Should(Succeed())

			waitForTrustManagerReady()

			By("verifying the CNO-injected CA bundle ConfigMap exists in operator namespace")
			Eventually(func(g Gomega) {
				cm, err := clientset.CoreV1().ConfigMaps(operatorNamespace).Get(ctx, trustedCABundleConfigMapName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(cm.Data).Should(HaveKey(trustedCABundleKey))
				g.Expect(cm.Data[trustedCABundleKey]).ShouldNot(BeEmpty())
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("verifying the default CA package ConfigMap is created in operand namespace")
			Eventually(func(g Gomega) {
				cm, err := clientset.CoreV1().ConfigMaps(trustManagerNamespace).Get(ctx, defaultCAPackageConfigMapName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(cm.Data).Should(HaveKey("cert-manager-package-openshift.json"))
				g.Expect(cm.Data["cert-manager-package-openshift.json"]).ShouldNot(BeEmpty())
				verifyTrustManagerManagedLabels(cm.Labels)
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("verifying deployment has --default-package-location arg")
			Eventually(func(g Gomega) {
				dep, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(dep.Spec.Template.Spec.Containers).ShouldNot(BeEmpty())
				g.Expect(dep.Spec.Template.Spec.Containers[0].Args).Should(
					ContainElement(fmt.Sprintf("--default-package-location=%s", defaultCAPackageLocation)),
				)
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("verifying deployment has CA package volume")
			Eventually(func(g Gomega) {
				dep, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())

				var hasVolume bool
				for _, v := range dep.Spec.Template.Spec.Volumes {
					if v.Name == defaultCAPackageVolumeName && v.ConfigMap != nil &&
						v.ConfigMap.Name == defaultCAPackageConfigMapName {
						hasVolume = true
						break
					}
				}
				g.Expect(hasVolume).Should(BeTrue(), "expected volume %q with configMap %q", defaultCAPackageVolumeName, defaultCAPackageConfigMapName)
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("verifying deployment container has CA package volume mount")
			Eventually(func(g Gomega) {
				dep, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(dep.Spec.Template.Spec.Containers).ShouldNot(BeEmpty())

				var hasMount bool
				for _, vm := range dep.Spec.Template.Spec.Containers[0].VolumeMounts {
					if vm.Name == defaultCAPackageVolumeName && vm.MountPath == defaultCAPackageMountPath && vm.ReadOnly {
						hasMount = true
						break
					}
				}
				g.Expect(hasMount).Should(BeTrue(), "expected volume mount %q at %q (readOnly)", defaultCAPackageVolumeName, defaultCAPackageMountPath)
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("verifying deployment pod template has CA bundle hash annotation")
			Eventually(func(g Gomega) {
				dep, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(dep.Spec.Template.Annotations).Should(HaveKey(defaultCAPackageHashAnnotation))
				g.Expect(dep.Spec.Template.Annotations[defaultCAPackageHashAnnotation]).ShouldNot(BeEmpty())
			}, lowTimeout, fastPollInterval).Should(Succeed())

			// --- Enabled → Disabled transition ---

			By("updating TrustManager CR to disable default CA package")
			Eventually(func() error {
				tm, err := trustManagerClient().Get(ctx, "cluster", metav1.GetOptions{})
				if err != nil {
					return err
				}
				tm.Spec.TrustManagerConfig.DefaultCAPackage.Policy = v1alpha1.DefaultCAPackagePolicyDisabled
				_, err = trustManagerClient().Update(ctx, tm, metav1.UpdateOptions{})
				return err
			}, lowTimeout, fastPollInterval).Should(Succeed())

			waitForTrustManagerReady()

			By("verifying deployment does not have --default-package-location arg after disabling")
			Eventually(func(g Gomega) {
				dep, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(dep.Spec.Template.Spec.Containers).ShouldNot(BeEmpty())
				g.Expect(dep.Spec.Template.Spec.Containers[0].Args).ShouldNot(
					ContainElement(ContainSubstring("--default-package-location")),
				)
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("verifying deployment does not have ConfigMap-backed CA package volume after disabling")
			Eventually(func(g Gomega) {
				dep, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				for _, v := range dep.Spec.Template.Spec.Volumes {
					if v.Name == defaultCAPackageVolumeName {
						g.Expect(v.ConfigMap).Should(BeNil(), "expected no ConfigMap-backed volume %q after disabling", defaultCAPackageVolumeName)
					}
				}
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("verifying deployment pod template does not have CA bundle hash annotation after disabling")
			Eventually(func(g Gomega) {
				dep, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				if dep.Spec.Template.Annotations != nil {
					g.Expect(dep.Spec.Template.Annotations).ShouldNot(HaveKey(defaultCAPackageHashAnnotation))
				}
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("verifying the default CA package ConfigMap still exists after disabling (operator does not delete managed resources)")
			Eventually(func(g Gomega) {
				cm, err := clientset.CoreV1().ConfigMaps(trustManagerNamespace).Get(ctx, defaultCAPackageConfigMapName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(cm.Data).Should(HaveKey("cert-manager-package-openshift.json"))
			}, lowTimeout, fastPollInterval).Should(Succeed())
		})

		It("should reconcile ConfigMap data drift when CA package ConfigMap is tampered", func() {
			createTrustManager(newTrustManagerCR().WithDefaultCAPackage(v1alpha1.DefaultCAPackagePolicyEnabled))

			var originalData string
			By("reading original CA package ConfigMap data")
			Eventually(func(g Gomega) {
				cm, err := clientset.CoreV1().ConfigMaps(trustManagerNamespace).Get(ctx, defaultCAPackageConfigMapName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(cm.Data).Should(HaveKey("cert-manager-package-openshift.json"))
				originalData = cm.Data["cert-manager-package-openshift.json"]
				g.Expect(originalData).ShouldNot(BeEmpty())
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("tampering with the CA package ConfigMap data")
			cm, err := clientset.CoreV1().ConfigMaps(trustManagerNamespace).Get(ctx, defaultCAPackageConfigMapName, metav1.GetOptions{})
			Expect(err).ShouldNot(HaveOccurred())
			cm.Data["cert-manager-package-openshift.json"] = `{"name":"tampered","bundle":"bad","version":"0"}`
			_, err = clientset.CoreV1().ConfigMaps(trustManagerNamespace).Update(ctx, cm, metav1.UpdateOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			By("verifying controller restores the original CA package ConfigMap data")
			Eventually(func(g Gomega) {
				cm, err := clientset.CoreV1().ConfigMaps(trustManagerNamespace).Get(ctx, defaultCAPackageConfigMapName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(cm.Data["cert-manager-package-openshift.json"]).Should(Equal(originalData))
			}, lowTimeout, fastPollInterval).Should(Succeed())
		})
	})

	// -------------------------------------------------------------------------
	// RBAC configuration
	// -------------------------------------------------------------------------

	Context("RBAC configuration", func() {
		It("should configure ClusterRoleBinding with correct subjects and roleRef", func() {
			createTrustManager(newTrustManagerCR())

			By("verifying ClusterRoleBinding references correct ClusterRole and ServiceAccount")
			Eventually(func(g Gomega) {
				crb, err := clientset.RbacV1().ClusterRoleBindings().Get(ctx, trustManagerClusterRoleBindingName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(crb.RoleRef.Name).Should(Equal(trustManagerClusterRoleName))
				g.Expect(crb.RoleRef.Kind).Should(Equal("ClusterRole"))

				g.Expect(crb.Subjects).ShouldNot(BeEmpty())
				g.Expect(crb.Subjects[0].Kind).Should(Equal("ServiceAccount"))
				g.Expect(crb.Subjects[0].Name).Should(Equal(trustManagerServiceAccountName))
				g.Expect(crb.Subjects[0].Namespace).Should(Equal(trustManagerNamespace))
			}, lowTimeout, fastPollInterval).Should(Succeed())
		})

		It("should configure trust namespace RoleBinding with correct subjects and roleRef", func() {
			createTrustManager(newTrustManagerCR())

			By("verifying trust namespace RoleBinding references correct Role and ServiceAccount")
			Eventually(func(g Gomega) {
				rb, err := clientset.RbacV1().RoleBindings(trustManagerNamespace).Get(ctx, trustManagerRoleBindingName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(rb.RoleRef.Name).Should(Equal(trustManagerRoleName))
				g.Expect(rb.RoleRef.Kind).Should(Equal("Role"))

				g.Expect(rb.Subjects).ShouldNot(BeEmpty())
				g.Expect(rb.Subjects[0].Kind).Should(Equal("ServiceAccount"))
				g.Expect(rb.Subjects[0].Name).Should(Equal(trustManagerServiceAccountName))
				g.Expect(rb.Subjects[0].Namespace).Should(Equal(trustManagerNamespace))
			}, lowTimeout, fastPollInterval).Should(Succeed())
		})

		It("should configure leader election RoleBinding with correct subjects and roleRef", func() {
			createTrustManager(newTrustManagerCR())

			By("verifying leader election RoleBinding references correct Role and ServiceAccount")
			Eventually(func(g Gomega) {
				rb, err := clientset.RbacV1().RoleBindings(trustManagerNamespace).Get(ctx, trustManagerLeaderElectionRoleBindingName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(rb.RoleRef.Name).Should(Equal(trustManagerLeaderElectionRoleName))
				g.Expect(rb.RoleRef.Kind).Should(Equal("Role"))

				g.Expect(rb.Subjects).ShouldNot(BeEmpty())
				g.Expect(rb.Subjects[0].Kind).Should(Equal("ServiceAccount"))
				g.Expect(rb.Subjects[0].Name).Should(Equal(trustManagerServiceAccountName))
				g.Expect(rb.Subjects[0].Namespace).Should(Equal(trustManagerNamespace))
			}, lowTimeout, fastPollInterval).Should(Succeed())
		})

		It("should create Role and RoleBinding in custom trust namespace", func() {
			By("creating custom trust namespace")
			customTrustNS := createUniqueNamespace("custom-trust-ns")
			createAndDestroyTestNamespace(ctx, clientset, customTrustNS)

			By("creating TrustManager CR with custom trust namespace")
			createTrustManager(newTrustManagerCR().WithTrustNamespace(customTrustNS))

			By("verifying Role is created in custom trust namespace")
			Eventually(func(g Gomega) {
				role, err := clientset.RbacV1().Roles(customTrustNS).Get(ctx, trustManagerRoleName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(role.Namespace).Should(Equal(customTrustNS))
				verifyTrustManagerManagedLabels(role.Labels)
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("verifying RoleBinding is created in custom trust namespace")
			Eventually(func(g Gomega) {
				rb, err := clientset.RbacV1().RoleBindings(customTrustNS).Get(ctx, trustManagerRoleBindingName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(rb.Namespace).Should(Equal(customTrustNS))
				g.Expect(rb.RoleRef.Name).Should(Equal(trustManagerRoleName))
				g.Expect(rb.Subjects).ShouldNot(BeEmpty())
				g.Expect(rb.Subjects[0].Name).Should(Equal(trustManagerServiceAccountName))
				g.Expect(rb.Subjects[0].Namespace).Should(Equal(trustManagerNamespace))
				verifyTrustManagerManagedLabels(rb.Labels)
			}, lowTimeout, fastPollInterval).Should(Succeed())
		})

		It("should place leader election Role and RoleBinding in operand namespace when trust namespace is custom", func() {
			By("creating custom trust namespace")
			customTrustNS := createUniqueNamespace("custom-trust-ns")
			createAndDestroyTestNamespace(ctx, clientset, customTrustNS)

			By("creating TrustManager CR with custom trust namespace")
			createTrustManager(newTrustManagerCR().WithTrustNamespace(customTrustNS))

			By("verifying leader election Role is in operand namespace")
			Eventually(func(g Gomega) {
				role, err := clientset.RbacV1().Roles(trustManagerNamespace).Get(ctx, trustManagerLeaderElectionRoleName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(role.Namespace).Should(Equal(trustManagerNamespace))
				verifyTrustManagerManagedLabels(role.Labels)
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("verifying leader election RoleBinding is in operand namespace")
			Eventually(func(g Gomega) {
				rb, err := clientset.RbacV1().RoleBindings(trustManagerNamespace).Get(ctx, trustManagerLeaderElectionRoleBindingName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(rb.Namespace).Should(Equal(trustManagerNamespace))
				verifyTrustManagerManagedLabels(rb.Labels)
			}, lowTimeout, fastPollInterval).Should(Succeed())
		})

		It("should have no secret rules on ClusterRole when secretTargets is Disabled", func() {
			createTrustManager(newTrustManagerCR())

			By("verifying ClusterRole has no secret rules")
			Eventually(func(g Gomega) {
				cr, err := clientset.RbacV1().ClusterRoles().Get(ctx, trustManagerClusterRoleName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(hasSecretRule(cr.Rules)).Should(BeFalse(), "ClusterRole should not have secret rules when secretTargets is Disabled")
			}, lowTimeout, fastPollInterval).Should(Succeed())
		})

		It("should add secret read and scoped write rules to ClusterRole when secretTargets is Custom", func() {
			authorizedSecrets := []string{"bundle-secret-a", "bundle-secret-b"}
			createTrustManager(newTrustManagerCR().WithSecretTargets(v1alpha1.SecretTargetsPolicyCustom, authorizedSecrets))

			By("verifying ClusterRole has secret read rule")
			Eventually(func(g Gomega) {
				cr, err := clientset.RbacV1().ClusterRoles().Get(ctx, trustManagerClusterRoleName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())

				readRule := findSecretRule(cr.Rules, "get")
				g.Expect(readRule).ShouldNot(BeNil(), "expected secret read rule with 'get' verb")
				g.Expect(readRule.Verbs).Should(ContainElements("get", "list", "watch"))
				g.Expect(readRule.ResourceNames).Should(BeEmpty(), "secret read rule should not be scoped to resourceNames")
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("verifying ClusterRole has scoped secret write rule")
			Eventually(func(g Gomega) {
				cr, err := clientset.RbacV1().ClusterRoles().Get(ctx, trustManagerClusterRoleName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())

				writeRule := findSecretRule(cr.Rules, "create")
				g.Expect(writeRule).ShouldNot(BeNil(), "expected secret write rule with 'create' verb")
				g.Expect(writeRule.Verbs).Should(ContainElements("create", "update", "patch", "delete"))
				g.Expect(writeRule.ResourceNames).Should(ConsistOf("bundle-secret-a", "bundle-secret-b"))
			}, lowTimeout, fastPollInterval).Should(Succeed())
		})

		It("should update ClusterRole rules when secretTargets policy changes from Disabled to Custom", func() {
			createTrustManager(newTrustManagerCR())

			By("verifying ClusterRole initially has no secret rules")
			Eventually(func(g Gomega) {
				cr, err := clientset.RbacV1().ClusterRoles().Get(ctx, trustManagerClusterRoleName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(hasSecretRule(cr.Rules)).Should(BeFalse())
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("updating TrustManager CR to enable secretTargets with Custom policy")
			Eventually(func() error {
				tm, err := trustManagerClient().Get(ctx, "cluster", metav1.GetOptions{})
				if err != nil {
					return err
				}
				tm.Spec.TrustManagerConfig.SecretTargets = v1alpha1.SecretTargetsConfig{
					Policy:            v1alpha1.SecretTargetsPolicyCustom,
					AuthorizedSecrets: []string{"updated-secret"},
				}
				_, err = trustManagerClient().Update(ctx, tm, metav1.UpdateOptions{})
				return err
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("verifying ClusterRole now has secret rules")
			Eventually(func(g Gomega) {
				cr, err := clientset.RbacV1().ClusterRoles().Get(ctx, trustManagerClusterRoleName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())

				writeRule := findSecretRule(cr.Rules, "create")
				g.Expect(writeRule).ShouldNot(BeNil(), "expected secret write rule after enabling secretTargets")
				g.Expect(writeRule.ResourceNames).Should(ConsistOf("updated-secret"))
			}, lowTimeout, fastPollInterval).Should(Succeed())
		})

		It("should update ClusterRole resourceNames when authorizedSecrets list changes", func() {
			createTrustManager(newTrustManagerCR().
				WithSecretTargets(v1alpha1.SecretTargetsPolicyCustom, []string{"secret-a", "secret-b"}))

			By("verifying ClusterRole has initial authorized secrets")
			Eventually(func(g Gomega) {
				cr, err := clientset.RbacV1().ClusterRoles().Get(ctx, trustManagerClusterRoleName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())

				writeRule := findSecretRule(cr.Rules, "create")
				g.Expect(writeRule).ShouldNot(BeNil(), "expected secret write rule")
				g.Expect(writeRule.ResourceNames).Should(ConsistOf("secret-a", "secret-b"))
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("updating authorizedSecrets to a different list")
			Eventually(func() error {
				tm, err := trustManagerClient().Get(ctx, "cluster", metav1.GetOptions{})
				if err != nil {
					return err
				}
				tm.Spec.TrustManagerConfig.SecretTargets = v1alpha1.SecretTargetsConfig{
					Policy:            v1alpha1.SecretTargetsPolicyCustom,
					AuthorizedSecrets: []string{"secret-b", "secret-c", "secret-d"},
				}
				_, err = trustManagerClient().Update(ctx, tm, metav1.UpdateOptions{})
				return err
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("verifying ClusterRole resourceNames reflect the updated list")
			Eventually(func(g Gomega) {
				cr, err := clientset.RbacV1().ClusterRoles().Get(ctx, trustManagerClusterRoleName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())

				writeRule := findSecretRule(cr.Rules, "create")
				g.Expect(writeRule).ShouldNot(BeNil(), "expected secret write rule after update")
				g.Expect(writeRule.ResourceNames).Should(ConsistOf("secret-b", "secret-c", "secret-d"))
			}, lowTimeout, fastPollInterval).Should(Succeed())
		})
	})

	// -------------------------------------------------------------------------
	// Webhook and certificate configuration
	// -------------------------------------------------------------------------

	Context("webhook and certificate configuration", func() {
		It("should configure webhook with cert-manager CA injection annotation", func() {
			createTrustManager(newTrustManagerCR())

			expectedAnnotation := fmt.Sprintf("%s/%s", trustManagerNamespace, trustManagerCertificateName)

			By("verifying ValidatingWebhookConfiguration has correct CA injection annotation")
			Eventually(func(g Gomega) {
				vwc, err := clientset.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(ctx, trustManagerWebhookConfigName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(vwc.Annotations).Should(HaveKeyWithValue("cert-manager.io/inject-ca-from", expectedAnnotation))
			}, lowTimeout, fastPollInterval).Should(Succeed())
		})

		It("should configure webhook service reference correctly", func() {
			createTrustManager(newTrustManagerCR())

			By("verifying webhook service references are correct")
			Eventually(func(g Gomega) {
				vwc, err := clientset.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(ctx, trustManagerWebhookConfigName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(vwc.Webhooks).ShouldNot(BeEmpty())

				for _, wh := range vwc.Webhooks {
					g.Expect(wh.ClientConfig.Service).ShouldNot(BeNil())
					g.Expect(wh.ClientConfig.Service.Name).Should(Equal(trustManagerServiceName))
					g.Expect(wh.ClientConfig.Service.Namespace).Should(Equal(trustManagerNamespace))
				}
			}, lowTimeout, fastPollInterval).Should(Succeed())
		})

		It("should have Issuer become ready", func() {
			createTrustManager(newTrustManagerCR())

			By("waiting for trust-manager Issuer to become ready")
			err := waitForIssuerReadiness(ctx, trustManagerIssuerName, trustManagerNamespace)
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("should have Certificate become ready and create TLS secret", func() {
			createTrustManager(newTrustManagerCR())

			By("waiting for trust-manager Certificate to become ready")
			err := waitForCertificateReadiness(ctx, trustManagerCertificateName, trustManagerNamespace)
			Expect(err).ShouldNot(HaveOccurred())

			By("verifying TLS secret is created with expected keys")
			Eventually(func(g Gomega) {
				secret, err := clientset.CoreV1().Secrets(trustManagerNamespace).Get(ctx, trustManagerTLSSecretName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(secret.Data).Should(HaveKey("tls.crt"))
				g.Expect(secret.Data).Should(HaveKey("tls.key"))
				g.Expect(secret.Data).Should(HaveKey("ca.crt"))
			}, highTimeout, slowPollInterval).Should(Succeed())
		})

		It("should configure Certificate with correct spec fields", func() {
			createTrustManager(newTrustManagerCR())

			expectedDNSName := fmt.Sprintf("%s.%s.svc", trustManagerServiceName, trustManagerNamespace)

			By("verifying Certificate spec fields")
			Eventually(func(g Gomega) {
				cert, err := certmanagerClient.CertmanagerV1().Certificates(trustManagerNamespace).Get(ctx, trustManagerCertificateName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(cert.Spec.SecretName).Should(Equal(trustManagerTLSSecretName))
				g.Expect(cert.Spec.CommonName).Should(Equal(expectedDNSName))
				g.Expect(cert.Spec.DNSNames).Should(ContainElement(expectedDNSName))
				g.Expect(cert.Spec.IssuerRef.Name).Should(Equal(trustManagerIssuerName))
				g.Expect(cert.Spec.IssuerRef.Kind).Should(Equal("Issuer"))
			}, lowTimeout, fastPollInterval).Should(Succeed())
		})
	})

	// -------------------------------------------------------------------------
	// Status reporting
	// -------------------------------------------------------------------------

	Context("status reporting", func() {
		It("should report trust-manager image in status", func() {
			createTrustManager(newTrustManagerCR())

			By("verifying TrustManager status has image set")
			Eventually(func(g Gomega) {
				tm, err := trustManagerClient().Get(ctx, "cluster", metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(tm.Status.TrustManagerImage).ShouldNot(BeEmpty())
			}, lowTimeout, fastPollInterval).Should(Succeed())
		})

		It("should report trust namespace in status", func() {
			createTrustManager(newTrustManagerCR())

			By("verifying TrustManager status has default trust namespace set")
			Eventually(func(g Gomega) {
				tm, err := trustManagerClient().Get(ctx, "cluster", metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(tm.Status.TrustNamespace).Should(Equal("cert-manager"))
			}, lowTimeout, fastPollInterval).Should(Succeed())
		})

		It("should report custom trust namespace in status", func() {
			By("creating custom trust namespace")
			customTrustNS := createUniqueNamespace("custom-trust-ns-status")
			createAndDestroyTestNamespace(ctx, clientset, customTrustNS)

			createTrustManager(newTrustManagerCR().WithTrustNamespace(customTrustNS))

			By("verifying TrustManager status has custom trust namespace set")
			Eventually(func(g Gomega) {
				tm, err := trustManagerClient().Get(ctx, "cluster", metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(tm.Status.TrustNamespace).Should(Equal(customTrustNS))
			}, lowTimeout, fastPollInterval).Should(Succeed())
		})

		It("should report secretTargets policy in status", func() {
			createTrustManager(newTrustManagerCR().WithSecretTargets(v1alpha1.SecretTargetsPolicyCustom, []string{"status-test-secret"}))

			By("verifying TrustManager status reflects Custom secretTargets policy")
			Eventually(func(g Gomega) {
				tm, err := trustManagerClient().Get(ctx, "cluster", metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(tm.Status.SecretTargetsPolicy).Should(Equal(v1alpha1.SecretTargetsPolicyCustom))
			}, lowTimeout, fastPollInterval).Should(Succeed())
		})

		It("should report default CA package policy in status", func() {
			createTrustManager(newTrustManagerCR().WithDefaultCAPackage(v1alpha1.DefaultCAPackagePolicyEnabled))

			By("verifying status reports Enabled policy")
			Eventually(func(g Gomega) {
				tm, err := trustManagerClient().Get(ctx, "cluster", metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(tm.Status.DefaultCAPackagePolicy).Should(Equal(v1alpha1.DefaultCAPackagePolicyEnabled))
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("updating TrustManager CR to disable default CA package")
			Eventually(func() error {
				tm, err := trustManagerClient().Get(ctx, "cluster", metav1.GetOptions{})
				if err != nil {
					return err
				}
				tm.Spec.TrustManagerConfig.DefaultCAPackage.Policy = v1alpha1.DefaultCAPackagePolicyDisabled
				_, err = trustManagerClient().Update(ctx, tm, metav1.UpdateOptions{})
				return err
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("verifying status reports Disabled policy")
			Eventually(func(g Gomega) {
				tm, err := trustManagerClient().Get(ctx, "cluster", metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(tm.Status.DefaultCAPackagePolicy).Should(Equal(v1alpha1.DefaultCAPackagePolicyDisabled))
			}, lowTimeout, fastPollInterval).Should(Succeed())
		})

		// TODO: Add test for status reporting when custom configuration is applied
		// (e.g. filter expired certificates policy; secret targets, default CA package, and trust namespace are covered above.)
	})

	// -------------------------------------------------------------------------
	// Trust namespace configuration
	// -------------------------------------------------------------------------

	Context("trust namespace configuration", func() {
		It("should reject updates that change spec.trustNamespace", func() {
			By("creating TrustManager with explicit trust namespace")
			createTrustManager(newTrustManagerCR().WithTrustNamespace("cert-manager"))

			By("attempting to mutate spec.trustNamespace (field is immutable once set)")
			tm, err := trustManagerClient().Get(ctx, "cluster", metav1.GetOptions{})
			Expect(err).ShouldNot(HaveOccurred())
			tm.Spec.TrustManagerConfig.TrustNamespace = "other-trust-ns-immutable"
			_, err = trustManagerClient().Update(ctx, tm, metav1.UpdateOptions{})
			Expect(err).Should(HaveOccurred())
			Expect(errors.IsInvalid(err)).Should(BeTrue())
			Expect(err.Error()).Should(ContainSubstring("trustNamespace is immutable once set"))
		})

		It("should set degraded condition when trust namespace does not exist", func() {
			nonExistentNS := createUniqueNamespace("non-existent-trust")

			By("creating TrustManager CR with non-existent trust namespace")
			_, err := trustManagerClient().Create(ctx, newTrustManagerCR().WithTrustNamespace(nonExistentNS).Build(), metav1.CreateOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			By("verifying TrustManager becomes Degraded=True")
			Eventually(func(g Gomega) {
				tm, err := trustManagerClient().Get(ctx, "cluster", metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())

				degradedCondition := meta.FindStatusCondition(tm.Status.Conditions, v1alpha1.Degraded)
				g.Expect(degradedCondition).ShouldNot(BeNil())
				g.Expect(degradedCondition.Status).Should(Equal(metav1.ConditionTrue))
				g.Expect(degradedCondition.Reason).Should(Equal(v1alpha1.ReasonFailed))
				g.Expect(degradedCondition.Message).Should(And(
					ContainSubstring("trust namespace"),
					ContainSubstring(nonExistentNS),
					ContainSubstring("does not exist"),
				))

				readyCondition := meta.FindStatusCondition(tm.Status.Conditions, v1alpha1.Ready)
				g.Expect(readyCondition).ShouldNot(BeNil())
				g.Expect(readyCondition.Status).Should(Equal(metav1.ConditionFalse))
				g.Expect(readyCondition.Reason).Should(Equal(v1alpha1.ReasonFailed))
				// Irrecoverable path: Ready message is left empty; detail is on Degraded only
				// (see HandleReconcileResult for IsIrrecoverableError).
				g.Expect(readyCondition.Message).Should(BeEmpty())
			}, lowTimeout, fastPollInterval).Should(Succeed())

			// Irrecoverable errors are not requeued, and Namespace is not watched, so creating the
			// namespace alone does not reconcile. Create the namespace, then change TrustManager
			// spec (controllerConfig.annotations) to bump generation and enqueue a reconcile.
			By("creating the trust namespace that was previously missing")
			createAndDestroyTestNamespace(ctx, clientset, nonExistentNS)

			By("updating TrustManager to trigger reconciliation now that the namespace exists")
			tm, err := trustManagerClient().Get(ctx, "cluster", metav1.GetOptions{})
			Expect(err).ShouldNot(HaveOccurred())
			if tm.Spec.ControllerConfig.Annotations == nil {
				tm.Spec.ControllerConfig.Annotations = map[string]string{}
			}
			tm.Spec.ControllerConfig.Annotations["trustmanager.e2e.openshift.io/recovery"] = "true"
			_, err = trustManagerClient().Update(ctx, tm, metav1.UpdateOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			By("waiting for TrustManager to recover (Ready=True, not Degraded)")
			waitForTrustManagerReady()
		})
	})

	// -------------------------------------------------------------------------
	// Custom labels and annotations
	// -------------------------------------------------------------------------

	Context("custom labels and annotations", func() {
		It("should apply custom labels from controllerConfig to all managed resources", func() {
			By("creating TrustManager CR with custom labels")
			createTrustManager(newTrustManagerCR().WithLabels(map[string]string{
				"custom-label": "custom-value",
			}))

			verifyCustomLabelOnResource := func(name string, getLabels func() (map[string]string, error)) {
				By(fmt.Sprintf("verifying custom label on %s", name))
				Eventually(func(g Gomega) {
					labels, err := getLabels()
					g.Expect(err).ShouldNot(HaveOccurred())
					g.Expect(labels).Should(HaveKeyWithValue("custom-label", "custom-value"))
				}, lowTimeout, fastPollInterval).Should(Succeed())
			}

			verifyCustomLabelOnResource("ServiceAccount", func() (map[string]string, error) {
				sa, err := clientset.CoreV1().ServiceAccounts(trustManagerNamespace).Get(ctx, trustManagerServiceAccountName, metav1.GetOptions{})
				if err != nil {
					return nil, err
				}
				return sa.Labels, nil
			})

			verifyCustomLabelOnResource("Deployment", func() (map[string]string, error) {
				dep, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
				if err != nil {
					return nil, err
				}
				return dep.Labels, nil
			})

			verifyCustomLabelOnResource("ClusterRole", func() (map[string]string, error) {
				cr, err := clientset.RbacV1().ClusterRoles().Get(ctx, trustManagerClusterRoleName, metav1.GetOptions{})
				if err != nil {
					return nil, err
				}
				return cr.Labels, nil
			})

			verifyCustomLabelOnResource("webhook Service", func() (map[string]string, error) {
				svc, err := clientset.CoreV1().Services(trustManagerNamespace).Get(ctx, trustManagerServiceName, metav1.GetOptions{})
				if err != nil {
					return nil, err
				}
				return svc.Labels, nil
			})

			verifyCustomLabelOnResource("ValidatingWebhookConfiguration", func() (map[string]string, error) {
				vwc, err := clientset.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(ctx, trustManagerWebhookConfigName, metav1.GetOptions{})
				if err != nil {
					return nil, err
				}
				return vwc.Labels, nil
			})
		})

		It("should apply custom annotations from controllerConfig to managed resources", func() {
			By("creating TrustManager CR with custom annotations")
			createTrustManager(newTrustManagerCR().WithAnnotations(map[string]string{
				"custom-annotation": "annotation-value",
			}))

			By("verifying custom annotation on ServiceAccount")
			Eventually(func(g Gomega) {
				sa, err := clientset.CoreV1().ServiceAccounts(trustManagerNamespace).Get(ctx, trustManagerServiceAccountName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(sa.Annotations).Should(HaveKeyWithValue("custom-annotation", "annotation-value"))
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("verifying custom annotation on Deployment")
			Eventually(func(g Gomega) {
				dep, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(dep.Annotations).Should(HaveKeyWithValue("custom-annotation", "annotation-value"))
			}, lowTimeout, fastPollInterval).Should(Succeed())

			By("verifying custom annotation on webhook does not override cert-manager CA injection annotation")
			Eventually(func(g Gomega) {
				vwc, err := clientset.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(ctx, trustManagerWebhookConfigName, metav1.GetOptions{})
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(vwc.Annotations).Should(HaveKeyWithValue("custom-annotation", "annotation-value"))
				expectedCAAnnotation := fmt.Sprintf("%s/%s", trustManagerNamespace, trustManagerCertificateName)
				g.Expect(vwc.Annotations).Should(HaveKeyWithValue("cert-manager.io/inject-ca-from", expectedCAAnnotation))
			}, lowTimeout, fastPollInterval).Should(Succeed())
		})
	})

	// -------------------------------------------------------------------------
	// Singleton validation
	// -------------------------------------------------------------------------

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
			Expect(err.Error()).Should(ContainSubstring("TrustManager is a singleton"))
			Expect(err.Error()).Should(ContainSubstring(".metadata.name must be 'cluster'"))
		})
	})
})

// pollTillTrustManagerAvailable polls the TrustManager object and returns its status
// once the TrustManager is available, otherwise returns a time-out error.
func pollTillTrustManagerAvailable(ctx context.Context, client operatorclientv1alpha1.TrustManagerInterface, trustManagerName string) (v1alpha1.TrustManagerStatus, error) {
	var trustManagerStatus v1alpha1.TrustManagerStatus

	err := wait.PollUntilContextTimeout(ctx, slowPollInterval, highTimeout, true, func(context.Context) (bool, error) {
		trustManager, err := client.Get(ctx, trustManagerName, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		trustManagerStatus = trustManager.Status

		readyCondition := meta.FindStatusCondition(trustManagerStatus.Conditions, v1alpha1.Ready)
		if readyCondition == nil {
			return false, nil
		}

		degradedCondition := meta.FindStatusCondition(trustManagerStatus.Conditions, v1alpha1.Degraded)
		if degradedCondition != nil && degradedCondition.Status == metav1.ConditionTrue {
			return false, nil
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
	Expect(labels).Should(HaveKey("app.kubernetes.io/version"))
}

// verifyTrustManagerTLSVolume verifies that the volumes contain a "tls" volume
// referencing the expected TLS secret.
func verifyTrustManagerTLSVolume(volumes []corev1.Volume) {
	var found bool
	for _, vol := range volumes {
		if vol.Name == "tls" && vol.Secret != nil {
			Expect(vol.Secret.SecretName).Should(Equal(trustManagerTLSSecretName))
			found = true
			break
		}
	}
	Expect(found).Should(BeTrue(), "TLS volume with correct secret name not found")
}

// verifyTrustManagerDefaultContainerArgs verifies that the deployment container args
// contain all the expected default flags for a trust-manager deployment.
func verifyTrustManagerDefaultContainerArgs(args []string) {
	Expect(args).Should(ContainElement("--log-format=text"))
	Expect(args).Should(ContainElement("--log-level=1"))
	Expect(args).Should(ContainElement("--metrics-port=9402"))
	Expect(args).Should(ContainElement("--readiness-probe-port=6060"))
	Expect(args).Should(ContainElement("--readiness-probe-path=/readyz"))
	Expect(args).Should(ContainElement("--leader-elect=true"))
	Expect(args).Should(ContainElement("--leader-election-lease-duration=15s"))
	Expect(args).Should(ContainElement("--leader-election-renew-deadline=10s"))
	Expect(args).Should(ContainElement("--trust-namespace=cert-manager"))
	Expect(args).Should(ContainElement("--webhook-host=0.0.0.0"))
	Expect(args).Should(ContainElement("--webhook-port=6443"))
	Expect(args).Should(ContainElement("--webhook-certificate-dir=/tls"))
}

// verifyTrustManagerResourceRecreation deletes a resource and verifies it is recreated
// by the controller within the timeout period.
func verifyTrustManagerResourceRecreation(deleteFunc func() error, getFunc func() error) {
	err := deleteFunc()
	Expect(err).ShouldNot(HaveOccurred())

	Eventually(func() error {
		return getFunc()
	}, lowTimeout, fastPollInterval).Should(Succeed(), "resource was not recreated by controller")
}

// hasSecretRule returns true if any rule in the list targets the "secrets" resource.
func hasSecretRule(rules []rbacv1.PolicyRule) bool {
	for _, rule := range rules {
		if slices.Contains(rule.Resources, "secrets") {
			return true
		}
	}
	return false
}

// findSecretRule returns the first rule targeting "secrets" that contains the given verb,
// or nil if no matching rule is found.
func findSecretRule(rules []rbacv1.PolicyRule, verb string) *rbacv1.PolicyRule {
	for i, rule := range rules {
		if slices.Contains(rule.Resources, "secrets") && slices.Contains(rule.Verbs, verb) {
			return &rules[i]
		}
	}
	return nil
}
