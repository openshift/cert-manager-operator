//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"slices"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientscheme "k8s.io/client-go/kubernetes/scheme"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	operatorclientv1alpha1 "github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned/typed/operator/v1alpha1"
)

// ---------------------------------------------------------------------------
// ApproverPolicyManager CR builder
// ---------------------------------------------------------------------------

type approverPolicyManagerCRBuilder struct {
	apm *v1alpha1.ApproverPolicyManager
}

func newApproverPolicyManagerCR() *approverPolicyManagerCRBuilder {
	return &approverPolicyManagerCRBuilder{
		apm: &v1alpha1.ApproverPolicyManager{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec: v1alpha1.ApproverPolicyManagerSpec{
				ApproverPolicyConfig: v1alpha1.ApproverPolicyManagerConfig{LogLevel: 1, LogFormat: "text"},
			},
		},
	}
}

func (b *approverPolicyManagerCRBuilder) WithLogLevel(level int32) *approverPolicyManagerCRBuilder {
	b.apm.Spec.ApproverPolicyConfig.LogLevel = level
	return b
}

func (b *approverPolicyManagerCRBuilder) WithLogFormat(format string) *approverPolicyManagerCRBuilder {
	b.apm.Spec.ApproverPolicyConfig.LogFormat = format
	return b
}

func (b *approverPolicyManagerCRBuilder) WithApproveSignerNames(names []string) *approverPolicyManagerCRBuilder {
	b.apm.Spec.ApproverPolicyConfig.ApproveSignerNames = names
	return b
}

func (b *approverPolicyManagerCRBuilder) WithResources(resources corev1.ResourceRequirements) *approverPolicyManagerCRBuilder {
	b.apm.Spec.ApproverPolicyConfig.Resources = resources
	return b
}

func (b *approverPolicyManagerCRBuilder) WithAffinity(affinity *corev1.Affinity) *approverPolicyManagerCRBuilder {
	b.apm.Spec.ApproverPolicyConfig.Affinity = affinity
	return b
}

func (b *approverPolicyManagerCRBuilder) WithTolerations(tolerations []corev1.Toleration) *approverPolicyManagerCRBuilder {
	b.apm.Spec.ApproverPolicyConfig.Tolerations = tolerations
	return b
}

func (b *approverPolicyManagerCRBuilder) WithNodeSelector(nodeSelector map[string]string) *approverPolicyManagerCRBuilder {
	b.apm.Spec.ApproverPolicyConfig.NodeSelector = nodeSelector
	return b
}

func (b *approverPolicyManagerCRBuilder) WithLabels(labels map[string]string) *approverPolicyManagerCRBuilder {
	b.apm.Spec.ControllerConfig.Labels = labels
	return b
}

func (b *approverPolicyManagerCRBuilder) WithAnnotations(annotations map[string]string) *approverPolicyManagerCRBuilder {
	b.apm.Spec.ControllerConfig.Annotations = annotations
	return b
}

func (b *approverPolicyManagerCRBuilder) Build() *v1alpha1.ApproverPolicyManager {
	return b.apm
}

// ---------------------------------------------------------------------------
// ApproverPolicyManager CR lifecycle helpers
// ---------------------------------------------------------------------------

func approverPolicyClient() operatorclientv1alpha1.ApproverPolicyManagerInterface {
	return certmanageroperatorclient.OperatorV1alpha1().ApproverPolicyManagers()
}

// createApproverPolicyManager creates the singleton CR from the builder and waits for the
// controller to report it ready (Ready=True, Degraded=False, observed at the current generation).
func createApproverPolicyManager(ctx context.Context, b *approverPolicyManagerCRBuilder) *v1alpha1.ApproverPolicyManager {
	By("creating ApproverPolicyManager CR")
	instance, err := approverPolicyClient().Create(ctx, b.Build(), metav1.CreateOptions{})
	Expect(err).ShouldNot(HaveOccurred())
	waitForApproverPolicyReady(ctx)
	return instance
}

func waitForApproverPolicyReady(ctx context.Context) {
	By("waiting for ApproverPolicyManager CR to be ready")
	Eventually(func(g Gomega) {
		current, err := approverPolicyClient().Get(ctx, "cluster", metav1.GetOptions{})
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

// updateApproverPolicyManager retries on update conflicts: it re-fetches the current CR,
// applies mutate to it, and attempts the update until it succeeds.
func updateApproverPolicyManager(ctx context.Context, mutate func(*v1alpha1.ApproverPolicyManager)) {
	Eventually(func() error {
		current, err := approverPolicyClient().Get(ctx, "cluster", metav1.GetOptions{})
		if err != nil {
			return err
		}
		mutate(current)
		_, err = approverPolicyClient().Update(ctx, current, metav1.UpdateOptions{})
		return err
	}, lowTimeout, fastPollInterval).Should(Succeed())
}

// approverPolicyManagerBeforeAll prepares the suite to safely own the ApproverPolicyManager
// singleton: it asserts no pre-existing instance or operand resources exist, then delegates to
// enableOperatorFeatureGateForSuite to set the ApproverPolicyManager feature gate.
// It returns a controller-runtime client usable against every resource kind
// the controller manages.
func approverPolicyManagerBeforeAll(ctx context.Context, originalGates, originalOperatorLogLevel *string, enabled bool) crclient.Client {
	By("verifying no pre-existing ApproverPolicyManager singleton or operand resources")
	_, err := approverPolicyClient().Get(ctx, "cluster", metav1.GetOptions{})
	Expect(apierrors.IsNotFound(err)).To(BeTrue(), "suite requires an absent singleton; never replace an existing user's instance")

	client, err := crclient.New(cfg, crclient.Options{Scheme: clientscheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	for _, obj := range approverPolicyResources() {
		Expect(apierrors.IsNotFound(client.Get(ctx, crclient.ObjectKeyFromObject(obj), obj))).To(BeTrue(), "suite requires no pre-existing operand resources")
	}

	enableOperatorFeatureGateForSuite(ctx, loader, "ApproverPolicyManager", enabled, true, originalGates, originalOperatorLogLevel)
	return client
}

// approverPolicyManagerAfterAll restores the Subscription's UNSUPPORTED_ADDON_FEATURES and
// OPERATOR_LOG_LEVEL to the values captured by approverPolicyManagerBeforeAll.
func approverPolicyManagerAfterAll(ctx context.Context, originalGates, originalOperatorLogLevel *string) {
	restoreOperatorFeatureGateForSuite(ctx, loader, originalGates, originalOperatorLogLevel)
}

// approverPolicyManagerAfterEach runs after every It: it releases the test-only hold finalizer
// from the TLS Secret in case a failed assertion left it in place mid "finalizer-gated cleanup"
// scenario, then deletes the ApproverPolicyManager CR and waits for the controller to finish
// tearing down every operand resource it owns, so each It starts from a clean cluster state.
func approverPolicyManagerAfterEach(ctx context.Context, client crclient.Client) {
	releaseApproverPolicyTLSSecretHold(ctx, client)

	_ = approverPolicyClient().Delete(ctx, "cluster", metav1.DeleteOptions{})
	Eventually(func() bool {
		_, err := approverPolicyClient().Get(ctx, "cluster", metav1.GetOptions{})
		return apierrors.IsNotFound(err)
	}, highTimeout, fastPollInterval).Should(BeTrue())
}

// releaseApproverPolicyTLSSecretHold removes approverPolicyHoldFinalizer from the TLS Secret, if
// present, so a pending deletion can complete. It is a no-op when the Secret is absent or was
// never held.
func releaseApproverPolicyTLSSecretHold(ctx context.Context, client crclient.Client) {
	secret := &corev1.Secret{}
	key := crclient.ObjectKey{Namespace: operandNamespace, Name: approverPolicyName + "-tls"}
	if client.Get(ctx, key, secret) != nil || !slices.Contains(secret.Finalizers, approverPolicyHoldFinalizer) {
		return
	}
	secret.Finalizers = slices.DeleteFunc(secret.Finalizers, func(f string) bool { return f == approverPolicyHoldFinalizer })
	Expect(client.Update(ctx, secret)).To(Succeed())
}

// ---------------------------------------------------------------------------
// Managed resource helpers
// ---------------------------------------------------------------------------

// approverPolicyResources returns a stub (name/namespace only) for every resource kind the
// controller manages, suitable for passing to client.Get/Delete.
func approverPolicyResources() []crclient.Object {
	namespaced := metav1.ObjectMeta{Name: approverPolicyName, Namespace: operandNamespace}
	cluster := metav1.ObjectMeta{Name: approverPolicyName}
	return []crclient.Object{
		&appsv1.Deployment{ObjectMeta: namespaced},
		&corev1.ServiceAccount{ObjectMeta: namespaced},
		&rbacv1.Role{ObjectMeta: namespaced},
		&rbacv1.RoleBinding{ObjectMeta: namespaced},
		&rbacv1.ClusterRole{ObjectMeta: cluster},
		&rbacv1.ClusterRoleBinding{ObjectMeta: cluster},
		&corev1.Service{ObjectMeta: namespaced},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: approverPolicyName + "-metrics", Namespace: operandNamespace}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: approverPolicyName + "-tls", Namespace: operandNamespace}},
		&admissionregistrationv1.ValidatingWebhookConfiguration{ObjectMeta: cluster},
	}
}

// resourceDescription renders a short "Kind namespace/name" label for a managed resource, for
// use in By() messages when iterating over approverPolicyResources().
func resourceDescription(obj crclient.Object) string {
	kind := fmt.Sprintf("%T", obj)
	if idx := strings.LastIndex(kind, "."); idx != -1 {
		kind = kind[idx+1:]
	}
	return fmt.Sprintf("%s %s", kind, crclient.ObjectKeyFromObject(obj))
}

// verifyApproverPolicyManagedLabels asserts the standard labels prepareResource stamps onto
// every managed resource.
func verifyApproverPolicyManagedLabels(labels map[string]string) {
	verifyManagedResourceLabels(labels, approverPolicyName)
}

// verifyApproverPolicyResourceRecreated deletes obj and waits for the controller to recreate it
// (observed as a new UID at the same name/namespace). obj is updated in place to the recreated
// object's state once the wait succeeds.
func verifyApproverPolicyResourceRecreated(ctx context.Context, client crclient.Client, obj crclient.Object) {
	key := crclient.ObjectKeyFromObject(obj)
	Expect(client.Get(ctx, key, obj)).To(Succeed())
	oldUID := obj.GetUID()
	Expect(client.Delete(ctx, obj)).To(Succeed())
	Eventually(func(g Gomega) {
		g.Expect(client.Get(ctx, key, obj)).To(Succeed())
		g.Expect(obj.GetUID()).NotTo(Equal(oldUID))
	}, lowTimeout, fastPollInterval).Should(Succeed())
}

// findSignerRule returns the "signers" approval PolicyRule from a ClusterRole's rules, if any.
func findSignerRule(rules []rbacv1.PolicyRule) (rbacv1.PolicyRule, bool) {
	for _, rule := range rules {
		if slices.Equal(rule.Resources, []string{"signers"}) {
			return rule, true
		}
	}
	return rbacv1.PolicyRule{}, false
}
