//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/uuid"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

const approverPolicyName = "cert-manager-approver-policy"

// approverPolicyHoldFinalizer is a synthetic, e2e-only finalizer. The "finalizer-gated cleanup"
// scenario adds it to the operator-managed TLS Secret to make the API server accept a DELETE on
// that Secret without ever actually removing it (any finalizer blocks removal until cleared).
// This simulates "the controller asked for a managed resource to be deleted, but it isn't gone
// yet," so the test can assert that the ApproverPolicyManager CR's own cleanup correctly stays
// in progress - keeping its finalizer and reporting Ready=False/InProgress - until every managed
// resource is actually absent.
const approverPolicyHoldFinalizer = "e2e.cert-manager-operator.openshift.io/hold-cleanup"

var _ = Describe("ApproverPolicyManager", Serial, Ordered,
	Label("Platform:Generic", "Feature:ApproverPolicyManager", "TechPreview"), func() {
		var (
			ctx                      = context.Background()
			client                   crclient.Client
			originalGates            string
			originalOperatorLogLevel string
		)

		BeforeAll(func() {
			client = approverPolicyManagerBeforeAll(ctx, &originalGates, &originalOperatorLogLevel, true)
		})

		AfterAll(func() {
			approverPolicyManagerAfterAll(ctx, &originalGates, &originalOperatorLogLevel)
		})

		BeforeEach(func() {
			By("waiting for operator status to become available")
			Expect(VerifyHealthyOperatorConditions(certmanageroperatorclient.OperatorV1alpha1())).To(Succeed())
		})

		AfterEach(func() {
			approverPolicyManagerAfterEach(ctx, client)
		})

		// ---------------------------------------------------------------------
		// Resource creation
		// ---------------------------------------------------------------------

		Context("resource creation", func() {
			It("should create every managed resource with the expected labels and ownership", func() {
				instance := createApproverPolicyManager(ctx, newApproverPolicyManagerCR())

				for _, obj := range approverPolicyResources() {
					By("verifying " + resourceDescription(obj))
					Eventually(func(g Gomega) {
						g.Expect(client.Get(ctx, crclient.ObjectKeyFromObject(obj), obj)).To(Succeed())
						verifyApproverPolicyManagedLabels(obj.GetLabels())
						g.Expect(metav1.IsControlledBy(obj, instance)).To(BeTrue())
					}, lowTimeout, fastPollInterval).Should(Succeed())
				}
			})
		})

		// ---------------------------------------------------------------------
		// Resource deletion and recreation
		// ---------------------------------------------------------------------

		Context("resource deletion and recreation", func() {
			It("should recreate every managed resource when deleted externally", func() {
				createApproverPolicyManager(ctx, newApproverPolicyManagerCR())

				for _, obj := range approverPolicyResources() {
					By("deleting and verifying recreation of " + resourceDescription(obj))
					verifyApproverPolicyResourceRecreated(ctx, client, obj)
				}
			})
		})

		// ---------------------------------------------------------------------
		// Label drift reconciliation
		// ---------------------------------------------------------------------

		Context("label drift reconciliation", func() {
			It("should restore a managed label modified externally on every managed resource", func() {
				createApproverPolicyManager(ctx, newApproverPolicyManagerCR())

				for _, obj := range approverPolicyResources() {
					By("tampering with the managed label on " + resourceDescription(obj))
					Expect(client.Get(ctx, crclient.ObjectKeyFromObject(obj), obj)).To(Succeed())
					labels := obj.GetLabels()
					labels["app"] = "tampered"
					obj.SetLabels(labels)
					Expect(client.Update(ctx, obj)).To(Succeed())

					By("verifying the controller restores it on " + resourceDescription(obj))
					Eventually(func(g Gomega) {
						g.Expect(client.Get(ctx, crclient.ObjectKeyFromObject(obj), obj)).To(Succeed())
						g.Expect(obj.GetLabels()).To(HaveKeyWithValue("app", approverPolicyName))
					}, lowTimeout, fastPollInterval).Should(Succeed())
				}
			})

			It("should restore a Deployment pod template label modified externally", func() {
				// The pod template's labels (Spec.Template.Labels) are reconciled separately from
				// the Deployment's own ObjectMeta.Labels (see deploymentModified), and aren't
				// reachable through the generic crclient.Object interface the test above loops
				// over, so this scenario needs its own typed check.
				createApproverPolicyManager(ctx, newApproverPolicyManagerCR())

				deploymentKey := crclient.ObjectKey{Namespace: operandNamespace, Name: approverPolicyName}
				deployment := &appsv1.Deployment{}
				Expect(client.Get(ctx, deploymentKey, deployment)).To(Succeed())
				deployment.Spec.Template.Labels["app.kubernetes.io/name"] = "tampered"
				Expect(client.Update(ctx, deployment)).To(Succeed())

				By("verifying the controller restores the Deployment's pod template label")
				Eventually(func(g Gomega) {
					g.Expect(client.Get(ctx, deploymentKey, deployment)).To(Succeed())
					g.Expect(deployment.Spec.Template.Labels).To(HaveKeyWithValue("app.kubernetes.io/name", approverPolicyName))
				}, lowTimeout, fastPollInterval).Should(Succeed())
			})
		})

		// ---------------------------------------------------------------------
		// Managed label removal reconciliation
		// ---------------------------------------------------------------------

		Context("managed label removal reconciliation", func() {
			It("should restore the managed label when removed externally from every managed resource", func() {
				createApproverPolicyManager(ctx, newApproverPolicyManagerCR())

				// The "app" label is the managed resource label the predicate uses to filter
				// watch events. Removing it (rather than just changing its value) tests that the
				// predicate checks both old and new objects on updates, so the event isn't
				// silently dropped.
				for _, obj := range approverPolicyResources() {
					By("removing the managed label from " + resourceDescription(obj))
					Expect(client.Get(ctx, crclient.ObjectKeyFromObject(obj), obj)).To(Succeed())
					labels := obj.GetLabels()
					delete(labels, "app")
					obj.SetLabels(labels)
					Expect(client.Update(ctx, obj)).To(Succeed())

					By("verifying the controller restores it on " + resourceDescription(obj))
					Eventually(func(g Gomega) {
						g.Expect(client.Get(ctx, crclient.ObjectKeyFromObject(obj), obj)).To(Succeed())
						g.Expect(obj.GetLabels()).To(HaveKeyWithValue("app", approverPolicyName))
					}, lowTimeout, fastPollInterval).Should(Succeed())
				}
			})
		})

		// ---------------------------------------------------------------------
		// Deployment configuration
		// ---------------------------------------------------------------------

		Context("deployment configuration", func() {
			deploymentKey := crclient.ObjectKey{Namespace: operandNamespace, Name: approverPolicyName}

			It("should default log level to 1 and log format to text", func() {
				createApproverPolicyManager(ctx, newApproverPolicyManagerCR())

				deployment := &appsv1.Deployment{}
				Expect(client.Get(ctx, deploymentKey, deployment)).To(Succeed())
				Expect(deployment.Spec.Template.Spec.Containers[0].Args).To(ContainElements("--log-level=1", "--log-format=text"))
			})

			It("should update container args when log level and log format change", func() {
				createApproverPolicyManager(ctx, newApproverPolicyManagerCR())

				updateApproverPolicyManager(ctx, func(current *v1alpha1.ApproverPolicyManager) {
					current.Spec.ApproverPolicyConfig.LogLevel = 4
					current.Spec.ApproverPolicyConfig.LogFormat = "json"
				})
				waitForApproverPolicyReady(ctx)

				deployment := &appsv1.Deployment{}
				Expect(client.Get(ctx, deploymentKey, deployment)).To(Succeed())
				Expect(deployment.Spec.Template.Spec.Containers[0].Args).To(ContainElements("--log-level=4", "--log-format=json"))
			})

			It("should apply custom resource requirements to the container", func() {
				resources := corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("25m"),
						corev1.ResourceMemory: resource.MustParse("64Mi"),
					},
				}
				createApproverPolicyManager(ctx, newApproverPolicyManagerCR().WithResources(resources))

				deployment := &appsv1.Deployment{}
				Expect(client.Get(ctx, deploymentKey, deployment)).To(Succeed())
				Expect(deployment.Spec.Template.Spec.Containers[0].Resources).To(Equal(resources))
			})

			It("should apply custom tolerations to the pod spec", func() {
				tolerations := []corev1.Toleration{{
					Key: "dedicated", Operator: corev1.TolerationOpEqual, Value: "approver-policy", Effect: corev1.TaintEffectNoSchedule,
				}}
				createApproverPolicyManager(ctx, newApproverPolicyManagerCR().WithTolerations(tolerations))

				deployment := &appsv1.Deployment{}
				Expect(client.Get(ctx, deploymentKey, deployment)).To(Succeed())
				Expect(deployment.Spec.Template.Spec.Tolerations).To(Equal(tolerations))
			})

			It("should merge a custom nodeSelector with the default kubernetes.io/os selector", func() {
				createApproverPolicyManager(ctx, newApproverPolicyManagerCR().WithNodeSelector(map[string]string{"disktype": "ssd"}))

				deployment := &appsv1.Deployment{}
				Expect(client.Get(ctx, deploymentKey, deployment)).To(Succeed())
				Expect(deployment.Spec.Template.Spec.NodeSelector).To(HaveKeyWithValue("disktype", "ssd"))
				Expect(deployment.Spec.Template.Spec.NodeSelector).To(HaveKeyWithValue("kubernetes.io/os", "linux"))
			})

			It("should apply custom affinity to the pod spec", func() {
				affinity := &corev1.Affinity{
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{{
								MatchExpressions: []corev1.NodeSelectorRequirement{{
									Key: "kubernetes.io/os", Operator: corev1.NodeSelectorOpIn, Values: []string{"linux"},
								}},
							}},
						},
					},
				}
				createApproverPolicyManager(ctx, newApproverPolicyManagerCR().WithAffinity(affinity))

				deployment := &appsv1.Deployment{}
				Expect(client.Get(ctx, deploymentKey, deployment)).To(Succeed())
				Expect(deployment.Spec.Template.Spec.Affinity).To(Equal(affinity))
			})
		})

		// ---------------------------------------------------------------------
		// RBAC configuration
		// ---------------------------------------------------------------------

		Context("RBAC configuration", func() {
			It("should configure ClusterRoleBinding with the operand ServiceAccount as subject", func() {
				createApproverPolicyManager(ctx, newApproverPolicyManagerCR())

				crb := &rbacv1.ClusterRoleBinding{}
				Expect(client.Get(ctx, crclient.ObjectKey{Name: approverPolicyName}, crb)).To(Succeed())
				Expect(crb.RoleRef).To(Equal(rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: approverPolicyName}))
				Expect(crb.Subjects).To(ConsistOf(rbacv1.Subject{Kind: "ServiceAccount", Name: approverPolicyName, Namespace: operandNamespace}))
			})

			It("should configure RoleBinding with the operand ServiceAccount as subject", func() {
				createApproverPolicyManager(ctx, newApproverPolicyManagerCR())

				rb := &rbacv1.RoleBinding{}
				Expect(client.Get(ctx, crclient.ObjectKey{Namespace: operandNamespace, Name: approverPolicyName}, rb)).To(Succeed())
				Expect(rb.RoleRef).To(Equal(rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: approverPolicyName}))
				Expect(rb.Subjects).To(ConsistOf(rbacv1.Subject{Kind: "ServiceAccount", Name: approverPolicyName, Namespace: operandNamespace}))
			})

			It("should scope the ClusterRole's signer approval rule to the configured signer names without rolling the Deployment", func() {
				createApproverPolicyManager(ctx, newApproverPolicyManagerCR())

				deploymentKey := crclient.ObjectKey{Namespace: operandNamespace, Name: approverPolicyName}
				deployment := &appsv1.Deployment{}
				Expect(client.Get(ctx, deploymentKey, deployment)).To(Succeed())
				generation := deployment.Generation

				for _, names := range [][]string{
					{"external.example/*"},
					{"issuers.cert-manager.io/*", "clusterissuers.cert-manager.io/*"},
					nil,
				} {
					By(fmt.Sprintf("setting approveSignerNames to %v", names))
					updateApproverPolicyManager(ctx, func(current *v1alpha1.ApproverPolicyManager) {
						current.Spec.ApproverPolicyConfig.ApproveSignerNames = names
					})
					waitForApproverPolicyReady(ctx)

					role := &rbacv1.ClusterRole{}
					Expect(client.Get(ctx, crclient.ObjectKey{Name: approverPolicyName}, role)).To(Succeed())
					signerRule, found := findSignerRule(role.Rules)
					Expect(found).To(BeTrue())
					Expect(signerRule.ResourceNames).To(ConsistOf(names))

					Expect(client.Get(ctx, deploymentKey, deployment)).To(Succeed())
					Expect(deployment.Generation).To(Equal(generation), "RBAC-only changes must not roll the Deployment")
				}
			})
		})

		// ---------------------------------------------------------------------
		// Webhook and certificate configuration
		// ---------------------------------------------------------------------

		Context("webhook and certificate configuration", func() {
			secretKey := crclient.ObjectKey{Namespace: operandNamespace, Name: approverPolicyName + "-tls"}
			webhookKey := crclient.ObjectKey{Name: approverPolicyName}

			It("should provision TLS secret data and a webhook CA bundle", func() {
				createApproverPolicyManager(ctx, newApproverPolicyManagerCR())

				secret, webhook := &corev1.Secret{}, &admissionregistrationv1.ValidatingWebhookConfiguration{}
				Eventually(func(g Gomega) {
					g.Expect(client.Get(ctx, secretKey, secret)).To(Succeed())
					g.Expect(secret.Data["tls.key"]).NotTo(BeEmpty())
					g.Expect(client.Get(ctx, webhookKey, webhook)).To(Succeed())
					g.Expect(webhook.Webhooks[0].ClientConfig.CABundle).NotTo(BeEmpty())
				}, highTimeout, fastPollInterval).Should(Succeed())
			})

			It("should not recreate the TLS Secret when unrelated spec fields change", func() {
				createApproverPolicyManager(ctx, newApproverPolicyManagerCR())

				secret := &corev1.Secret{}
				Eventually(func(g Gomega) {
					g.Expect(client.Get(ctx, secretKey, secret)).To(Succeed())
					g.Expect(secret.Data["tls.key"]).NotTo(BeEmpty())
				}, highTimeout, fastPollInterval).Should(Succeed())
				secretUID := secret.UID

				updateApproverPolicyManager(ctx, func(current *v1alpha1.ApproverPolicyManager) {
					current.Spec.ApproverPolicyConfig.LogLevel = 4
				})
				waitForApproverPolicyReady(ctx)

				Expect(client.Get(ctx, secretKey, secret)).To(Succeed())
				Expect(secret.UID).To(Equal(secretUID))
				Expect(secret.Data["tls.key"]).NotTo(BeEmpty())
			})

			It("should configure the webhook with the cert-manager CA injection annotation and service reference", func() {
				createApproverPolicyManager(ctx, newApproverPolicyManagerCR())

				webhook := &admissionregistrationv1.ValidatingWebhookConfiguration{}
				Expect(client.Get(ctx, webhookKey, webhook)).To(Succeed())
				Expect(webhook.Annotations).To(HaveKeyWithValue(
					"cert-manager.io/inject-ca-from-secret", fmt.Sprintf("%s/%s-tls", operandNamespace, approverPolicyName)))
				Expect(webhook.Webhooks[0].ClientConfig.Service.Name).To(Equal(approverPolicyName))
				Expect(webhook.Webhooks[0].ClientConfig.Service.Namespace).To(Equal(operandNamespace))
			})
		})

		// ---------------------------------------------------------------------
		// Status reporting
		// ---------------------------------------------------------------------

		Context("status reporting", func() {
			It("should report the approver-policy image used by the Deployment", func() {
				createApproverPolicyManager(ctx, newApproverPolicyManagerCR())

				deployment := &appsv1.Deployment{}
				Expect(client.Get(ctx, crclient.ObjectKey{Namespace: operandNamespace, Name: approverPolicyName}, deployment)).To(Succeed())

				instance, err := approverPolicyClient().Get(ctx, "cluster", metav1.GetOptions{})
				Expect(err).ShouldNot(HaveOccurred())
				Expect(instance.Status.ApproverPolicyImage).To(Equal(deployment.Spec.Template.Spec.Containers[0].Image))
			})
		})

		// ---------------------------------------------------------------------
		// Custom labels and annotations
		// ---------------------------------------------------------------------

		Context("custom labels and annotations", func() {
			It("should apply custom labels from controllerConfig to all managed resources", func() {
				createApproverPolicyManager(ctx, newApproverPolicyManagerCR().WithLabels(map[string]string{"custom-label": "custom-value"}))

				for _, obj := range approverPolicyResources() {
					By("verifying custom label on " + resourceDescription(obj))
					Eventually(func(g Gomega) {
						g.Expect(client.Get(ctx, crclient.ObjectKeyFromObject(obj), obj)).To(Succeed())
						g.Expect(obj.GetLabels()).To(HaveKeyWithValue("custom-label", "custom-value"))
					}, lowTimeout, fastPollInterval).Should(Succeed())
				}
			})

			It("should apply custom annotations without overriding the webhook's CA injection annotation", func() {
				createApproverPolicyManager(ctx, newApproverPolicyManagerCR().WithAnnotations(map[string]string{"custom-annotation": "annotation-value"}))

				webhook := &admissionregistrationv1.ValidatingWebhookConfiguration{}
				Eventually(func(g Gomega) {
					g.Expect(client.Get(ctx, crclient.ObjectKey{Name: approverPolicyName}, webhook)).To(Succeed())
					g.Expect(webhook.Annotations).To(HaveKeyWithValue("custom-annotation", "annotation-value"))
					g.Expect(webhook.Annotations).To(HaveKeyWithValue(
						"cert-manager.io/inject-ca-from-secret", fmt.Sprintf("%s/%s-tls", operandNamespace, approverPolicyName)))
				}, lowTimeout, fastPollInterval).Should(Succeed())
			})

			It("should remove a previously configured label when dropped from the spec", func() {
				createApproverPolicyManager(ctx, newApproverPolicyManagerCR().WithLabels(map[string]string{"e2e.example/remove": "value"}))

				secretKey := crclient.ObjectKey{Namespace: operandNamespace, Name: approverPolicyName + "-tls"}
				secret := &corev1.Secret{}
				Eventually(func(g Gomega) {
					g.Expect(client.Get(ctx, secretKey, secret)).To(Succeed())
					g.Expect(secret.Labels).To(HaveKeyWithValue("e2e.example/remove", "value"))
				}, lowTimeout, fastPollInterval).Should(Succeed())

				updateApproverPolicyManager(ctx, func(current *v1alpha1.ApproverPolicyManager) {
					current.Spec.ControllerConfig.Labels = nil
				})
				waitForApproverPolicyReady(ctx)

				Expect(client.Get(ctx, secretKey, secret)).To(Succeed())
				Expect(secret.Labels).NotTo(HaveKey("e2e.example/remove"))
			})
		})

		// ---------------------------------------------------------------------
		// Singleton validation
		// ---------------------------------------------------------------------

		Context("singleton validation", func() {
			It("should reject ApproverPolicyManager with a name other than 'cluster'", func() {
				_, err := approverPolicyClient().Create(ctx, &v1alpha1.ApproverPolicyManager{
					ObjectMeta: metav1.ObjectMeta{Name: "invalid-name"},
					Spec:       v1alpha1.ApproverPolicyManagerSpec{ApproverPolicyConfig: v1alpha1.ApproverPolicyManagerConfig{}},
				}, metav1.CreateOptions{})
				Expect(err).Should(HaveOccurred())
				Expect(err.Error()).Should(ContainSubstring("ApproverPolicyManager is a singleton"))
				Expect(err.Error()).Should(ContainSubstring(".metadata.name must be 'cluster'"))
			})
		})

		// ---------------------------------------------------------------------
		// Finalizer-gated cleanup
		// ---------------------------------------------------------------------

		Context("finalizer-gated cleanup", func() {
			It("should wait for a held managed resource before completing cleanup, leaving unrelated resources untouched", func() {
				instance := createApproverPolicyManager(ctx, newApproverPolicyManagerCR())

				By("creating a user CertificateRequestPolicy unrelated to the operator's own managed resources")
				policy := &unstructured.Unstructured{Object: map[string]interface{}{
					"apiVersion": "policy.cert-manager.io/v1alpha1",
					"kind":       "CertificateRequestPolicy",
					"metadata":   map[string]interface{}{"name": "approver-e2e-" + string(uuid.NewUUID())},
					"spec":       map[string]interface{}{"selector": map[string]interface{}{"issuerRef": map[string]interface{}{}}},
				}}
				Expect(client.Create(ctx, policy)).To(Succeed())
				DeferCleanup(func() { Expect(crclient.IgnoreNotFound(client.Delete(context.Background(), policy))).To(Succeed()) })

				By("holding the TLS Secret so its deletion is accepted but never completes")
				secretKey := crclient.ObjectKey{Namespace: operandNamespace, Name: approverPolicyName + "-tls"}
				secret := &corev1.Secret{}
				Expect(client.Get(ctx, secretKey, secret)).To(Succeed())
				secret.Finalizers = append(secret.Finalizers, approverPolicyHoldFinalizer)
				Expect(client.Update(ctx, secret)).To(Succeed())

				By("deleting the ApproverPolicyManager CR")
				uid := instance.UID
				Expect(approverPolicyClient().Delete(ctx, "cluster", metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})).To(Succeed())

				By("verifying cleanup stays in progress while the held Secret cannot be deleted")
				Eventually(func(g Gomega) {
					g.Expect(client.Get(ctx, secretKey, secret)).To(Succeed())
					g.Expect(secret.DeletionTimestamp.IsZero()).To(BeFalse())

					current, err := approverPolicyClient().Get(ctx, "cluster", metav1.GetOptions{})
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(current.Finalizers).NotTo(BeEmpty())

					condition := meta.FindStatusCondition(current.Status.Conditions, v1alpha1.Ready)
					g.Expect(condition).NotTo(BeNil())
					g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))
					g.Expect(condition.Reason).To(Equal(v1alpha1.ReasonInProgress))
				}, highTimeout, fastPollInterval).Should(Succeed())

				By("releasing the held Secret and letting cleanup complete")
				releaseApproverPolicyTLSSecretHold(ctx, client)
				Eventually(func() bool {
					_, err := approverPolicyClient().Get(ctx, "cluster", metav1.GetOptions{})
					return apierrors.IsNotFound(err)
				}, highTimeout, fastPollInterval).Should(BeTrue())

				By("verifying all managed resources were removed")
				for _, obj := range approverPolicyResources() {
					Expect(apierrors.IsNotFound(client.Get(ctx, crclient.ObjectKeyFromObject(obj), obj))).To(BeTrue())
				}
				pods := &corev1.PodList{}
				Expect(client.List(ctx, pods, crclient.InNamespace(operandNamespace), crclient.MatchingLabels{"app": approverPolicyName})).To(Succeed())
				Expect(pods.Items).To(BeEmpty())

				By("verifying the unrelated user policy and shared namespace were left untouched")
				Expect(client.Get(ctx, crclient.ObjectKeyFromObject(policy), policy)).To(Succeed())
				Expect(client.Get(ctx, crclient.ObjectKey{Name: operandNamespace}, &corev1.Namespace{})).To(Succeed())
			})
		})
	})

// TechPreview:Inverted labels test scenarios that invert the main ApproverPolicyManager suite
// (feature gate off): creating the CR must not deploy any operand resource.
var _ = Describe("ApproverPolicyManager with the operator gate disabled", Serial, Ordered,
	Label("Platform:Generic", "Feature:ApproverPolicyManager", "TechPreview:Inverted"), func() {
		var (
			ctx                      = context.Background()
			client                   crclient.Client
			originalGates            string
			originalOperatorLogLevel string
		)

		BeforeAll(func() {
			client = approverPolicyManagerBeforeAll(ctx, &originalGates, &originalOperatorLogLevel, false)
		})

		AfterAll(func() {
			approverPolicyManagerAfterAll(ctx, &originalGates, &originalOperatorLogLevel)
		})

		BeforeEach(func() {
			By("waiting for operator status to become available")
			Expect(VerifyHealthyOperatorConditions(certmanageroperatorclient.OperatorV1alpha1())).To(Succeed())
		})

		AfterEach(func() {
			approverPolicyManagerAfterEach(ctx, client)
		})

		It("should leave the singleton without finalizers, conditions, or operand resources", func() {
			_, err := approverPolicyClient().Create(ctx, newApproverPolicyManagerCR().Build(), metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			Consistently(func(g Gomega) {
				current, err := approverPolicyClient().Get(ctx, "cluster", metav1.GetOptions{})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(current.Finalizers).To(BeEmpty())
				g.Expect(current.Status.Conditions).To(BeEmpty())
				for _, obj := range approverPolicyResources() {
					g.Expect(apierrors.IsNotFound(client.Get(ctx, crclient.ObjectKeyFromObject(obj), obj))).To(BeTrue())
				}
			}, 40*time.Second, fastPollInterval).Should(Succeed())
		})
	})
