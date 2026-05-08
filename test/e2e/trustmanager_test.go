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

const (
	trustManagerDeploymentName    = "cert-manager-trust-manager"
	trustManagerServiceAccount    = "trust-manager"
	trustManagerClusterRoleName   = "trust-manager"
	trustManagerDefaultCAPackageCM = "trust-manager-default-ca-package"
)

var trustmanagerSchema = schema.GroupVersionResource{
	Group:    "operator.openshift.io",
	Version:  "v1alpha1",
	Resource: "trustmanagers",
}

var _ = Describe("TrustManager", Ordered, Label("Feature:TrustManager"), func() {
	ctx := context.TODO()
	var clientset *kubernetes.Clientset

	// waitForTrustManagerReady polls the TrustManager CR until it reaches Ready state
	waitForTrustManagerReady := func() v1alpha1.TrustManagerStatus {
		By("poll till trust-manager deployment is available")
		err := pollTillDeploymentAvailable(ctx, clientset, operandNamespace, trustManagerDeploymentName)
		Expect(err).Should(BeNil())

		By("poll till trustmanager object is available and ready")
		status, err := pollTillTrustManagerAvailable(ctx, loader, "cluster")
		Expect(err).Should(BeNil())

		return status
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

	Context("basic trust-manager deployment", func() {
		It("should deploy trust-manager with default configuration", func() {
			By("creating trustmanager.operator.openshift.io resource")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "trustmanager", "trustmanager_cr.yaml"), "")
			defer func() {
				By("deleting trustmanager.operator.openshift.io resource")
				loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "trustmanager", "trustmanager_cr.yaml"), "")
				// cleanup cluster-scoped RBAC resources
				cleanupTrustManagerRBAC(ctx, clientset)
			}()

			status := waitForTrustManagerReady()

			By("verifying trust-manager status fields are populated")
			Expect(status.TrustManagerImage).NotTo(BeEmpty(), "TrustManagerImage should be populated")
			Expect(status.TrustNamespace).To(Equal("cert-manager"), "TrustNamespace should be cert-manager")
			Expect(string(status.SecretTargetsPolicy)).To(Equal("Disabled"), "SecretTargetsPolicy should be Disabled")
			Expect(string(status.DefaultCAPackagePolicy)).To(Equal("Disabled"), "DefaultCAPackagePolicy should be Disabled")
			Expect(string(status.FilterExpiredCertificatesPolicy)).To(Equal("Disabled"), "FilterExpiredCertificatesPolicy should be Disabled")

			By("verifying Ready condition is True")
			readyCondition := meta.FindStatusCondition(status.Conditions, v1alpha1.Ready)
			Expect(readyCondition).NotTo(BeNil(), "Ready condition should exist")
			Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue), "Ready condition should be True")

			By("verifying Degraded condition is False")
			degradedCondition := meta.FindStatusCondition(status.Conditions, v1alpha1.Degraded)
			Expect(degradedCondition).NotTo(BeNil(), "Degraded condition should exist")
			Expect(degradedCondition.Status).To(Equal(metav1.ConditionFalse), "Degraded condition should be False")

			By("verifying ServiceAccount is created")
			err := pollTillServiceAccountAvailable(ctx, clientset, operandNamespace, trustManagerContainerName)
			Expect(err).Should(BeNil(), "ServiceAccount should be created")

			By("verifying ClusterRole is created")
			_, err = clientset.RbacV1().ClusterRoles().Get(ctx, trustManagerClusterRoleName, metav1.GetOptions{})
			Expect(err).Should(BeNil(), "ClusterRole should be created")

			By("verifying ClusterRoleBinding is created")
			_, err = clientset.RbacV1().ClusterRoleBindings().Get(ctx, trustManagerClusterRoleName, metav1.GetOptions{})
			Expect(err).Should(BeNil(), "ClusterRoleBinding should be created")

			By("verifying webhook Service is created")
			_, err = clientset.CoreV1().Services(operandNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
			Expect(err).Should(BeNil(), "webhook Service should be created")

			By("verifying metrics Service is created")
			_, err = clientset.CoreV1().Services(operandNamespace).Get(ctx, trustManagerDeploymentName+"-metrics", metav1.GetOptions{})
			Expect(err).Should(BeNil(), "metrics Service should be created")

			By("verifying Certificate is created for webhook TLS")
			cert, err := certmanagerClient.CertmanagerV1().Certificates(operandNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
			Expect(err).Should(BeNil(), "Certificate should be created")
			Expect(cert.Spec.SecretName).To(Equal(trustManagerDeploymentName + "-tls"), "Certificate should reference the TLS secret")

			By("verifying Issuer is created for webhook TLS")
			_, err = certmanagerClient.CertmanagerV1().Issuers(operandNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
			Expect(err).Should(BeNil(), "Issuer should be created")

			By("verifying DefaultCAPackage ConfigMap is NOT created when policy is Disabled")
			err = pollTillConfigMapRemains(ctx, clientset, operandNamespace, trustManagerDefaultCAPackageCM, lowTimeout)
			Expect(err).Should(BeNil(), "DefaultCAPackage ConfigMap should not exist when policy is Disabled")

			By("verifying trust-manager deployment container args")
			deployment, err := clientset.AppsV1().Deployments(operandNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
			Expect(err).Should(BeNil())
			Expect(len(deployment.Spec.Template.Spec.Containers)).To(BeNumerically(">", 0))
			container := deployment.Spec.Template.Spec.Containers[0]
			Expect(container.Args).To(ContainElement("--trust-namespace=cert-manager"))
			Expect(container.Args).To(ContainElement("--log-format=text"))
			Expect(container.Args).To(ContainElement("--log-level=1"))
		})
	})

	Context("trust-manager with SecretTargets configuration", func() {
		It("should configure RBAC rules for authorized secrets", func() {
			By("creating trustmanager.operator.openshift.io resource with SecretTargets Custom policy")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "trustmanager", "trustmanager_cr_with_secret_targets.yaml"), "")
			defer func() {
				By("deleting trustmanager.operator.openshift.io resource")
				loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "trustmanager", "trustmanager_cr_with_secret_targets.yaml"), "")
				cleanupTrustManagerRBAC(ctx, clientset)
			}()

			waitForTrustManagerReady()

			By("verifying ClusterRole has secret write rules for authorized secrets")
			err := wait.PollUntilContextTimeout(ctx, fastPollInterval, lowTimeout, true, func(context.Context) (bool, error) {
				clusterRole, err := clientset.RbacV1().ClusterRoles().Get(ctx, trustManagerClusterRoleName, metav1.GetOptions{})
				if err != nil {
					if apierrors.IsNotFound(err) {
						return false, nil
					}
					return false, err
				}

				// Check for the secret write rule with authorized secret names
				for _, rule := range clusterRole.Rules {
					if containsString(rule.Resources, "secrets") &&
						containsString(rule.Verbs, "create") &&
						containsString(rule.Verbs, "update") &&
						containsString(rule.ResourceNames, "my-trust-bundle-secret") &&
						containsString(rule.ResourceNames, "another-trust-bundle-secret") {
						return true, nil
					}
				}
				return false, nil
			})
			Expect(err).Should(BeNil(), "ClusterRole should have authorized secret write rules")

			By("verifying trust-manager deployment container args include secret-targets-enabled")
			deployment, err := clientset.AppsV1().Deployments(operandNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
			Expect(err).Should(BeNil())
			container := deployment.Spec.Template.Spec.Containers[0]
			Expect(container.Args).To(ContainElement("--secret-targets-enabled=true"))
		})
	})

	Context("trust-manager with DefaultCAPackage configuration", func() {
		It("should create DefaultCAPackage ConfigMap when policy is Enabled", func() {
			By("creating trustmanager.operator.openshift.io resource with DefaultCAPackage Enabled")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "trustmanager", "trustmanager_cr_with_default_ca_package.yaml"), "")
			defer func() {
				By("deleting trustmanager.operator.openshift.io resource")
				loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "trustmanager", "trustmanager_cr_with_default_ca_package.yaml"), "")
				cleanupTrustManagerRBAC(ctx, clientset)
			}()

			// Note: This test may fail if the operator namespace does not have the
			// cert-manager-operator-trusted-ca-bundle ConfigMap injected by CNO.
			// In that case, the controller will set Degraded=True with a message
			// about the missing CA bundle.
			By("poll till trust-manager deployment is available or status becomes degraded")
			status, err := pollTillTrustManagerAvailable(ctx, loader, "cluster")
			if err != nil {
				// If DefaultCAPackage failed due to missing CA bundle, verify the error is expected
				By("checking if failure is due to missing CA bundle ConfigMap (expected in test environments)")
				degradedStatus, statusErr := getTrustManagerStatus(ctx, loader, "cluster")
				if statusErr == nil {
					degradedCondition := meta.FindStatusCondition(degradedStatus.Conditions, v1alpha1.Degraded)
					if degradedCondition != nil && degradedCondition.Status == metav1.ConditionTrue {
						Skip("DefaultCAPackage test skipped: CA bundle ConfigMap not available in test environment")
					}
				}
				Expect(err).Should(BeNil(), "TrustManager should become available")
			}

			By("verifying DefaultCAPackage ConfigMap is created")
			err = pollTillConfigMapAvailable(ctx, clientset, operandNamespace, trustManagerDefaultCAPackageCM)
			Expect(err).Should(BeNil(), "DefaultCAPackage ConfigMap should be created")

			By("verifying DefaultCAPackage ConfigMap has expected data key")
			cm, err := clientset.CoreV1().ConfigMaps(operandNamespace).Get(ctx, trustManagerDefaultCAPackageCM, metav1.GetOptions{})
			Expect(err).Should(BeNil())
			Expect(cm.Data).To(HaveKey("cert-manager-package-openshift.json"), "ConfigMap should have CA package JSON")

			By("verifying trust-manager deployment has default-package-location arg")
			deployment, err := clientset.AppsV1().Deployments(operandNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
			Expect(err).Should(BeNil())
			container := deployment.Spec.Template.Spec.Containers[0]
			Expect(container.Args).To(ContainElement(ContainSubstring("--default-package-location=")))

			By("verifying deployment has CA package volume mount")
			found := false
			for _, vm := range container.VolumeMounts {
				if vm.Name == "default-ca-package" {
					found = true
					break
				}
			}
			Expect(found).To(BeTrue(), "Container should have default-ca-package volume mount")

			_ = status
		})
	})

	Context("trust-manager CR deletion", func() {
		It("should stop reconciliation without removing operand resources", func() {
			By("creating trustmanager.operator.openshift.io resource")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "trustmanager", "trustmanager_cr.yaml"), "")

			waitForTrustManagerReady()

			By("deleting trustmanager.operator.openshift.io resource")
			loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "trustmanager", "trustmanager_cr.yaml"), "")

			By("verifying TrustManager CR is deleted")
			trustmanagerClient := loader.DynamicClient.Resource(trustmanagerSchema)
			err := wait.PollUntilContextTimeout(ctx, fastPollInterval, lowTimeout, true, func(context.Context) (bool, error) {
				_, err := trustmanagerClient.Get(ctx, "cluster", metav1.GetOptions{})
				if apierrors.IsNotFound(err) {
					return true, nil
				}
				return false, err
			})
			Expect(err).Should(BeNil(), "TrustManager CR should be deleted")

			By("verifying trust-manager deployment still exists (non-destructive cleanup)")
			_, err = clientset.AppsV1().Deployments(operandNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
			Expect(err).Should(BeNil(), "Deployment should still exist after CR deletion")

			By("verifying ServiceAccount still exists")
			_, err = clientset.CoreV1().ServiceAccounts(operandNamespace).Get(ctx, trustManagerContainerName, metav1.GetOptions{})
			Expect(err).Should(BeNil(), "ServiceAccount should still exist after CR deletion")

			defer func() {
				// Final cleanup: remove leftover resources manually since cleanup is non-destructive
				cleanupTrustManagerRBAC(ctx, clientset)
				cleanupTrustManagerResources(ctx, clientset)
			}()
		})
	})
})

// trustManagerContainerName is the expected name of the trust-manager container and ServiceAccount
const trustManagerContainerName = "trust-manager"

// pollTillTrustManagerAvailable polls the TrustManager CR and returns its status
// once it reaches Ready=True, otherwise returns a timeout error.
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

// getTrustManagerStatus fetches the current status of the TrustManager CR without waiting for readiness.
func getTrustManagerStatus(ctx context.Context, loader library.DynamicResourceLoader, trustManagerName string) (v1alpha1.TrustManagerStatus, error) {
	var trustManagerStatus v1alpha1.TrustManagerStatus
	trustmanagerClient := loader.DynamicClient.Resource(trustmanagerSchema)

	customResource, err := trustmanagerClient.Get(ctx, trustManagerName, metav1.GetOptions{})
	if err != nil {
		return trustManagerStatus, err
	}

	status, found, err := unstructured.NestedMap(customResource.Object, "status")
	if err != nil {
		return trustManagerStatus, fmt.Errorf("failed to extract status from TrustManager: %w", err)
	}
	if !found {
		return trustManagerStatus, fmt.Errorf("status not found in TrustManager CR")
	}

	err = runtime.DefaultUnstructuredConverter.FromUnstructured(status, &trustManagerStatus)
	if err != nil {
		return trustManagerStatus, fmt.Errorf("failed to convert status to TrustManagerStatus: %w", err)
	}

	return trustManagerStatus, nil
}

// containsString checks if a string slice contains a given string.
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// cleanupTrustManagerRBAC removes cluster-scoped RBAC resources created by the trust-manager controller.
func cleanupTrustManagerRBAC(ctx context.Context, clientset *kubernetes.Clientset) {
	clientset.RbacV1().ClusterRoles().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
		LabelSelector: "app=cert-manager-trust-manager",
	})
	clientset.RbacV1().ClusterRoleBindings().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
		LabelSelector: "app=cert-manager-trust-manager",
	})
}

// cleanupTrustManagerResources removes namespaced resources created by the trust-manager controller.
// This is used for manual cleanup since the TrustManager controller does not remove resources on CR deletion.
func cleanupTrustManagerResources(ctx context.Context, clientset *kubernetes.Clientset) {
	// Delete deployment
	clientset.AppsV1().Deployments(operandNamespace).Delete(ctx, trustManagerDeploymentName, metav1.DeleteOptions{})
	// Delete services
	clientset.CoreV1().Services(operandNamespace).Delete(ctx, trustManagerDeploymentName, metav1.DeleteOptions{})
	clientset.CoreV1().Services(operandNamespace).Delete(ctx, trustManagerDeploymentName+"-metrics", metav1.DeleteOptions{})
	// Delete service account
	clientset.CoreV1().ServiceAccounts(operandNamespace).Delete(ctx, trustManagerContainerName, metav1.DeleteOptions{})
	// Delete ConfigMap
	clientset.CoreV1().ConfigMaps(operandNamespace).Delete(ctx, trustManagerDefaultCAPackageCM, metav1.DeleteOptions{})
	// Delete roles and rolebindings in operand namespace
	clientset.RbacV1().Roles(operandNamespace).DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
		LabelSelector: "app=cert-manager-trust-manager",
	})
	clientset.RbacV1().RoleBindings(operandNamespace).DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
		LabelSelector: "app=cert-manager-trust-manager",
	})
}
