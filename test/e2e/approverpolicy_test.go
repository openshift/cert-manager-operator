//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/uuid"
	clientscheme "k8s.io/client-go/kubernetes/scheme"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

const approverPolicyName = "cert-manager-approver-policy"
const approverPolicyHoldFinalizer = "e2e.cert-manager-operator.openshift.io/hold-cleanup"

var _ = Describe("ApproverPolicyManager lifecycle", Serial, Ordered,
	Label("Platform:Generic", "Feature:ApproverPolicyManager", "TechPreview"), func() {
		var live crclient.Client
		BeforeAll(func(ctx SpecContext) {
			live = configureApproverPolicySuite(ctx, true)
		})

		It("installs, configures, repairs and cleans up the operand", func(ctx SpecContext) {
			managers := certmanageroperatorclient.OperatorV1alpha1().ApproverPolicyManagers()
			instance, err := managers.Create(ctx, &v1alpha1.ApproverPolicyManager{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: v1alpha1.ApproverPolicyManagerSpec{
					ApproverPolicyConfig: v1alpha1.ApproverPolicyManagerConfig{LogLevel: 1, LogFormat: "text"},
				},
			}, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred()) // AlreadyExists is never creation success.
			uid := instance.UID
			DeferCleanup(func() {
				cleanupCtx := context.Background()
				secret := &corev1.Secret{}
				key := crclient.ObjectKey{Namespace: operandNamespace, Name: approverPolicyName + "-tls"}
				if live.Get(cleanupCtx, key, secret) == nil && slices.Contains(secret.Finalizers, approverPolicyHoldFinalizer) {
					secret.Finalizers = slices.DeleteFunc(secret.Finalizers, func(s string) bool { return s == approverPolicyHoldFinalizer })
					Expect(live.Update(cleanupCtx, secret)).To(Succeed())
				}
				err := managers.Delete(cleanupCtx, "cluster", metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})
				Expect(crclient.IgnoreNotFound(err)).To(Succeed())
				Eventually(func() bool {
					_, err := managers.Get(cleanupCtx, "cluster", metav1.GetOptions{})
					return apierrors.IsNotFound(err)
				}, highTimeout, fastPollInterval).Should(BeTrue())
			})
			waitForApproverPolicyReady(ctx)
			for _, obj := range approverPolicyResources() {
				Expect(live.Get(ctx, crclient.ObjectKeyFromObject(obj), obj)).To(Succeed())
				Expect(obj.GetLabels()["app"]).To(Equal(approverPolicyName))
				Expect(metav1.IsControlledBy(obj, instance)).To(BeTrue())
			}
			deployment := &appsv1.Deployment{}
			deploymentKey := crclient.ObjectKey{Namespace: operandNamespace, Name: approverPolicyName}
			Expect(live.Get(ctx, deploymentKey, deployment)).To(Succeed())
			instance, err = managers.Get(ctx, "cluster", metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(instance.Status.ApproverPolicyImage).To(Equal(deployment.Spec.Template.Spec.Containers[0].Image))

			By("preserving dynamically generated TLS material during metadata and logging changes")
			secret := &corev1.Secret{}
			secretKey := crclient.ObjectKey{Namespace: operandNamespace, Name: approverPolicyName + "-tls"}
			webhook := &admissionregistrationv1.ValidatingWebhookConfiguration{}
			Eventually(func(g Gomega) {
				g.Expect(live.Get(ctx, secretKey, secret)).To(Succeed())
				g.Expect(secret.Data["tls.key"]).NotTo(BeEmpty())
				g.Expect(live.Get(ctx, crclient.ObjectKey{Name: approverPolicyName}, webhook)).To(Succeed())
				g.Expect(webhook.Webhooks[0].ClientConfig.CABundle).NotTo(BeEmpty())
			}, highTimeout, fastPollInterval).Should(Succeed())
			secretUID := secret.UID
			Eventually(func() error {
				current, err := managers.Get(ctx, "cluster", metav1.GetOptions{})
				if err != nil {
					return err
				}
				current.Spec.ApproverPolicyConfig.LogLevel = 4
				current.Spec.ApproverPolicyConfig.LogFormat = "json"
				current.Spec.ControllerConfig.Labels = map[string]string{"e2e.example/remove": "value"}
				_, err = managers.Update(ctx, current, metav1.UpdateOptions{})
				return err
			}, lowTimeout, fastPollInterval).Should(Succeed())
			waitForApproverPolicyReady(ctx)
			Expect(live.Get(ctx, deploymentKey, deployment)).To(Succeed())
			Expect(deployment.Spec.Template.Spec.Containers[0].Args).To(ContainElements("--log-level=4", "--log-format=json"))
			Expect(live.Get(ctx, secretKey, secret)).To(Succeed())
			Expect(secret.UID).To(Equal(secretUID))
			Expect(secret.Data["tls.key"]).NotTo(BeEmpty())
			Expect(secret.Labels).To(HaveKeyWithValue("e2e.example/remove", "value"))

			By("changing and clearing signer scope without rolling the Deployment")
			generation := deployment.Generation
			for _, names := range [][]string{{"external.example/*"}, {"issuers.cert-manager.io/*", "clusterissuers.cert-manager.io/*"}, nil} {
				Eventually(func() error {
					current, err := managers.Get(ctx, "cluster", metav1.GetOptions{})
					if err != nil {
						return err
					}
					current.Spec.ApproverPolicyConfig.ApproveSignerNames = names
					_, err = managers.Update(ctx, current, metav1.UpdateOptions{})
					return err
				}, lowTimeout, fastPollInterval).Should(Succeed())
				waitForApproverPolicyReady(ctx)
				role := &rbacv1.ClusterRole{}
				Expect(live.Get(ctx, crclient.ObjectKey{Name: approverPolicyName}, role)).To(Succeed())
				var found bool
				for _, rule := range role.Rules {
					if slices.Equal(rule.Resources, []string{"signers"}) {
						found = true
						Expect(rule.ResourceNames).To(ConsistOf(names))
					}
				}
				Expect(found).To(BeTrue())
				Expect(live.Get(ctx, deploymentKey, deployment)).To(Succeed())
				Expect(deployment.Generation).To(Equal(generation))
			}

			By("removing a previously configured label and recreating a deleted Service")
			Eventually(func() error {
				current, err := managers.Get(ctx, "cluster", metav1.GetOptions{})
				if err != nil {
					return err
				}
				current.Spec.ControllerConfig.Labels = nil
				_, err = managers.Update(ctx, current, metav1.UpdateOptions{})
				return err
			}, lowTimeout, fastPollInterval).Should(Succeed())
			waitForApproverPolicyReady(ctx)
			Expect(live.Get(ctx, secretKey, secret)).To(Succeed())
			Expect(secret.Labels).NotTo(HaveKey("e2e.example/remove"))
			service := &corev1.Service{}
			serviceKey := crclient.ObjectKey{Namespace: operandNamespace, Name: approverPolicyName + "-metrics"}
			Expect(live.Get(ctx, serviceKey, service)).To(Succeed())
			oldUID := service.UID
			Expect(live.Delete(ctx, service)).To(Succeed())
			Eventually(func(g Gomega) {
				g.Expect(live.Get(ctx, serviceKey, service)).To(Succeed())
				g.Expect(service.UID).NotTo(Equal(oldUID))
			}, highTimeout, fastPollInterval).Should(Succeed())

			By("preserving a user policy while finalizer-gated cleanup waits for a held Secret")
			policy := &unstructured.Unstructured{Object: map[string]interface{}{
				"apiVersion": "policy.cert-manager.io/v1alpha1", "kind": "CertificateRequestPolicy",
				"metadata": map[string]interface{}{"name": "approver-e2e-" + string(uuid.NewUUID())},
				"spec":     map[string]interface{}{"selector": map[string]interface{}{"issuerRef": map[string]interface{}{}}},
			}}
			Expect(live.Create(ctx, policy)).To(Succeed())
			DeferCleanup(func() { Expect(crclient.IgnoreNotFound(live.Delete(context.Background(), policy))).To(Succeed()) })
			Expect(live.Get(ctx, secretKey, secret)).To(Succeed())
			secret.Finalizers = append(secret.Finalizers, approverPolicyHoldFinalizer)
			Expect(live.Update(ctx, secret)).To(Succeed())
			Expect(managers.Delete(ctx, "cluster", metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})).To(Succeed())
			Eventually(func(g Gomega) {
				g.Expect(live.Get(ctx, secretKey, secret)).To(Succeed())
				g.Expect(secret.DeletionTimestamp.IsZero()).To(BeFalse())
				current, err := managers.Get(ctx, "cluster", metav1.GetOptions{})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(current.Finalizers).NotTo(BeEmpty())
				condition := meta.FindStatusCondition(current.Status.Conditions, v1alpha1.Ready)
				g.Expect(condition).NotTo(BeNil())
				g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(condition.Reason).To(Equal(v1alpha1.ReasonInProgress))
			}, highTimeout, fastPollInterval).Should(Succeed())
			secret.Finalizers = slices.DeleteFunc(secret.Finalizers, func(s string) bool { return s == approverPolicyHoldFinalizer })
			Expect(live.Update(ctx, secret)).To(Succeed())
			Eventually(func() bool {
				_, err := managers.Get(ctx, "cluster", metav1.GetOptions{})
				return apierrors.IsNotFound(err)
			}, highTimeout, fastPollInterval).Should(BeTrue())
			for _, obj := range approverPolicyResources() {
				Expect(apierrors.IsNotFound(live.Get(ctx, crclient.ObjectKeyFromObject(obj), obj))).To(BeTrue())
			}
			pods := &corev1.PodList{}
			Expect(live.List(ctx, pods, crclient.InNamespace(operandNamespace), crclient.MatchingLabels{"app": approverPolicyName})).To(Succeed())
			Expect(pods.Items).To(BeEmpty())
			Expect(live.Get(ctx, crclient.ObjectKeyFromObject(policy), policy)).To(Succeed())
			Expect(live.Get(ctx, crclient.ObjectKey{Name: operandNamespace}, &corev1.Namespace{})).To(Succeed())
		})
	})

var _ = Describe("ApproverPolicyManager with the operator gate disabled", Serial, Ordered,
	Label("Platform:Generic", "Feature:ApproverPolicyManager", "TechPreview:Inverted"), func() {
		var live crclient.Client
		BeforeAll(func(ctx SpecContext) { live = configureApproverPolicySuite(ctx, false) })
		It("leaves the singleton and operand resources untouched", func(ctx SpecContext) {
			managers := certmanageroperatorclient.OperatorV1alpha1().ApproverPolicyManagers()
			instance, err := managers.Create(ctx, &v1alpha1.ApproverPolicyManager{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec:       v1alpha1.ApproverPolicyManagerSpec{ApproverPolicyConfig: v1alpha1.ApproverPolicyManagerConfig{LogLevel: 1, LogFormat: "text"}},
			}, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() {
				Expect(managers.Delete(context.Background(), "cluster", metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &instance.UID}})).To(Succeed())
			})
			Consistently(func(g Gomega) {
				current, err := managers.Get(ctx, "cluster", metav1.GetOptions{})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(current.Finalizers).To(BeEmpty())
				g.Expect(current.Status.Conditions).To(BeEmpty())
				for _, obj := range approverPolicyResources() {
					g.Expect(apierrors.IsNotFound(live.Get(ctx, crclient.ObjectKeyFromObject(obj), obj))).To(BeTrue())
				}
			}, 40*time.Second, fastPollInterval).Should(Succeed())
		})
	})

func configureApproverPolicySuite(ctx context.Context, enabled bool) crclient.Client {
	_, err := certmanageroperatorclient.OperatorV1alpha1().ApproverPolicyManagers().Get(ctx, "cluster", metav1.GetOptions{})
	Expect(apierrors.IsNotFound(err)).To(BeTrue(), "suite requires an absent singleton; never replace an existing user's instance")
	live, err := crclient.New(cfg, crclient.Options{Scheme: clientscheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	for _, obj := range approverPolicyResources() {
		Expect(apierrors.IsNotFound(live.Get(ctx, crclient.ObjectKeyFromObject(obj), obj))).To(BeTrue(), "suite requires no pre-existing operand resources")
	}
	original, err := getSubscriptionEnvVar(ctx, loader, "UNSUPPORTED_ADDON_FEATURES")
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() {
		cleanupCtx := context.Background()
		Expect(patchSubscriptionWithEnvVars(cleanupCtx, loader, map[string]string{"UNSUPPORTED_ADDON_FEATURES": original})).To(Succeed())
		if original == "" {
			Expect(waitForDeploymentEnvVarRemovedAndRollout(cleanupCtx, operatorNamespace, operatorDeploymentName, "UNSUPPORTED_ADDON_FEATURES", lowTimeout)).To(Succeed())
		} else {
			Expect(waitForDeploymentEnvVarAndRollout(cleanupCtx, operatorNamespace, operatorDeploymentName, "UNSUPPORTED_ADDON_FEATURES", original, lowTimeout)).To(Succeed())
		}
	})
	var gates []string
	for _, gate := range strings.Split(original, ",") {
		if gate = strings.TrimSpace(gate); gate != "" && !strings.HasPrefix(gate, "ApproverPolicyManager=") {
			gates = append(gates, gate)
		}
	}
	gates = append(gates, fmt.Sprintf("ApproverPolicyManager=%t", enabled))
	value := strings.Join(gates, ",")
	Expect(patchSubscriptionWithEnvVars(ctx, loader, map[string]string{"UNSUPPORTED_ADDON_FEATURES": value})).To(Succeed())
	Expect(waitForDeploymentEnvVarAndRollout(ctx, operatorNamespace, operatorDeploymentName, "UNSUPPORTED_ADDON_FEATURES", value, lowTimeout)).To(Succeed())
	return live
}

func waitForApproverPolicyReady(ctx context.Context) {
	Eventually(func(g Gomega) {
		current, err := certmanageroperatorclient.OperatorV1alpha1().ApproverPolicyManagers().Get(ctx, "cluster", metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		ready := meta.FindStatusCondition(current.Status.Conditions, v1alpha1.Ready)
		g.Expect(ready).NotTo(BeNil())
		g.Expect(ready.Status).To(Equal(metav1.ConditionTrue))
		g.Expect(ready.Reason).To(Equal(v1alpha1.ReasonReady))
		g.Expect(ready.ObservedGeneration).To(Equal(current.Generation))
		degraded := meta.FindStatusCondition(current.Status.Conditions, v1alpha1.Degraded)
		g.Expect(degraded).NotTo(BeNil())
		g.Expect(degraded.Status).To(Equal(metav1.ConditionFalse))
	}, highTimeout, fastPollInterval).Should(Succeed())
}

func approverPolicyResources() []crclient.Object {
	namespaced := metav1.ObjectMeta{Name: approverPolicyName, Namespace: operandNamespace}
	cluster := metav1.ObjectMeta{Name: approverPolicyName}
	return []crclient.Object{
		&appsv1.Deployment{ObjectMeta: namespaced}, &corev1.ServiceAccount{ObjectMeta: namespaced},
		&rbacv1.Role{ObjectMeta: namespaced}, &rbacv1.RoleBinding{ObjectMeta: namespaced},
		&rbacv1.ClusterRole{ObjectMeta: cluster}, &rbacv1.ClusterRoleBinding{ObjectMeta: cluster},
		&corev1.Service{ObjectMeta: namespaced},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: approverPolicyName + "-metrics", Namespace: operandNamespace}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: approverPolicyName + "-tls", Namespace: operandNamespace}},
		&admissionregistrationv1.ValidatingWebhookConfiguration{ObjectMeta: cluster},
	}
}
