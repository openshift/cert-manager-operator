//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

// TrustManagerConfig holds the template values for the trust-manager CR template.
type TrustManagerConfig struct {
	LogLevel                  int32
	LogFormat                 string
	TrustNamespace            string
	FilterExpiredCertificates string
	SecretTargetsPolicy       string
	AuthorizedSecrets         []string
	DefaultCAPackagePolicy    string
}

var _ = Describe("TrustManager", Ordered, Label("Feature:TrustManager"), func() {
	ctx := context.TODO()
	var clientset *kubernetes.Clientset

	const (
		trustManagerDeploymentName    = "trust-manager"
		trustManagerServiceAccount    = "trust-manager"
		trustManagerNamespace         = "cert-manager"
		trustManagerClusterRoleName   = "trust-manager"
		trustManagerCRBName           = "trust-manager"
		trustManagerLeasesRoleName    = "trust-manager-leases"
		trustManagerLeasesRBName      = "trust-manager-leases"
		trustManagerMetricsService    = "trust-manager-metrics"
		trustManagerSecretTargetsCR   = "trust-manager-secret-targets"
		trustManagerSecretTargetsCRB  = "trust-manager-secret-targets"
		defaultCAPackageConfigMapName = "trust-manager-default-package"
	)

	defaultTrustManagerConfig := func() TrustManagerConfig {
		return TrustManagerConfig{
			LogLevel:  1,
			LogFormat: "text",
		}
	}

	createTrustManagerCR := func(tmConfig TrustManagerConfig) {
		By("creating trustmanager.operator.openshift.io resource")
		loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(tmConfig),
			filepath.Join("testdata", "trust_manager", "trust_manager_template.yaml"), "")
	}

	deleteTrustManagerCR := func(tmConfig TrustManagerConfig) {
		By("deleting trustmanager.operator.openshift.io resource")
		loader.DeleteFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(tmConfig),
			filepath.Join("testdata", "trust_manager", "trust_manager_template.yaml"), "")
	}

	waitForTrustManagerReady := func() {
		By("waiting for trust-manager deployment to be available")
		err := pollTillDeploymentAvailable(ctx, clientset, trustManagerNamespace, trustManagerDeploymentName)
		Expect(err).Should(BeNil())

		By("waiting for TrustManager CR to become Ready")
		err = pollTillTrustManagerReady(ctx)
		Expect(err).Should(BeNil())
	}

	waitForTrustManagerDeleted := func() {
		By("waiting for trust-manager deployment to be removed")
		err := pollTillResourceDeleted(ctx, func() error {
			_, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
			return err
		})
		Expect(err).Should(BeNil())
	}

	BeforeAll(func() {
		var err error
		clientset, err = kubernetes.NewForConfig(cfg)
		Expect(err).Should(BeNil())

		By("increase operator log verbosity")
		err = patchSubscriptionWithEnvVars(ctx, loader, map[string]string{
			"OPERATOR_LOG_LEVEL": "5",
		})
		Expect(err).NotTo(HaveOccurred())
	})

	BeforeEach(func() {
		By("waiting for operator status to become available")
		err := VerifyHealthyOperatorConditions(certmanageroperatorclient.OperatorV1alpha1())
		Expect(err).NotTo(HaveOccurred(), "Operator is expected to be available")
	})

	Context("basic lifecycle", func() {
		AfterEach(func() {
			tmConfig := defaultTrustManagerConfig()
			deleteTrustManagerCR(tmConfig)

			By("waiting for trust-manager resources to be cleaned up")
			waitForTrustManagerDeleted()

			By("cleaning up cluster-scoped RBAC resources")
			clientset.RbacV1().ClusterRoles().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
				LabelSelector: "app.kubernetes.io/name=trust-manager",
			})
			clientset.RbacV1().ClusterRoleBindings().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
				LabelSelector: "app.kubernetes.io/name=trust-manager",
			})
		})

		It("should create all trust-manager resources when TrustManager CR is created", func() {
			tmConfig := defaultTrustManagerConfig()
			createTrustManagerCR(tmConfig)
			waitForTrustManagerReady()

			By("verifying the deployment exists in the cert-manager namespace")
			deployment, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
			Expect(err).Should(BeNil())
			Expect(deployment.Spec.Template.Spec.Containers).Should(HaveLen(1))
			Expect(deployment.Spec.Template.Spec.Containers[0].Name).Should(Equal(trustManagerDeploymentName))

			By("verifying the service account exists")
			_, err = clientset.CoreV1().ServiceAccounts(trustManagerNamespace).Get(ctx, trustManagerServiceAccount, metav1.GetOptions{})
			Expect(err).Should(BeNil())

			By("verifying the cluster role exists")
			_, err = clientset.RbacV1().ClusterRoles().Get(ctx, trustManagerClusterRoleName, metav1.GetOptions{})
			Expect(err).Should(BeNil())

			By("verifying the cluster role binding exists")
			_, err = clientset.RbacV1().ClusterRoleBindings().Get(ctx, trustManagerCRBName, metav1.GetOptions{})
			Expect(err).Should(BeNil())

			By("verifying the leases role exists")
			_, err = clientset.RbacV1().Roles(trustManagerNamespace).Get(ctx, trustManagerLeasesRoleName, metav1.GetOptions{})
			Expect(err).Should(BeNil())

			By("verifying the leases role binding exists")
			_, err = clientset.RbacV1().RoleBindings(trustManagerNamespace).Get(ctx, trustManagerLeasesRBName, metav1.GetOptions{})
			Expect(err).Should(BeNil())

			By("verifying the metrics service exists")
			_, err = clientset.CoreV1().Services(trustManagerNamespace).Get(ctx, trustManagerMetricsService, metav1.GetOptions{})
			Expect(err).Should(BeNil())
		})

		It("should set Ready condition to True on successful deployment", func() {
			tmConfig := defaultTrustManagerConfig()
			createTrustManagerCR(tmConfig)
			waitForTrustManagerReady()

			By("fetching the TrustManager CR")
			tm, err := certmanageroperatorclient.OperatorV1alpha1().TrustManagers().Get(ctx, "cluster", metav1.GetOptions{})
			Expect(err).Should(BeNil())

			By("checking Ready condition")
			readyCondition := meta.FindStatusCondition(tm.Status.Conditions, v1alpha1.Ready)
			Expect(readyCondition).ShouldNot(BeNil())
			Expect(readyCondition.Status).Should(Equal(metav1.ConditionTrue))

			By("checking Degraded condition is False")
			degradedCondition := meta.FindStatusCondition(tm.Status.Conditions, v1alpha1.Degraded)
			if degradedCondition != nil {
				Expect(degradedCondition.Status).Should(Equal(metav1.ConditionFalse))
			}

			By("checking status fields are populated")
			Expect(tm.Status.TrustManagerImage).ShouldNot(BeEmpty())
		})

		It("should clean up all resources when TrustManager CR is deleted", func() {
			tmConfig := defaultTrustManagerConfig()
			createTrustManagerCR(tmConfig)
			waitForTrustManagerReady()

			By("deleting the TrustManager CR")
			deleteTrustManagerCR(tmConfig)
			waitForTrustManagerDeleted()

			By("verifying the deployment is removed")
			_, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
			Expect(apierrors.IsNotFound(err)).Should(BeTrue(), "deployment should be deleted")

			By("verifying the service account is removed")
			_, err = clientset.CoreV1().ServiceAccounts(trustManagerNamespace).Get(ctx, trustManagerServiceAccount, metav1.GetOptions{})
			Expect(apierrors.IsNotFound(err)).Should(BeTrue(), "service account should be deleted")

			By("verifying the cluster role is removed")
			Eventually(func() bool {
				_, err := clientset.RbacV1().ClusterRoles().Get(ctx, trustManagerClusterRoleName, metav1.GetOptions{})
				return apierrors.IsNotFound(err)
			}, lowTimeout, fastPollInterval).Should(BeTrue(), "cluster role should be deleted")

			By("verifying the cluster role binding is removed")
			Eventually(func() bool {
				_, err := clientset.RbacV1().ClusterRoleBindings().Get(ctx, trustManagerCRBName, metav1.GetOptions{})
				return apierrors.IsNotFound(err)
			}, lowTimeout, fastPollInterval).Should(BeTrue(), "cluster role binding should be deleted")
		})
	})

	Context("deployment reconciliation", func() {
		AfterEach(func() {
			tmConfig := defaultTrustManagerConfig()
			deleteTrustManagerCR(tmConfig)
			waitForTrustManagerDeleted()

			clientset.RbacV1().ClusterRoles().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
				LabelSelector: "app.kubernetes.io/name=trust-manager",
			})
			clientset.RbacV1().ClusterRoleBindings().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
				LabelSelector: "app.kubernetes.io/name=trust-manager",
			})
		})

		It("should reconcile the deployment back to desired state if manually modified", func() {
			tmConfig := defaultTrustManagerConfig()
			createTrustManagerCR(tmConfig)
			waitForTrustManagerReady()

			By("manually scaling down the deployment to zero replicas")
			deployment, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
			Expect(err).Should(BeNil())

			modifiedDeployment := deployment.DeepCopy()
			zero := int32(0)
			modifiedDeployment.Spec.Replicas = &zero
			_, err = clientset.AppsV1().Deployments(trustManagerNamespace).Update(ctx, modifiedDeployment, metav1.UpdateOptions{})
			Expect(err).Should(BeNil())

			By("verifying the deployment is reconciled back to 1 replica")
			Eventually(func() bool {
				dep, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				return dep.Spec.Replicas != nil && *dep.Spec.Replicas == 1
			}, lowTimeout, fastPollInterval).Should(BeTrue(), "deployment should be reconciled back to 1 replica")

			By("waiting for the deployment to become available again")
			err = pollTillDeploymentAvailable(ctx, clientset, trustManagerNamespace, trustManagerDeploymentName)
			Expect(err).Should(BeNil())
		})

		It("should pass configured log level and format to the deployment", func() {
			tmConfig := TrustManagerConfig{
				LogLevel:  3,
				LogFormat: "json",
			}
			createTrustManagerCR(tmConfig)
			defer deleteTrustManagerCR(tmConfig)
			waitForTrustManagerReady()

			By("verifying the deployment has the correct log level and format args")
			Eventually(func() bool {
				dep, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
				if err != nil || len(dep.Spec.Template.Spec.Containers) == 0 {
					return false
				}
				args := dep.Spec.Template.Spec.Containers[0].Args
				hasLogLevel := false
				hasLogFormat := false
				for _, arg := range args {
					if arg == "--log-level=3" {
						hasLogLevel = true
					}
					if arg == "--log-format=json" {
						hasLogFormat = true
					}
				}
				return hasLogLevel && hasLogFormat
			}, lowTimeout, fastPollInterval).Should(BeTrue(), "deployment should have --log-level=3 and --log-format=json args")
		})
	})

	Context("secret targets policy", func() {
		AfterEach(func() {
			// Clean up any TrustManager CR
			tmConfig := defaultTrustManagerConfig()
			deleteTrustManagerCR(tmConfig)
			waitForTrustManagerDeleted()

			// Clean up cluster-scoped resources
			clientset.RbacV1().ClusterRoles().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
				LabelSelector: "app.kubernetes.io/name=trust-manager",
			})
			clientset.RbacV1().ClusterRoleBindings().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
				LabelSelector: "app.kubernetes.io/name=trust-manager",
			})
		})

		It("should create secret targets RBAC when policy is Custom", func() {
			tmConfig := TrustManagerConfig{
				LogLevel:            1,
				LogFormat:           "text",
				SecretTargetsPolicy: "Custom",
				AuthorizedSecrets:   []string{"my-secret"},
			}
			createTrustManagerCR(tmConfig)
			defer deleteTrustManagerCR(tmConfig)
			waitForTrustManagerReady()

			By("verifying secret-targets ClusterRole is created")
			Eventually(func() error {
				_, err := clientset.RbacV1().ClusterRoles().Get(ctx, trustManagerSecretTargetsCR, metav1.GetOptions{})
				return err
			}, lowTimeout, fastPollInterval).Should(Succeed(), "secret-targets ClusterRole should exist")

			By("verifying secret-targets ClusterRoleBinding is created")
			Eventually(func() error {
				_, err := clientset.RbacV1().ClusterRoleBindings().Get(ctx, trustManagerSecretTargetsCRB, metav1.GetOptions{})
				return err
			}, lowTimeout, fastPollInterval).Should(Succeed(), "secret-targets ClusterRoleBinding should exist")

			By("verifying deployment has --secret-targets-enabled arg")
			Eventually(func() bool {
				dep, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
				if err != nil || len(dep.Spec.Template.Spec.Containers) == 0 {
					return false
				}
				for _, arg := range dep.Spec.Template.Spec.Containers[0].Args {
					if arg == "--secret-targets-enabled=true" {
						return true
					}
				}
				return false
			}, lowTimeout, fastPollInterval).Should(BeTrue(), "deployment should have --secret-targets-enabled=true")
		})

		It("should not create secret targets RBAC when policy is Disabled", func() {
			tmConfig := defaultTrustManagerConfig()
			createTrustManagerCR(tmConfig)
			defer deleteTrustManagerCR(tmConfig)
			waitForTrustManagerReady()

			By("verifying secret-targets ClusterRole does NOT exist")
			Consistently(func() bool {
				_, err := clientset.RbacV1().ClusterRoles().Get(ctx, trustManagerSecretTargetsCR, metav1.GetOptions{})
				return apierrors.IsNotFound(err)
			}, "30s", fastPollInterval).Should(BeTrue(), "secret-targets ClusterRole should not exist when policy is Disabled")
		})
	})

	Context("default CA package", func() {
		AfterEach(func() {
			tmConfig := defaultTrustManagerConfig()
			deleteTrustManagerCR(tmConfig)
			waitForTrustManagerDeleted()

			clientset.RbacV1().ClusterRoles().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
				LabelSelector: "app.kubernetes.io/name=trust-manager",
			})
			clientset.RbacV1().ClusterRoleBindings().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
				LabelSelector: "app.kubernetes.io/name=trust-manager",
			})
		})

		It("should create CA package ConfigMap with injection annotation when enabled", func() {
			tmConfig := TrustManagerConfig{
				LogLevel:               1,
				LogFormat:              "text",
				DefaultCAPackagePolicy: "Enabled",
			}
			createTrustManagerCR(tmConfig)
			defer deleteTrustManagerCR(tmConfig)
			waitForTrustManagerReady()

			By("verifying default CA package ConfigMap exists")
			Eventually(func() error {
				cm, err := clientset.CoreV1().ConfigMaps(trustManagerNamespace).Get(ctx, defaultCAPackageConfigMapName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				By("checking ConfigMap has the trusted CA bundle injection annotation")
				annotations := cm.GetAnnotations()
				val, exists := annotations["config.openshift.io/inject-trusted-cabundle"]
				if !exists || val != "true" {
					return fmt.Errorf("expected annotation config.openshift.io/inject-trusted-cabundle=true, got: %v", annotations)
				}
				return nil
			}, lowTimeout, fastPollInterval).Should(Succeed(), "default CA package ConfigMap should exist with injection annotation")

			By("verifying the deployment has the --default-package-location arg")
			Eventually(func() bool {
				dep, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
				if err != nil || len(dep.Spec.Template.Spec.Containers) == 0 {
					return false
				}
				for _, arg := range dep.Spec.Template.Spec.Containers[0].Args {
					if arg == "--default-package-location=/var/run/trust-manager/default-package/ca-bundle.crt" {
						return true
					}
				}
				return false
			}, lowTimeout, fastPollInterval).Should(BeTrue(), "deployment should have --default-package-location arg")
		})

		It("should not create CA package ConfigMap when disabled", func() {
			tmConfig := defaultTrustManagerConfig()
			createTrustManagerCR(tmConfig)
			defer deleteTrustManagerCR(tmConfig)
			waitForTrustManagerReady()

			By("verifying default CA package ConfigMap does NOT exist")
			Consistently(func() bool {
				_, err := clientset.CoreV1().ConfigMaps(trustManagerNamespace).Get(ctx, defaultCAPackageConfigMapName, metav1.GetOptions{})
				return apierrors.IsNotFound(err)
			}, "30s", fastPollInterval).Should(BeTrue(), "default CA package ConfigMap should not exist when disabled")
		})
	})

	Context("filter expired certificates", func() {
		AfterEach(func() {
			tmConfig := defaultTrustManagerConfig()
			deleteTrustManagerCR(tmConfig)
			waitForTrustManagerDeleted()

			clientset.RbacV1().ClusterRoles().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
				LabelSelector: "app.kubernetes.io/name=trust-manager",
			})
			clientset.RbacV1().ClusterRoleBindings().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
				LabelSelector: "app.kubernetes.io/name=trust-manager",
			})
		})

		It("should pass --filter-expired-certs=true when policy is Enabled", func() {
			tmConfig := TrustManagerConfig{
				LogLevel:                  1,
				LogFormat:                 "text",
				FilterExpiredCertificates: "Enabled",
			}
			createTrustManagerCR(tmConfig)
			defer deleteTrustManagerCR(tmConfig)
			waitForTrustManagerReady()

			By("verifying the deployment has --filter-expired-certs=true arg")
			Eventually(func() bool {
				dep, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
				if err != nil || len(dep.Spec.Template.Spec.Containers) == 0 {
					return false
				}
				for _, arg := range dep.Spec.Template.Spec.Containers[0].Args {
					if arg == "--filter-expired-certs=true" {
						return true
					}
				}
				return false
			}, lowTimeout, fastPollInterval).Should(BeTrue(), "deployment should have --filter-expired-certs=true")
		})

		It("should pass --filter-expired-certs=false when policy is Disabled", func() {
			tmConfig := defaultTrustManagerConfig()
			createTrustManagerCR(tmConfig)
			defer deleteTrustManagerCR(tmConfig)
			waitForTrustManagerReady()

			By("verifying the deployment has --filter-expired-certs=false arg")
			Eventually(func() bool {
				dep, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
				if err != nil || len(dep.Spec.Template.Spec.Containers) == 0 {
					return false
				}
				for _, arg := range dep.Spec.Template.Spec.Containers[0].Args {
					if arg == "--filter-expired-certs=false" {
						return true
					}
				}
				return false
			}, lowTimeout, fastPollInterval).Should(BeTrue(), "deployment should have --filter-expired-certs=false")
		})
	})
})

// pollTillTrustManagerReady polls the TrustManager CR until it has a Ready=True condition.
func pollTillTrustManagerReady(ctx context.Context) error {
	return wait.PollUntilContextTimeout(ctx, slowPollInterval, highTimeout, true, func(context.Context) (bool, error) {
		tm, err := certmanageroperatorclient.OperatorV1alpha1().TrustManagers().Get(ctx, "cluster", metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}

		readyCondition := meta.FindStatusCondition(tm.Status.Conditions, v1alpha1.Ready)
		if readyCondition == nil {
			return false, nil
		}

		degradedCondition := meta.FindStatusCondition(tm.Status.Conditions, v1alpha1.Degraded)
		if degradedCondition != nil && degradedCondition.Status == metav1.ConditionTrue {
			return false, fmt.Errorf("TrustManager is degraded: %s", degradedCondition.Message)
		}

		return readyCondition.Status == metav1.ConditionTrue, nil
	})
}

// pollTillResourceDeleted polls until a get function returns IsNotFound.
func pollTillResourceDeleted(ctx context.Context, getFn func() error) error {
	return wait.PollUntilContextTimeout(ctx, fastPollInterval, highTimeout, true, func(context.Context) (bool, error) {
		err := getFn()
		if err != nil {
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	})
}
