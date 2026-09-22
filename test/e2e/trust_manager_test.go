//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"fmt"
	"log"
	"path/filepath"

	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// TrustManagerTemplateConfig holds template configuration values for
// the trust-manager CR template YAML.
type TrustManagerTemplateConfig struct {
	LogLevel                 int32
	LogFormat                string
	TrustNamespace           string
	FilterExpiredCertificates string
	SecretTargetsPolicy      string
	AuthorizedSecrets        []string
	DefaultCAPackagePolicy   string
}

var trustmanagerSchema = schema.GroupVersionResource{
	Group:    "operator.openshift.io",
	Version:  "v1alpha1",
	Resource: "trustmanagers",
}

// pollTillTrustManagerAvailable polls the TrustManager object and returns status and nil error
// once the trust-manager is available, otherwise should return a time-out error.
func pollTillTrustManagerAvailable(ctx context.Context, tmName string) (v1alpha1.TrustManagerStatus, error) {
	var trustManagerStatus v1alpha1.TrustManagerStatus
	trustmanagerClient := loader.DynamicClient.Resource(trustmanagerSchema)
	err := wait.PollUntilContextTimeout(ctx, slowPollInterval, highTimeout, true, func(context.Context) (bool, error) {
		customResource, err := trustmanagerClient.Get(ctx, tmName, metav1.GetOptions{})
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

		// Check if the image field is populated
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

// pollTillClusterRoleExists polls until a ClusterRole with the given name exists.
func pollTillClusterRoleExists(ctx context.Context, clientset *kubernetes.Clientset, name string) error {
	return wait.PollUntilContextTimeout(ctx, fastPollInterval, lowTimeout, true, func(context.Context) (bool, error) {
		_, err := clientset.RbacV1().ClusterRoles().Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	})
}

// pollTillClusterRoleDeleted polls until a ClusterRole with the given name no longer exists.
func pollTillClusterRoleDeleted(ctx context.Context, clientset *kubernetes.Clientset, name string) error {
	return wait.PollUntilContextTimeout(ctx, fastPollInterval, lowTimeout, true, func(context.Context) (bool, error) {
		_, err := clientset.RbacV1().ClusterRoles().Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	})
}

// pollTillClusterRoleBindingExists polls until a ClusterRoleBinding with the given name exists.
func pollTillClusterRoleBindingExists(ctx context.Context, clientset *kubernetes.Clientset, name string) error {
	return wait.PollUntilContextTimeout(ctx, fastPollInterval, lowTimeout, true, func(context.Context) (bool, error) {
		_, err := clientset.RbacV1().ClusterRoleBindings().Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	})
}

// pollTillClusterRoleBindingDeleted polls until a ClusterRoleBinding with the given name no longer exists.
func pollTillClusterRoleBindingDeleted(ctx context.Context, clientset *kubernetes.Clientset, name string) error {
	return wait.PollUntilContextTimeout(ctx, fastPollInterval, lowTimeout, true, func(context.Context) (bool, error) {
		_, err := clientset.RbacV1().ClusterRoleBindings().Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	})
}

// deleteTrustManagerCR deletes the TrustManager CR using the dynamic client.
func deleteTrustManagerCR(ctx context.Context, name string) error {
	trustmanagerClient := loader.DynamicClient.Resource(trustmanagerSchema)
	err := trustmanagerClient.Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

// pollTillTrustManagerDeleted polls until the TrustManager CR no longer exists.
func pollTillTrustManagerDeleted(ctx context.Context, name string) error {
	trustmanagerClient := loader.DynamicClient.Resource(trustmanagerSchema)
	return wait.PollUntilContextTimeout(ctx, fastPollInterval, lowTimeout, true, func(context.Context) (bool, error) {
		_, err := trustmanagerClient.Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	})
}

var _ = Describe("TrustManager", Ordered, Label("Feature:TrustManager"), func() {
	ctx := context.TODO()
	var clientset *kubernetes.Clientset

	const (
		trustManagerDeploymentName  = "trust-manager"
		trustManagerNamespace       = "cert-manager"
		trustManagerCRName          = "cluster"
		secretTargetsClusterRole    = "trust-manager-secret-targets"
		secretTargetsClusterBinding = "trust-manager-secret-targets"
	)

	waitForTrustManagerReady := func() v1alpha1.TrustManagerStatus {
		By("poll till trust-manager deployment is available")
		err := pollTillDeploymentAvailable(ctx, clientset, trustManagerNamespace, trustManagerDeploymentName)
		Expect(err).Should(BeNil())

		By("poll till trustmanager object is available")
		trustManagerStatus, err := pollTillTrustManagerAvailable(ctx, trustManagerCRName)
		Expect(err).Should(BeNil())

		return trustManagerStatus
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

	Context("basic deployment lifecycle", func() {
		It("should deploy trust-manager when TrustManager CR is created with default settings", func() {
			By("creating trustmanager.operator.openshift.io resource")
			loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
				TrustManagerTemplateConfig{},
			), filepath.Join("testdata", "trustmanager", "trust_manager_template.yaml"), "")
			DeferCleanup(func() {
				By("deleting the TrustManager CR")
				err := deleteTrustManagerCR(ctx, trustManagerCRName)
				Expect(err).Should(BeNil())

				By("waiting for TrustManager CR to be deleted")
				err = pollTillTrustManagerDeleted(ctx, trustManagerCRName)
				Expect(err).Should(BeNil())

				By("deleting cluster-scoped RBAC resources of trust-manager")
				clientset.RbacV1().ClusterRoles().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
					LabelSelector: "app.kubernetes.io/name=trust-manager",
				})
				clientset.RbacV1().ClusterRoleBindings().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
					LabelSelector: "app.kubernetes.io/name=trust-manager",
				})
			})

			trustManagerStatus := waitForTrustManagerReady()
			log.Printf("TrustManager status: %+v", trustManagerStatus)

			By("verifying status fields are populated correctly")
			Expect(trustManagerStatus.TrustManagerImage).ShouldNot(BeEmpty(), "trust-manager image should be reported in status")
			Expect(trustManagerStatus.TrustNamespace).Should(Equal("cert-manager"), "default trustNamespace should be cert-manager")
			Expect(string(trustManagerStatus.SecretTargetsPolicy)).Should(Equal("Disabled"), "default secretTargetsPolicy should be Disabled")
			Expect(string(trustManagerStatus.FilterExpiredCertificatesPolicy)).Should(Equal("Disabled"), "default filterExpiredCertificatesPolicy should be Disabled")
			Expect(string(trustManagerStatus.DefaultCAPackagePolicy)).Should(Equal("Disabled"), "default defaultCAPackagePolicy should be Disabled")

			By("verifying trust-manager deployment exists and is running")
			deployment, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
			Expect(err).Should(BeNil())
			Expect(deployment.Status.ReadyReplicas).Should(BeNumerically(">=", 1))

			By("verifying trust-manager ServiceAccount exists")
			err = pollTillServiceAccountAvailable(ctx, clientset, trustManagerNamespace, "trust-manager")
			Expect(err).Should(BeNil())

			By("verifying trust-manager ClusterRole exists")
			err = pollTillClusterRoleExists(ctx, clientset, "trust-manager")
			Expect(err).Should(BeNil())

			By("verifying trust-manager ClusterRoleBinding exists")
			err = pollTillClusterRoleBindingExists(ctx, clientset, "trust-manager")
			Expect(err).Should(BeNil())

			By("verifying trust-manager webhook Service exists")
			_, err = clientset.CoreV1().Services(trustManagerNamespace).Get(ctx, "trust-manager", metav1.GetOptions{})
			Expect(err).Should(BeNil())

			By("verifying trust-manager metrics Service exists")
			_, err = clientset.CoreV1().Services(trustManagerNamespace).Get(ctx, "trust-manager-metrics", metav1.GetOptions{})
			Expect(err).Should(BeNil())

			By("verifying secret-targets ClusterRole does NOT exist (default policy is Disabled)")
			_, err = clientset.RbacV1().ClusterRoles().Get(ctx, secretTargetsClusterRole, metav1.GetOptions{})
			Expect(apierrors.IsNotFound(err)).Should(BeTrue(), "secret-targets ClusterRole should not exist when policy is Disabled")
		})

		It("should reconcile trust-manager deployment when it is manually deleted", func() {
			By("creating trustmanager.operator.openshift.io resource")
			loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
				TrustManagerTemplateConfig{},
			), filepath.Join("testdata", "trustmanager", "trust_manager_template.yaml"), "")
			DeferCleanup(func() {
				By("deleting the TrustManager CR")
				err := deleteTrustManagerCR(ctx, trustManagerCRName)
				Expect(err).Should(BeNil())
				err = pollTillTrustManagerDeleted(ctx, trustManagerCRName)
				Expect(err).Should(BeNil())

				clientset.RbacV1().ClusterRoles().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
					LabelSelector: "app.kubernetes.io/name=trust-manager",
				})
				clientset.RbacV1().ClusterRoleBindings().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
					LabelSelector: "app.kubernetes.io/name=trust-manager",
				})
			})

			waitForTrustManagerReady()

			By("deleting the trust-manager deployment manually")
			err := clientset.AppsV1().Deployments(trustManagerNamespace).Delete(ctx, trustManagerDeploymentName, metav1.DeleteOptions{})
			Expect(err).Should(BeNil())

			By("verifying the trust-manager deployment is recreated by the controller")
			err = pollTillDeploymentAvailable(ctx, clientset, trustManagerNamespace, trustManagerDeploymentName)
			Expect(err).Should(BeNil(), "trust-manager deployment should be reconciled after manual deletion")
		})
	})

	Context("secretTargets policy management", func() {
		It("should create and remove secret-targets RBAC when policy changes between Custom and Disabled", func() {
			By("creating trustmanager.operator.openshift.io resource with Custom secretTargets policy")
			loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
				TrustManagerTemplateConfig{
					SecretTargetsPolicy: "Custom",
					AuthorizedSecrets:   []string{"my-secret"},
				},
			), filepath.Join("testdata", "trustmanager", "trust_manager_template.yaml"), "")
			DeferCleanup(func() {
				By("deleting the TrustManager CR")
				err := deleteTrustManagerCR(ctx, trustManagerCRName)
				Expect(err).Should(BeNil())
				err = pollTillTrustManagerDeleted(ctx, trustManagerCRName)
				Expect(err).Should(BeNil())

				clientset.RbacV1().ClusterRoles().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
					LabelSelector: "app.kubernetes.io/name=trust-manager",
				})
				clientset.RbacV1().ClusterRoleBindings().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
					LabelSelector: "app.kubernetes.io/name=trust-manager",
				})
			})

			trustManagerStatus := waitForTrustManagerReady()

			By("verifying status reflects Custom policy")
			Expect(string(trustManagerStatus.SecretTargetsPolicy)).Should(Equal("Custom"))

			By("verifying secret-targets ClusterRole is created")
			err := pollTillClusterRoleExists(ctx, clientset, secretTargetsClusterRole)
			Expect(err).Should(BeNil(), "secret-targets ClusterRole should be created when policy is Custom")

			By("verifying secret-targets ClusterRoleBinding is created")
			err = pollTillClusterRoleBindingExists(ctx, clientset, secretTargetsClusterBinding)
			Expect(err).Should(BeNil(), "secret-targets ClusterRoleBinding should be created when policy is Custom")

			By("verifying the ClusterRole has correct permissions for secrets")
			clusterRole, err := clientset.RbacV1().ClusterRoles().Get(ctx, secretTargetsClusterRole, metav1.GetOptions{})
			Expect(err).Should(BeNil())
			Expect(clusterRole.Rules).ShouldNot(BeEmpty())
			foundSecretRule := false
			for _, rule := range clusterRole.Rules {
				for _, resource := range rule.Resources {
					if resource == "secrets" {
						foundSecretRule = true
						break
					}
				}
			}
			Expect(foundSecretRule).Should(BeTrue(), "secret-targets ClusterRole should contain a rule for secrets")

			By("updating the TrustManager CR to Disabled secretTargets policy")
			trustmanagerClient := loader.DynamicClient.Resource(trustmanagerSchema)
			tmResource, err := trustmanagerClient.Get(ctx, trustManagerCRName, metav1.GetOptions{})
			Expect(err).Should(BeNil())

			// Update the secretTargets policy to Disabled and remove authorizedSecrets
			spec, found, err := unstructured.NestedMap(tmResource.Object, "spec")
			Expect(err).Should(BeNil())
			Expect(found).Should(BeTrue(), "spec should exist in TrustManager CR")
			tmConfig, found, err := unstructured.NestedMap(spec, "trustManagerConfig")
			Expect(err).Should(BeNil())
			Expect(found).Should(BeTrue(), "trustManagerConfig should exist in spec")
			tmConfig["secretTargets"] = map[string]interface{}{
				"policy": "Disabled",
			}
			spec["trustManagerConfig"] = tmConfig
			tmResource.Object["spec"] = spec
			_, err = trustmanagerClient.Update(ctx, tmResource, metav1.UpdateOptions{})
			Expect(err).Should(BeNil())

			By("verifying secret-targets ClusterRole is removed")
			err = pollTillClusterRoleDeleted(ctx, clientset, secretTargetsClusterRole)
			Expect(err).Should(BeNil(), "secret-targets ClusterRole should be removed when policy changes to Disabled")

			By("verifying secret-targets ClusterRoleBinding is removed")
			err = pollTillClusterRoleBindingDeleted(ctx, clientset, secretTargetsClusterBinding)
			Expect(err).Should(BeNil(), "secret-targets ClusterRoleBinding should be removed when policy changes to Disabled")
		})
	})

	Context("defaultCAPackage configuration", func() {
		It("should create CA bundle ConfigMaps when defaultCAPackage policy is Enabled", func() {
			By("creating trustmanager.operator.openshift.io resource with defaultCAPackage Enabled")
			loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
				TrustManagerTemplateConfig{
					DefaultCAPackagePolicy: "Enabled",
				},
			), filepath.Join("testdata", "trustmanager", "trust_manager_template.yaml"), "")
			DeferCleanup(func() {
				By("deleting the TrustManager CR")
				err := deleteTrustManagerCR(ctx, trustManagerCRName)
				Expect(err).Should(BeNil())
				err = pollTillTrustManagerDeleted(ctx, trustManagerCRName)
				Expect(err).Should(BeNil())

				clientset.RbacV1().ClusterRoles().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
					LabelSelector: "app.kubernetes.io/name=trust-manager",
				})
				clientset.RbacV1().ClusterRoleBindings().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
					LabelSelector: "app.kubernetes.io/name=trust-manager",
				})
			})

			trustManagerStatus := waitForTrustManagerReady()
			log.Printf("TrustManager status with defaultCAPackage: %+v", trustManagerStatus)

			By("verifying status reflects Enabled defaultCAPackage policy")
			Expect(string(trustManagerStatus.DefaultCAPackagePolicy)).Should(Equal("Enabled"))

			By("verifying the OpenShift CA bundle injector ConfigMap exists")
			err := pollTillConfigMapAvailable(ctx, clientset, trustManagerNamespace, "trust-manager-openshift-ca-bundle")
			Expect(err).Should(BeNil(), "CA bundle injector ConfigMap should exist when defaultCAPackage is Enabled")

			By("verifying the injector ConfigMap has the trusted CA bundle annotation")
			injectorCM, err := clientset.CoreV1().ConfigMaps(trustManagerNamespace).Get(ctx, "trust-manager-openshift-ca-bundle", metav1.GetOptions{})
			Expect(err).Should(BeNil())
			Expect(injectorCM.Annotations).Should(HaveKey("config.openshift.io/inject-trusted-cabundle"))
			Expect(injectorCM.Annotations["config.openshift.io/inject-trusted-cabundle"]).Should(Equal("true"))

			By("verifying the default CA package ConfigMap exists")
			err = pollTillConfigMapAvailable(ctx, clientset, trustManagerNamespace, "trust-manager-default-ca-package")
			Expect(err).Should(BeNil(), "default CA package ConfigMap should exist when defaultCAPackage is Enabled")

			By("verifying the trust-manager deployment has the default CA package volume mount")
			deployment, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
			Expect(err).Should(BeNil())
			volumeFound := false
			for _, vol := range deployment.Spec.Template.Spec.Volumes {
				if vol.Name == "default-ca-package" {
					volumeFound = true
					Expect(vol.ConfigMap).ShouldNot(BeNil())
					Expect(vol.ConfigMap.Name).Should(Equal("trust-manager-default-ca-package"))
					break
				}
			}
			Expect(volumeFound).Should(BeTrue(), "deployment should have the default-ca-package volume")
		})

		It("should clean up CA bundle ConfigMaps when defaultCAPackage policy changes to Disabled", func() {
			By("creating trustmanager.operator.openshift.io resource with defaultCAPackage Enabled")
			loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
				TrustManagerTemplateConfig{
					DefaultCAPackagePolicy: "Enabled",
				},
			), filepath.Join("testdata", "trustmanager", "trust_manager_template.yaml"), "")
			DeferCleanup(func() {
				By("deleting the TrustManager CR")
				err := deleteTrustManagerCR(ctx, trustManagerCRName)
				Expect(err).Should(BeNil())
				err = pollTillTrustManagerDeleted(ctx, trustManagerCRName)
				Expect(err).Should(BeNil())

				clientset.RbacV1().ClusterRoles().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
					LabelSelector: "app.kubernetes.io/name=trust-manager",
				})
				clientset.RbacV1().ClusterRoleBindings().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
					LabelSelector: "app.kubernetes.io/name=trust-manager",
				})
			})

			waitForTrustManagerReady()

			By("verifying ConfigMaps are created")
			err := pollTillConfigMapAvailable(ctx, clientset, trustManagerNamespace, "trust-manager-openshift-ca-bundle")
			Expect(err).Should(BeNil())
			err = pollTillConfigMapAvailable(ctx, clientset, trustManagerNamespace, "trust-manager-default-ca-package")
			Expect(err).Should(BeNil())

			By("updating the TrustManager CR to disable defaultCAPackage")
			trustmanagerClient := loader.DynamicClient.Resource(trustmanagerSchema)
			tmResource, err := trustmanagerClient.Get(ctx, trustManagerCRName, metav1.GetOptions{})
			Expect(err).Should(BeNil())

			spec, found, err := unstructured.NestedMap(tmResource.Object, "spec")
			Expect(err).Should(BeNil())
			Expect(found).Should(BeTrue(), "spec should exist in TrustManager CR")
			tmConfig, found, err := unstructured.NestedMap(spec, "trustManagerConfig")
			Expect(err).Should(BeNil())
			Expect(found).Should(BeTrue(), "trustManagerConfig should exist in spec")
			tmConfig["defaultCAPackage"] = map[string]interface{}{
				"policy": "Disabled",
			}
			spec["trustManagerConfig"] = tmConfig
			tmResource.Object["spec"] = spec
			_, err = trustmanagerClient.Update(ctx, tmResource, metav1.UpdateOptions{})
			Expect(err).Should(BeNil())

			By("verifying the injector ConfigMap is removed")
			Eventually(func() bool {
				_, err := clientset.CoreV1().ConfigMaps(trustManagerNamespace).Get(ctx, "trust-manager-openshift-ca-bundle", metav1.GetOptions{})
				return apierrors.IsNotFound(err)
			}, lowTimeout, fastPollInterval).Should(BeTrue(), "injector ConfigMap should be removed when defaultCAPackage is Disabled")

			By("verifying the default CA package ConfigMap is removed")
			Eventually(func() bool {
				_, err := clientset.CoreV1().ConfigMaps(trustManagerNamespace).Get(ctx, "trust-manager-default-ca-package", metav1.GetOptions{})
				return apierrors.IsNotFound(err)
			}, lowTimeout, fastPollInterval).Should(BeTrue(), "default CA package ConfigMap should be removed when defaultCAPackage is Disabled")
		})
	})

	Context("filterExpiredCertificates configuration", func() {
		It("should pass filterExpiredCertificates flag to trust-manager when enabled", func() {
			By("creating trustmanager.operator.openshift.io resource with filterExpiredCertificates Enabled")
			loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
				TrustManagerTemplateConfig{
					FilterExpiredCertificates: "Enabled",
				},
			), filepath.Join("testdata", "trustmanager", "trust_manager_template.yaml"), "")
			DeferCleanup(func() {
				By("deleting the TrustManager CR")
				err := deleteTrustManagerCR(ctx, trustManagerCRName)
				Expect(err).Should(BeNil())
				err = pollTillTrustManagerDeleted(ctx, trustManagerCRName)
				Expect(err).Should(BeNil())

				clientset.RbacV1().ClusterRoles().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
					LabelSelector: "app.kubernetes.io/name=trust-manager",
				})
				clientset.RbacV1().ClusterRoleBindings().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
					LabelSelector: "app.kubernetes.io/name=trust-manager",
				})
			})

			trustManagerStatus := waitForTrustManagerReady()

			By("verifying status reflects filterExpiredCertificates Enabled")
			Expect(string(trustManagerStatus.FilterExpiredCertificatesPolicy)).Should(Equal("Enabled"))

			By("verifying the trust-manager deployment container args include --filter-expired-certificates")
			deployment, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
			Expect(err).Should(BeNil())
			Expect(deployment.Spec.Template.Spec.Containers).ShouldNot(BeEmpty())
			args := deployment.Spec.Template.Spec.Containers[0].Args
			Expect(args).Should(ContainElement("--filter-expired-certificates"), "deployment should have the --filter-expired-certificates flag")
		})
	})

	Context("deployment container argument verification", func() {
		It("should configure trust-manager deployment args based on spec", func() {
			By("creating trustmanager.operator.openshift.io resource with custom configuration")
			loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
				TrustManagerTemplateConfig{
					LogLevel:                 3,
					LogFormat:                "json",
					FilterExpiredCertificates: "Enabled",
					SecretTargetsPolicy:      "Custom",
					AuthorizedSecrets:        []string{"test-secret-1", "test-secret-2"},
				},
			), filepath.Join("testdata", "trustmanager", "trust_manager_template.yaml"), "")
			DeferCleanup(func() {
				By("deleting the TrustManager CR")
				err := deleteTrustManagerCR(ctx, trustManagerCRName)
				Expect(err).Should(BeNil())
				err = pollTillTrustManagerDeleted(ctx, trustManagerCRName)
				Expect(err).Should(BeNil())

				clientset.RbacV1().ClusterRoles().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
					LabelSelector: "app.kubernetes.io/name=trust-manager",
				})
				clientset.RbacV1().ClusterRoleBindings().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
					LabelSelector: "app.kubernetes.io/name=trust-manager",
				})
			})

			waitForTrustManagerReady()

			By("fetching trust-manager deployment")
			deployment, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
			Expect(err).Should(BeNil())
			Expect(deployment.Spec.Template.Spec.Containers).ShouldNot(BeEmpty())
			args := deployment.Spec.Template.Spec.Containers[0].Args

			By("verifying log level is set")
			Expect(args).Should(ContainElement(ContainSubstring("-v=3")))

			By("verifying log format is set to json")
			Expect(args).Should(ContainElement("--log-format=json"))

			By("verifying filter expired certificates flag is set")
			Expect(args).Should(ContainElement("--filter-expired-certificates"))

			By("verifying trust namespace is set")
			Expect(args).Should(ContainElement("--trust-namespace=cert-manager"))

			By("verifying secret targets are configured")
			Expect(args).Should(ContainElement(ContainSubstring("--secret-targets-enabled")))
		})
	})

	Context("RBAC resources", func() {
		It("should create all expected RBAC resources for trust-manager", func() {
			By("creating trustmanager.operator.openshift.io resource")
			loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
				TrustManagerTemplateConfig{},
			), filepath.Join("testdata", "trustmanager", "trust_manager_template.yaml"), "")
			DeferCleanup(func() {
				By("deleting the TrustManager CR")
				err := deleteTrustManagerCR(ctx, trustManagerCRName)
				Expect(err).Should(BeNil())
				err = pollTillTrustManagerDeleted(ctx, trustManagerCRName)
				Expect(err).Should(BeNil())

				clientset.RbacV1().ClusterRoles().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
					LabelSelector: "app.kubernetes.io/name=trust-manager",
				})
				clientset.RbacV1().ClusterRoleBindings().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
					LabelSelector: "app.kubernetes.io/name=trust-manager",
				})
			})

			waitForTrustManagerReady()

			By("verifying ClusterRole exists with correct labels")
			clusterRole, err := clientset.RbacV1().ClusterRoles().Get(ctx, "trust-manager", metav1.GetOptions{})
			Expect(err).Should(BeNil())
			Expect(clusterRole.Labels).Should(HaveKeyWithValue("app.kubernetes.io/name", "trust-manager"))

			By("verifying ClusterRoleBinding exists with correct role reference")
			clusterRoleBinding, err := clientset.RbacV1().ClusterRoleBindings().Get(ctx, "trust-manager", metav1.GetOptions{})
			Expect(err).Should(BeNil())
			Expect(clusterRoleBinding.RoleRef).Should(Equal(rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     "trust-manager",
			}))

			By("verifying namespace-scoped Role exists")
			_, err = clientset.RbacV1().Roles(trustManagerNamespace).Get(ctx, "trust-manager", metav1.GetOptions{})
			Expect(err).Should(BeNil())

			By("verifying namespace-scoped RoleBinding exists")
			_, err = clientset.RbacV1().RoleBindings(trustManagerNamespace).Get(ctx, "trust-manager", metav1.GetOptions{})
			Expect(err).Should(BeNil())

			By("verifying leases Role exists")
			_, err = clientset.RbacV1().Roles(trustManagerNamespace).Get(ctx, "trust-manager-leases", metav1.GetOptions{})
			Expect(err).Should(BeNil())

			By("verifying leases RoleBinding exists")
			_, err = clientset.RbacV1().RoleBindings(trustManagerNamespace).Get(ctx, "trust-manager-leases", metav1.GetOptions{})
			Expect(err).Should(BeNil())
		})
	})

	Context("network policies", func() {
		It("should create network policies for trust-manager", func() {
			By("creating trustmanager.operator.openshift.io resource")
			loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
				TrustManagerTemplateConfig{},
			), filepath.Join("testdata", "trustmanager", "trust_manager_template.yaml"), "")
			DeferCleanup(func() {
				By("deleting the TrustManager CR")
				err := deleteTrustManagerCR(ctx, trustManagerCRName)
				Expect(err).Should(BeNil())
				err = pollTillTrustManagerDeleted(ctx, trustManagerCRName)
				Expect(err).Should(BeNil())

				clientset.RbacV1().ClusterRoles().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
					LabelSelector: "app.kubernetes.io/name=trust-manager",
				})
				clientset.RbacV1().ClusterRoleBindings().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
					LabelSelector: "app.kubernetes.io/name=trust-manager",
				})
			})

			waitForTrustManagerReady()

			expectedNetworkPolicies := []string{
				"trust-manager-deny-all",
				"trust-manager-allow-egress-to-api-server",
				"trust-manager-allow-ingress-to-metrics",
				"trust-manager-allow-ingress-to-webhook",
			}

			for _, npName := range expectedNetworkPolicies {
				By(fmt.Sprintf("verifying network policy %s exists", npName))
				_, err := clientset.NetworkingV1().NetworkPolicies(trustManagerNamespace).Get(ctx, npName, metav1.GetOptions{})
				Expect(err).Should(BeNil(), fmt.Sprintf("network policy %s should exist", npName))
			}
		})
	})

	Context("webhook configuration", func() {
		It("should create ValidatingWebhookConfiguration for trust-manager", func() {
			By("creating trustmanager.operator.openshift.io resource")
			loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
				TrustManagerTemplateConfig{},
			), filepath.Join("testdata", "trustmanager", "trust_manager_template.yaml"), "")
			DeferCleanup(func() {
				By("deleting the TrustManager CR")
				err := deleteTrustManagerCR(ctx, trustManagerCRName)
				Expect(err).Should(BeNil())
				err = pollTillTrustManagerDeleted(ctx, trustManagerCRName)
				Expect(err).Should(BeNil())

				clientset.RbacV1().ClusterRoles().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
					LabelSelector: "app.kubernetes.io/name=trust-manager",
				})
				clientset.RbacV1().ClusterRoleBindings().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
					LabelSelector: "app.kubernetes.io/name=trust-manager",
				})
			})

			waitForTrustManagerReady()

			By("verifying ValidatingWebhookConfiguration exists")
			Eventually(func() error {
				_, err := clientset.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(ctx, "trust-manager", metav1.GetOptions{})
				return err
			}, lowTimeout, fastPollInterval).Should(Succeed(), "ValidatingWebhookConfiguration should be created")

			By("verifying the ValidatingWebhookConfiguration has the inject-ca-from annotation")
			vwc, err := clientset.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(ctx, "trust-manager", metav1.GetOptions{})
			Expect(err).Should(BeNil())
			Expect(vwc.Annotations).Should(HaveKey("cert-manager.io/inject-ca-from"))

			By("verifying webhook TLS Certificate exists")
			err = waitForCertificateReadiness(ctx, "trust-manager-tls", trustManagerNamespace)
			Expect(err).Should(BeNil(), "webhook TLS certificate should be created and ready")
		})
	})
})
