//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"fmt"
	"log"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/test/library"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

// TrustManagerConfig customizes fields in the trust-manager template
type TrustManagerConfig struct {
	LogLevel                  int32
	LogFormat                 string
	TrustNamespace            string
	FilterExpiredCertificates string
	SecretTargetsPolicy       string
	AuthorizedSecrets         []string
	DefaultCAPackagePolicy    string
}

var trustManagerSchema = schema.GroupVersionResource{
	Group:    "operator.openshift.io",
	Version:  "v1alpha1",
	Resource: "trustmanagers",
}

// pollTillTrustManagerAvailable polls the TrustManager object and returns its status
// once the TrustManager is available, otherwise should return a time-out error
func pollTillTrustManagerAvailable(ctx context.Context, loader library.DynamicResourceLoader) (v1alpha1.TrustManagerStatus, error) {
	var trustManagerStatus v1alpha1.TrustManagerStatus
	trustManagerClient := loader.DynamicClient.Resource(trustManagerSchema)
	err := wait.PollUntilContextTimeout(ctx, slowPollInterval, highTimeout, true, func(context.Context) (bool, error) {
		customResource, err := trustManagerClient.Get(ctx, "cluster", metav1.GetOptions{})
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

		// Check ready condition
		readyCondition := findCondition(trustManagerStatus.Conditions, v1alpha1.Ready)
		if readyCondition == nil {
			return false, nil
		}

		// Check for degraded condition
		degradedCondition := findCondition(trustManagerStatus.Conditions, v1alpha1.Degraded)
		if degradedCondition != nil && degradedCondition.Status == metav1.ConditionTrue {
			return false, fmt.Errorf("TrustManager is degraded: %s", degradedCondition.Message)
		}

		return readyCondition.Status == metav1.ConditionTrue, nil
	})

	return trustManagerStatus, err
}

// pollTillTrustManagerDegraded polls the TrustManager object until it reaches a degraded state
func pollTillTrustManagerDegraded(ctx context.Context, loader library.DynamicResourceLoader) error {
	trustManagerClient := loader.DynamicClient.Resource(trustManagerSchema)
	return wait.PollUntilContextTimeout(ctx, fastPollInterval, lowTimeout, true, func(context.Context) (bool, error) {
		customResource, err := trustManagerClient.Get(ctx, "cluster", metav1.GetOptions{})
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

		var trustManagerStatus v1alpha1.TrustManagerStatus
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(status, &trustManagerStatus)
		if err != nil {
			return false, fmt.Errorf("failed to convert status to TrustManagerStatus: %w", err)
		}

		degradedCondition := findCondition(trustManagerStatus.Conditions, v1alpha1.Degraded)
		if degradedCondition != nil && degradedCondition.Status == metav1.ConditionTrue {
			return true, nil
		}
		return false, nil
	})
}

// findCondition finds a condition by type in the conditions slice
func findCondition(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}

// deleteTrustManagerCR deletes the TrustManager custom resource and waits for deletion
func deleteTrustManagerCR(ctx context.Context, loader library.DynamicResourceLoader) {
	trustManagerClient := loader.DynamicClient.Resource(trustManagerSchema)
	err := trustManagerClient.Delete(ctx, "cluster", metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		log.Printf("WARNING: Failed to delete TrustManager CR: %v", err)
	}

	// Wait for the CR to be fully deleted
	_ = wait.PollUntilContextTimeout(ctx, fastPollInterval, lowTimeout, true, func(context.Context) (bool, error) {
		_, err := trustManagerClient.Get(ctx, "cluster", metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, nil
	})
}

// cleanupTrustManagerResources cleans up cluster-scoped resources created for trust-manager
func cleanupTrustManagerResources(ctx context.Context, clientset *kubernetes.Clientset) {
	labelSelector := "app.kubernetes.io/name=cert-manager-trust-manager"

	clientset.RbacV1().ClusterRoles().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	clientset.RbacV1().ClusterRoleBindings().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
}

var _ = Describe("TrustManager", Ordered, Label("Feature:TrustManager"), func() {
	ctx := context.TODO()
	var clientset *kubernetes.Clientset

	waitForTrustManagerReady := func() v1alpha1.TrustManagerStatus {
		By("poll till trust-manager deployment is available")
		err := pollTillDeploymentAvailable(ctx, clientset, operandNamespace, "cert-manager-trust-manager")
		Expect(err).Should(BeNil())

		By("poll till trustmanager object is available")
		trustManagerStatus, err := pollTillTrustManagerAvailable(ctx, loader)
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

	AfterEach(func() {
		By("deleting TrustManager CR")
		deleteTrustManagerCR(ctx, loader)

		By("cleaning up cluster-scoped trust-manager resources")
		cleanupTrustManagerResources(ctx, clientset)
	})

	Context("basic deployment lifecycle", func() {
		It("should deploy trust-manager with minimal configuration", func() {
			By("creating trustmanager.operator.openshift.io resource with minimal config")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "trust_manager", "trust_manager_minimal_template.yaml"), "")

			trustManagerStatus := waitForTrustManagerReady()
			log.Printf("TrustManager status: %+v", trustManagerStatus)

			By("verifying trust-manager image is populated in status")
			Expect(trustManagerStatus.TrustManagerImage).ShouldNot(BeEmpty())

			By("verifying trust-manager deployment exists in cert-manager namespace")
			deployment, err := clientset.AppsV1().Deployments(operandNamespace).Get(ctx, "cert-manager-trust-manager", metav1.GetOptions{})
			Expect(err).Should(BeNil())
			Expect(deployment.Status.ReadyReplicas).Should(BeNumerically(">=", 1))

			By("verifying trust-manager service account exists")
			_, err = clientset.CoreV1().ServiceAccounts(operandNamespace).Get(ctx, "cert-manager-trust-manager", metav1.GetOptions{})
			Expect(err).Should(BeNil())

			By("verifying trust-manager webhook service exists")
			_, err = clientset.CoreV1().Services(operandNamespace).Get(ctx, "cert-manager-trust-manager", metav1.GetOptions{})
			Expect(err).Should(BeNil())

			By("verifying trust-manager metrics service exists")
			_, err = clientset.CoreV1().Services(operandNamespace).Get(ctx, "cert-manager-trust-manager-metrics", metav1.GetOptions{})
			Expect(err).Should(BeNil())

			By("verifying trust-manager ClusterRole exists")
			_, err = clientset.RbacV1().ClusterRoles().Get(ctx, "cert-manager-trust-manager", metav1.GetOptions{})
			Expect(err).Should(BeNil())

			By("verifying trust-manager ClusterRoleBinding exists")
			_, err = clientset.RbacV1().ClusterRoleBindings().Get(ctx, "cert-manager-trust-manager", metav1.GetOptions{})
			Expect(err).Should(BeNil())

			By("verifying trust-manager ValidatingWebhookConfiguration exists")
			_, err = clientset.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(ctx, "cert-manager-trust-manager", metav1.GetOptions{})
			Expect(err).Should(BeNil())
		})

		It("should reconcile trust-manager deployment when manually modified", func() {
			By("creating trustmanager.operator.openshift.io resource")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "trust_manager", "trust_manager_minimal_template.yaml"), "")
			waitForTrustManagerReady()

			By("manually scaling down the trust-manager deployment to 0 replicas")
			deployment, err := clientset.AppsV1().Deployments(operandNamespace).Get(ctx, "cert-manager-trust-manager", metav1.GetOptions{})
			Expect(err).Should(BeNil())

			zero := int32(0)
			deployment.Spec.Replicas = &zero
			_, err = clientset.AppsV1().Deployments(operandNamespace).Update(ctx, deployment, metav1.UpdateOptions{})
			Expect(err).Should(BeNil())

			By("verifying operator reconciles the deployment back")
			Eventually(func() bool {
				dep, err := clientset.AppsV1().Deployments(operandNamespace).Get(ctx, "cert-manager-trust-manager", metav1.GetOptions{})
				if err != nil {
					return false
				}
				return dep.Status.ReadyReplicas >= 1
			}, highTimeout, slowPollInterval).Should(BeTrue(), "trust-manager deployment should be reconciled back to running")
		})

		It("should clean up resources when TrustManager CR is deleted", func() {
			By("creating trustmanager.operator.openshift.io resource")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "trust_manager", "trust_manager_minimal_template.yaml"), "")
			waitForTrustManagerReady()

			By("deleting TrustManager CR")
			deleteTrustManagerCR(ctx, loader)

			By("verifying trust-manager CR is deleted")
			trustManagerClient := loader.DynamicClient.Resource(trustManagerSchema)
			_, err := trustManagerClient.Get(ctx, "cluster", metav1.GetOptions{})
			Expect(apierrors.IsNotFound(err)).Should(BeTrue(), "TrustManager CR should be deleted")
		})
	})

	Context("deployment configuration", func() {
		It("should deploy trust-manager with custom log level and format", func() {
			By("creating trustmanager.operator.openshift.io resource with custom logging config")
			templateData := TrustManagerConfig{
				LogLevel:  3,
				LogFormat: "json",
			}
			loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(templateData), filepath.Join("testdata", "trust_manager", "trust_manager_template.yaml"), "")
			waitForTrustManagerReady()

			By("verifying deployment has correct log level and format args")
			deployment, err := clientset.AppsV1().Deployments(operandNamespace).Get(ctx, "cert-manager-trust-manager", metav1.GetOptions{})
			Expect(err).Should(BeNil())

			container := findContainer(deployment, "trust-manager")
			Expect(container).ShouldNot(BeNil(), "trust-manager container should exist")

			Expect(container.Args).Should(ContainElement("--log-level=3"))
			Expect(container.Args).Should(ContainElement("--log-format=json"))
		})

		It("should deploy trust-manager with filter expired certificates enabled", func() {
			By("creating trustmanager.operator.openshift.io resource with filter expired certs enabled")
			templateData := TrustManagerConfig{
				FilterExpiredCertificates: "Enabled",
			}
			loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(templateData), filepath.Join("testdata", "trust_manager", "trust_manager_template.yaml"), "")
			waitForTrustManagerReady()

			By("verifying deployment has filter-expired-certificates arg")
			deployment, err := clientset.AppsV1().Deployments(operandNamespace).Get(ctx, "cert-manager-trust-manager", metav1.GetOptions{})
			Expect(err).Should(BeNil())

			container := findContainer(deployment, "trust-manager")
			Expect(container).ShouldNot(BeNil())
			Expect(container.Args).Should(ContainElement("--filter-expired-certificates"))
		})

		It("should deploy trust-manager with default CA package enabled", func() {
			By("creating trustmanager.operator.openshift.io resource with default CA package enabled")
			templateData := TrustManagerConfig{
				DefaultCAPackagePolicy: "Enabled",
			}
			loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(templateData), filepath.Join("testdata", "trust_manager", "trust_manager_template.yaml"), "")
			waitForTrustManagerReady()

			By("verifying deployment has default-package-location arg")
			deployment, err := clientset.AppsV1().Deployments(operandNamespace).Get(ctx, "cert-manager-trust-manager", metav1.GetOptions{})
			Expect(err).Should(BeNil())

			container := findContainer(deployment, "trust-manager")
			Expect(container).ShouldNot(BeNil())
			Expect(container.Args).Should(ContainElement(ContainSubstring("--default-package-location=")))

			By("verifying deployment has CA bundle volume mount")
			hasCAVolume := false
			for _, vol := range deployment.Spec.Template.Spec.Volumes {
				if vol.Name == "openshift-ca-bundle" || vol.Name == "ca-bundle" {
					hasCAVolume = true
					break
				}
			}
			Expect(hasCAVolume).Should(BeTrue(), "deployment should have a CA bundle volume")
		})
	})

	Context("secret targets configuration", func() {
		It("should configure RBAC when secret targets policy is Custom", func() {
			By("creating trustmanager.operator.openshift.io resource with secret targets policy Custom")
			templateData := TrustManagerConfig{
				SecretTargetsPolicy: "Custom",
				AuthorizedSecrets:   []string{"my-trust-bundle", "another-secret"},
			}
			loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(templateData), filepath.Join("testdata", "trust_manager", "trust_manager_template.yaml"), "")
			waitForTrustManagerReady()

			By("verifying ClusterRole has secret resource rules")
			clusterRole, err := clientset.RbacV1().ClusterRoles().Get(ctx, "cert-manager-trust-manager", metav1.GetOptions{})
			Expect(err).Should(BeNil())

			hasSecretRule := hasResourceRule(clusterRole, "", "secrets")
			Expect(hasSecretRule).Should(BeTrue(), "ClusterRole should have secret rules when secretTargets.policy is Custom")

			By("verifying authorized secret names are in the ClusterRole resourceNames")
			hasResourceName := hasResourceNameInRule(clusterRole, "", "secrets", "my-trust-bundle")
			Expect(hasResourceName).Should(BeTrue(), "ClusterRole should contain authorized secret name")

			By("verifying status reflects secret targets policy")
			trustManagerStatus, err := pollTillTrustManagerAvailable(ctx, loader)
			Expect(err).Should(BeNil())
			Expect(string(trustManagerStatus.SecretTargetsPolicy)).Should(Equal("Custom"))
		})

		It("should not have secret RBAC rules when secret targets policy is Disabled", func() {
			By("creating trustmanager.operator.openshift.io resource with default (Disabled) secret targets")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "trust_manager", "trust_manager_minimal_template.yaml"), "")
			waitForTrustManagerReady()

			By("verifying ClusterRole does not have secret write rules")
			clusterRole, err := clientset.RbacV1().ClusterRoles().Get(ctx, "cert-manager-trust-manager", metav1.GetOptions{})
			Expect(err).Should(BeNil())

			hasSecretWriteRule := hasVerbOnResource(clusterRole, "", "secrets", "create")
			Expect(hasSecretWriteRule).Should(BeFalse(), "ClusterRole should not have secret create verb when secretTargets.policy is Disabled")
		})
	})

	Context("status reporting", func() {
		It("should report correct status fields after successful deployment", func() {
			By("creating trustmanager.operator.openshift.io resource")
			templateData := TrustManagerConfig{
				FilterExpiredCertificates: "Enabled",
				DefaultCAPackagePolicy:    "Enabled",
			}
			loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(templateData), filepath.Join("testdata", "trust_manager", "trust_manager_template.yaml"), "")
			trustManagerStatus := waitForTrustManagerReady()

			By("verifying status fields are populated")
			Expect(trustManagerStatus.TrustManagerImage).ShouldNot(BeEmpty(), "trustManagerImage should be populated in status")
			Expect(trustManagerStatus.TrustNamespace).ShouldNot(BeEmpty(), "trustNamespace should be populated in status")
			Expect(string(trustManagerStatus.FilterExpiredCertificatesPolicy)).Should(Equal("Enabled"))
			Expect(string(trustManagerStatus.DefaultCAPackagePolicy)).Should(Equal("Enabled"))

			By("verifying Ready condition is True")
			readyCondition := findCondition(trustManagerStatus.Conditions, v1alpha1.Ready)
			Expect(readyCondition).ShouldNot(BeNil())
			Expect(readyCondition.Status).Should(Equal(metav1.ConditionTrue))

			By("verifying Degraded condition is False")
			degradedCondition := findCondition(trustManagerStatus.Conditions, v1alpha1.Degraded)
			Expect(degradedCondition).ShouldNot(BeNil())
			Expect(degradedCondition.Status).Should(Equal(metav1.ConditionFalse))
		})
	})

	Context("managed resource labels", func() {
		It("should apply correct labels to all managed resources", func() {
			By("creating trustmanager.operator.openshift.io resource")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "trust_manager", "trust_manager_minimal_template.yaml"), "")
			waitForTrustManagerReady()

			expectedLabels := map[string]string{
				"app":                          "cert-manager-trust-manager",
				"app.kubernetes.io/name":       "cert-manager-trust-manager",
				"app.kubernetes.io/managed-by": "cert-manager-operator",
			}

			By("verifying deployment has correct labels")
			deployment, err := clientset.AppsV1().Deployments(operandNamespace).Get(ctx, "cert-manager-trust-manager", metav1.GetOptions{})
			Expect(err).Should(BeNil())
			for k, v := range expectedLabels {
				Expect(deployment.Labels[k]).Should(Equal(v), fmt.Sprintf("deployment should have label %s=%s", k, v))
			}

			By("verifying service account has correct labels")
			sa, err := clientset.CoreV1().ServiceAccounts(operandNamespace).Get(ctx, "cert-manager-trust-manager", metav1.GetOptions{})
			Expect(err).Should(BeNil())
			for k, v := range expectedLabels {
				Expect(sa.Labels[k]).Should(Equal(v), fmt.Sprintf("service account should have label %s=%s", k, v))
			}
		})
	})

	Context("controller labels and annotations from spec", func() {
		It("should apply controllerConfig labels to managed resources", func() {
			By("creating trustmanager.operator.openshift.io resource with controller config labels")
			templateData := TrustManagerConfig{}
			loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(templateData), filepath.Join("testdata", "trust_manager", "trust_manager_template.yaml"), "")
			waitForTrustManagerReady()

			By("verifying deployment has the custom label from controllerConfig")
			deployment, err := clientset.AppsV1().Deployments(operandNamespace).Get(ctx, "cert-manager-trust-manager", metav1.GetOptions{})
			Expect(err).Should(BeNil())
			Expect(deployment.Labels["test-label"]).Should(Equal("e2e-test"), "deployment should have custom label from controllerConfig")
		})
	})
})

// findContainer finds a container by name in a deployment
func findContainer(deployment *appsv1.Deployment, name string) *corev1.Container {
	for i := range deployment.Spec.Template.Spec.Containers {
		if deployment.Spec.Template.Spec.Containers[i].Name == name {
			return &deployment.Spec.Template.Spec.Containers[i]
		}
	}
	return nil
}

// hasResourceRule checks if a ClusterRole has a rule for the given API group and resource
func hasResourceRule(role *rbacv1.ClusterRole, apiGroup, resource string) bool {
	for _, rule := range role.Rules {
		for _, g := range rule.APIGroups {
			if g == apiGroup {
				for _, r := range rule.Resources {
					if r == resource {
						return true
					}
				}
			}
		}
	}
	return false
}

// hasResourceNameInRule checks if a ClusterRole has a rule with the given resource name
func hasResourceNameInRule(role *rbacv1.ClusterRole, apiGroup, resource, resourceName string) bool {
	for _, rule := range role.Rules {
		for _, g := range rule.APIGroups {
			if g == apiGroup {
				for _, r := range rule.Resources {
					if r == resource {
						for _, rn := range rule.ResourceNames {
							if rn == resourceName {
								return true
							}
						}
					}
				}
			}
		}
	}
	return false
}

// hasVerbOnResource checks if a ClusterRole has a specific verb on a resource
func hasVerbOnResource(role *rbacv1.ClusterRole, apiGroup, resource, verb string) bool {
	for _, rule := range role.Rules {
		for _, g := range rule.APIGroups {
			if g == apiGroup {
				for _, r := range rule.Resources {
					if r == resource {
						for _, v := range rule.Verbs {
							if v == verb {
								return true
							}
						}
					}
				}
			}
		}
	}
	return false
}
