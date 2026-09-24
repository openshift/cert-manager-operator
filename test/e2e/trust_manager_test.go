//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"fmt"
	"path/filepath"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/test/library"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var trustmanagerSchema = schema.GroupVersionResource{
	Group:    "operator.openshift.io",
	Version:  "v1alpha1",
	Resource: "trustmanagers",
}

// TrustManagerTemplateConfig provides template values for the TrustManager CR YAML template.
type TrustManagerTemplateConfig struct {
	TrustNamespace            string
	SecretTargetsPolicy       string
	AuthorizedSecrets         []string
	DefaultCAPackagePolicy    string
	FilterExpiredCertificates string
}

var _ = Describe("TrustManager", Ordered, Label("Feature:TrustManager"), func() {
	ctx := context.TODO()
	var clientset *kubernetes.Clientset

	const (
		trustManagerDeploymentName = "trust-manager"
		trustManagerNamespace      = "cert-manager"
		trustManagerServiceAccount = "trust-manager"
	)

	waitForTrustManagerReady := func() v1alpha1.TrustManagerStatus {
		By("poll till trust-manager deployment is available")
		err := pollTillDeploymentAvailable(ctx, clientset, trustManagerNamespace, trustManagerDeploymentName)
		Expect(err).Should(BeNil())

		By("poll till trustmanager object is available")
		trustManagerStatus, err := pollTillTrustManagerAvailable(ctx, loader, "cluster")
		Expect(err).Should(BeNil())

		return trustManagerStatus
	}

	cleanupTrustManagerClusterResources := func() {
		By("deleting cluster-scoped RBAC resources of the trust-manager agent")
		clientset.RbacV1().ClusterRoles().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/name=trust-manager",
		})
		clientset.RbacV1().ClusterRoleBindings().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/name=trust-manager",
		})
		// Clean up secret-targets RBAC if present
		clientset.RbacV1().ClusterRoles().Delete(ctx, "trust-manager-secret-targets", metav1.DeleteOptions{})
		clientset.RbacV1().ClusterRoleBindings().Delete(ctx, "trust-manager-secret-targets", metav1.DeleteOptions{})

		By("deleting validating webhook configuration")
		clientset.AdmissionregistrationV1().ValidatingWebhookConfigurations().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
			LabelSelector: "app=trust-manager",
		})
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

	Context("basic TrustManager CR lifecycle", func() {
		It("should deploy trust-manager operand resources when TrustManager CR is created", func() {
			By("creating TrustManager CR with default configuration")
			templateConfig := TrustManagerTemplateConfig{}
			loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
				templateConfig,
			), filepath.Join("testdata", "trustmanager", "trustmanager_template.yaml"), "")
			DeferCleanup(func() {
				loader.DeleteFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
					templateConfig,
				), filepath.Join("testdata", "trustmanager", "trustmanager_template.yaml"), "")
				cleanupTrustManagerClusterResources()
			})

			trustManagerStatus := waitForTrustManagerReady()

			By("verifying trust-manager image is set in status")
			Expect(trustManagerStatus.TrustManagerImage).ShouldNot(BeEmpty(), "trust-manager image should be set in status")

			By("verifying trust-manager deployment exists in cert-manager namespace")
			deployment, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
			Expect(err).Should(BeNil())
			Expect(deployment.Status.AvailableReplicas).Should(BeNumerically(">=", 1))

			By("verifying trust-manager service account exists")
			err = pollTillServiceAccountAvailable(ctx, clientset, trustManagerNamespace, trustManagerServiceAccount)
			Expect(err).Should(BeNil())

			By("verifying trust-manager ClusterRole exists")
			_, err = clientset.RbacV1().ClusterRoles().Get(ctx, "trust-manager", metav1.GetOptions{})
			Expect(err).Should(BeNil())

			By("verifying trust-manager ClusterRoleBinding exists")
			_, err = clientset.RbacV1().ClusterRoleBindings().Get(ctx, "trust-manager", metav1.GetOptions{})
			Expect(err).Should(BeNil())

			By("verifying trust-manager Role exists in cert-manager namespace")
			_, err = clientset.RbacV1().Roles(trustManagerNamespace).Get(ctx, "trust-manager", metav1.GetOptions{})
			Expect(err).Should(BeNil())

			By("verifying trust-manager RoleBinding exists in cert-manager namespace")
			_, err = clientset.RbacV1().RoleBindings(trustManagerNamespace).Get(ctx, "trust-manager", metav1.GetOptions{})
			Expect(err).Should(BeNil())

			By("verifying trust-manager webhook service exists")
			_, err = clientset.CoreV1().Services(trustManagerNamespace).Get(ctx, "trust-manager-webhook", metav1.GetOptions{})
			Expect(err).Should(BeNil())

			By("verifying trust-manager metrics service exists")
			_, err = clientset.CoreV1().Services(trustManagerNamespace).Get(ctx, "trust-manager-metrics", metav1.GetOptions{})
			Expect(err).Should(BeNil())

			By("verifying ValidatingWebhookConfiguration exists")
			Eventually(func() error {
				_, err := clientset.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(ctx, "trust-manager", metav1.GetOptions{})
				return err
			}, highTimeout, slowPollInterval).Should(Succeed())

			By("verifying TrustManager status conditions")
			Eventually(func() bool {
				status, err := pollTillTrustManagerAvailable(ctx, loader, "cluster")
				if err != nil {
					return false
				}
				readyCondition := meta.FindStatusCondition(status.Conditions, v1alpha1.Ready)
				return readyCondition != nil && readyCondition.Status == metav1.ConditionTrue
			}, highTimeout, slowPollInterval).Should(BeTrue(), "TrustManager should have Ready=True condition")
		})

		It("should reconcile trust-manager deployment back to desired state when manually modified", func() {
			By("creating TrustManager CR")
			templateConfig := TrustManagerTemplateConfig{}
			loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
				templateConfig,
			), filepath.Join("testdata", "trustmanager", "trustmanager_template.yaml"), "")
			DeferCleanup(func() {
				loader.DeleteFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
					templateConfig,
				), filepath.Join("testdata", "trustmanager", "trustmanager_template.yaml"), "")
				cleanupTrustManagerClusterResources()
			})

			waitForTrustManagerReady()

			By("manually scaling down trust-manager deployment to 0 replicas")
			deployment, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
			Expect(err).Should(BeNil())
			replicas := int32(0)
			deployment.Spec.Replicas = &replicas
			_, err = clientset.AppsV1().Deployments(trustManagerNamespace).Update(ctx, deployment, metav1.UpdateOptions{})
			Expect(err).Should(BeNil())

			By("verifying trust-manager deployment is reconciled back to desired state")
			Eventually(func() bool {
				dep, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				return dep.Spec.Replicas != nil && *dep.Spec.Replicas >= 1
			}, highTimeout, slowPollInterval).Should(BeTrue(), "trust-manager deployment should be reconciled back to desired replicas")
		})
	})

	Context("with DefaultCAPackage enabled", func() {
		It("should create default CA package ConfigMap with CNO injection annotation", func() {
			By("creating TrustManager CR with DefaultCAPackage enabled")
			templateConfig := TrustManagerTemplateConfig{DefaultCAPackagePolicy: "Enabled"}
			loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
				templateConfig,
			), filepath.Join("testdata", "trustmanager", "trustmanager_template.yaml"), "")
			DeferCleanup(func() {
				loader.DeleteFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
					templateConfig,
				), filepath.Join("testdata", "trustmanager", "trustmanager_template.yaml"), "")
				cleanupTrustManagerClusterResources()
				clientset.CoreV1().ConfigMaps(trustManagerNamespace).Delete(ctx, "trust-manager-default-ca-package", metav1.DeleteOptions{})
			})

			waitForTrustManagerReady()

			By("verifying default CA package ConfigMap exists with CNO annotation")
			Eventually(func() error {
				cm, err := clientset.CoreV1().ConfigMaps(trustManagerNamespace).Get(ctx, "trust-manager-default-ca-package", metav1.GetOptions{})
				if err != nil {
					return err
				}

				annotation, exists := cm.Annotations["config.openshift.io/inject-trusted-cabundle"]
				if !exists || annotation != "true" {
					return fmt.Errorf("expected CNO injection annotation to be 'true', got: %q (exists=%v)", annotation, exists)
				}
				return nil
			}, highTimeout, slowPollInterval).Should(Succeed(), "default CA package ConfigMap should exist with CNO annotation")

			By("verifying trust-manager deployment has default-ca-package volume mount")
			Eventually(func() error {
				deployment, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				// Check volume exists
				volumeFound := false
				for _, vol := range deployment.Spec.Template.Spec.Volumes {
					if vol.Name == "default-ca-package" {
						if vol.ConfigMap == nil {
							return fmt.Errorf("volume default-ca-package is not a ConfigMap volume")
						}
						if vol.ConfigMap.Name != "trust-manager-default-ca-package" {
							return fmt.Errorf("volume references wrong ConfigMap: got %s, want trust-manager-default-ca-package", vol.ConfigMap.Name)
						}
						volumeFound = true
						break
					}
				}
				if !volumeFound {
					return fmt.Errorf("volume default-ca-package not found")
				}

				// Check volume mount exists
				for _, container := range deployment.Spec.Template.Spec.Containers {
					if container.Name == "trust-manager" {
						for _, vm := range container.VolumeMounts {
							if vm.Name == "default-ca-package" {
								if vm.MountPath != "/var/run/configmaps/default-ca-package" {
									return fmt.Errorf("volume mount has wrong path: got %s", vm.MountPath)
								}
								return nil
							}
						}
						return fmt.Errorf("volume mount default-ca-package not found in container")
					}
				}
				return fmt.Errorf("container trust-manager not found")
			}, highTimeout, slowPollInterval).Should(Succeed(), "deployment should have default CA package volume mount")

			By("verifying trust-manager deployment args include --default-package-location")
			Eventually(func() bool {
				deployment, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				for _, container := range deployment.Spec.Template.Spec.Containers {
					if container.Name == "trust-manager" {
						for _, arg := range container.Args {
							if arg == "--default-package-location=/var/run/configmaps/default-ca-package/ca-certificates.crt" {
								return true
							}
						}
					}
				}
				return false
			}, highTimeout, slowPollInterval).Should(BeTrue(), "deployment should have --default-package-location arg")
		})

		It("should reconcile default CA package ConfigMap when manually deleted", func() {
			By("creating TrustManager CR with DefaultCAPackage enabled")
			templateConfig := TrustManagerTemplateConfig{DefaultCAPackagePolicy: "Enabled"}
			loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
				templateConfig,
			), filepath.Join("testdata", "trustmanager", "trustmanager_template.yaml"), "")
			DeferCleanup(func() {
				loader.DeleteFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
					templateConfig,
				), filepath.Join("testdata", "trustmanager", "trustmanager_template.yaml"), "")
				cleanupTrustManagerClusterResources()
				clientset.CoreV1().ConfigMaps(trustManagerNamespace).Delete(ctx, "trust-manager-default-ca-package", metav1.DeleteOptions{})
			})

			waitForTrustManagerReady()

			By("waiting for default CA package ConfigMap to exist")
			err := pollTillConfigMapAvailable(ctx, clientset, trustManagerNamespace, "trust-manager-default-ca-package")
			Expect(err).Should(BeNil())

			By("manually deleting the default CA package ConfigMap")
			err = clientset.CoreV1().ConfigMaps(trustManagerNamespace).Delete(ctx, "trust-manager-default-ca-package", metav1.DeleteOptions{})
			Expect(err).Should(BeNil())

			By("verifying the ConfigMap is re-created by the controller")
			err = pollTillConfigMapAvailable(ctx, clientset, trustManagerNamespace, "trust-manager-default-ca-package")
			Expect(err).Should(BeNil(), "default CA package ConfigMap should be re-created after deletion")
		})
	})

	Context("with SecretTargets policy", func() {
		It("should create secret-targets RBAC when SecretTargets policy is Custom", func() {
			By("creating TrustManager CR with SecretTargets Custom policy")
			templateConfig := TrustManagerTemplateConfig{
				SecretTargetsPolicy: "Custom",
				AuthorizedSecrets:   []string{"my-trust-bundle-secret"},
			}
			loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
				templateConfig,
			), filepath.Join("testdata", "trustmanager", "trustmanager_template.yaml"), "")
			DeferCleanup(func() {
				loader.DeleteFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
					templateConfig,
				), filepath.Join("testdata", "trustmanager", "trustmanager_template.yaml"), "")
				cleanupTrustManagerClusterResources()
			})

			waitForTrustManagerReady()

			By("verifying trust-manager deployment args include --secret-targets-enabled=true")
			Eventually(func() bool {
				deployment, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				for _, container := range deployment.Spec.Template.Spec.Containers {
					if container.Name == "trust-manager" {
						for _, arg := range container.Args {
							if arg == "--secret-targets-enabled=true" {
								return true
							}
						}
					}
				}
				return false
			}, highTimeout, slowPollInterval).Should(BeTrue(), "deployment should have --secret-targets-enabled=true arg")

			By("verifying secret-targets ClusterRole exists with authorized secret names")
			Eventually(func() error {
				cr, err := clientset.RbacV1().ClusterRoles().Get(ctx, "trust-manager-secret-targets", metav1.GetOptions{})
				if err != nil {
					return err
				}
				// Verify rules contain the authorized secret names
				for _, rule := range cr.Rules {
					for _, resource := range rule.Resources {
						if resource == "secrets" {
							for _, name := range rule.ResourceNames {
								if name == "my-trust-bundle-secret" {
									return nil
								}
							}
						}
					}
				}
				return fmt.Errorf("ClusterRole does not contain authorized secret name 'my-trust-bundle-secret'")
			}, highTimeout, slowPollInterval).Should(Succeed(), "secret-targets ClusterRole should have authorized secrets")

			By("verifying secret-targets ClusterRoleBinding exists")
			Eventually(func() error {
				_, err := clientset.RbacV1().ClusterRoleBindings().Get(ctx, "trust-manager-secret-targets", metav1.GetOptions{})
				return err
			}, highTimeout, slowPollInterval).Should(Succeed(), "secret-targets ClusterRoleBinding should exist")
		})
	})

	Context("with FilterExpiredCertificates enabled", func() {
		It("should pass --filter-expired-certificates=true to trust-manager deployment", func() {
			By("creating TrustManager CR with FilterExpiredCertificates enabled")
			templateConfig := TrustManagerTemplateConfig{FilterExpiredCertificates: "Enabled"}
			loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
				templateConfig,
			), filepath.Join("testdata", "trustmanager", "trustmanager_template.yaml"), "")
			DeferCleanup(func() {
				loader.DeleteFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
					templateConfig,
				), filepath.Join("testdata", "trustmanager", "trustmanager_template.yaml"), "")
				cleanupTrustManagerClusterResources()
			})

			waitForTrustManagerReady()

			By("verifying trust-manager deployment args include --filter-expired-certificates=true")
			Eventually(func() bool {
				deployment, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				for _, container := range deployment.Spec.Template.Spec.Containers {
					if container.Name == "trust-manager" {
						for _, arg := range container.Args {
							if arg == "--filter-expired-certificates=true" {
								return true
							}
						}
					}
				}
				return false
			}, highTimeout, slowPollInterval).Should(BeTrue(), "deployment should have --filter-expired-certificates=true arg")
		})
	})

	Context("resource cleanup on TrustManager CR deletion", func() {
		It("should remove the finalizer and allow CR deletion", func() {
			By("creating TrustManager CR")
			templateConfig := TrustManagerTemplateConfig{}
			loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
				templateConfig,
			), filepath.Join("testdata", "trustmanager", "trustmanager_template.yaml"), "")
			// No DeferCleanup here because the test itself deletes the CR

			waitForTrustManagerReady()

			By("verifying trust-manager deployment exists before deletion")
			_, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
			Expect(err).Should(BeNil(), "deployment should exist before deletion")

			By("deleting TrustManager CR")
			loader.DeleteFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
				templateConfig,
			), filepath.Join("testdata", "trustmanager", "trustmanager_template.yaml"), "")

			By("verifying TrustManager CR is deleted (finalizer removed)")
			trustmanagerClient := loader.DynamicClient.Resource(trustmanagerSchema)
			Eventually(func() bool {
				_, err := trustmanagerClient.Get(ctx, "cluster", metav1.GetOptions{})
				return apierrors.IsNotFound(err)
			}, highTimeout, slowPollInterval).Should(BeTrue(), "TrustManager CR should be fully deleted with finalizer removed")

			// Clean up any remaining cluster-scoped resources
			cleanupTrustManagerClusterResources()
		})
	})
})

// pollTillTrustManagerAvailable polls the TrustManager object and returns its status
// once available with Ready=True, otherwise returns a timeout error.
func pollTillTrustManagerAvailable(ctx context.Context, loader library.DynamicResourceLoader, trustManagerName string) (v1alpha1.TrustManagerStatus, error) {
	var trustManagerStatus v1alpha1.TrustManagerStatus
	trustmanagerClient := loader.DynamicClient.Resource(trustmanagerSchema)
	err := wait.PollUntilContextTimeout(ctx, slowPollInterval, highTimeout, true, func(context.Context) (bool, error) {
		customResource, err := trustmanagerClient.Get(ctx, trustManagerName, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}

		status, found, err := unstructured.NestedMap(customResource.Object, "status")
		if err != nil {
			return false, fmt.Errorf("failed to extract status from TrustManager: %w", err)
		}
		if !found {
			return false, nil
		}

		err = runtime.DefaultUnstructuredConverter.FromUnstructured(status, &trustManagerStatus)
		if err != nil {
			return false, fmt.Errorf("failed to convert status to TrustManagerStatus: %w", err)
		}

		// Check if image is populated
		if trustManagerStatus.TrustManagerImage == "" {
			return false, nil
		}

		// Check ready condition
		readyCondition := meta.FindStatusCondition(trustManagerStatus.Conditions, v1alpha1.Ready)
		if readyCondition == nil {
			return false, nil
		}

		// Check for degraded condition
		degradedCondition := meta.FindStatusCondition(trustManagerStatus.Conditions, v1alpha1.Degraded)
		if degradedCondition != nil && degradedCondition.Status == metav1.ConditionTrue {
			return false, fmt.Errorf("TrustManager is degraded: %s", degradedCondition.Message)
		}

		return readyCondition.Status == metav1.ConditionTrue, nil
	})

	return trustManagerStatus, err
}
