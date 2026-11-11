//go:build e2e

package e2e

import (
	"context"
	"encoding/csv"
	"fmt"
	"slices"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	opv1 "github.com/openshift/api/operator/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	clientscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/util/retry"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
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

func waitForApproverPolicyNotReady(ctx context.Context) {
	Eventually(func(g Gomega) {
		current, err := approverPolicyClient().Get(ctx, "cluster", metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		ready := meta.FindStatusCondition(current.Status.Conditions, v1alpha1.Ready)
		g.Expect(ready).NotTo(BeNil())
		g.Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		g.Expect(ready.ObservedGeneration).To(Equal(current.Generation))
	}, highTimeout, fastPollInterval).Should(Succeed())
}

func verifyAutoApproverState(ctx context.Context, g Gomega, disabled bool) {
	cm, err := certmanageroperatorclient.OperatorV1alpha1().CertManagers().Get(ctx, "cluster", metav1.GetOptions{})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(cm.Status.AutoApproverDisabled).To(Equal(disabled))
	verifyAutoApproverIntent(g, cm, disabled)
	deployment, err := k8sClientSet.AppsV1().Deployments(operandNamespace).Get(ctx, certmanagerControllerDeployment, metav1.GetOptions{})
	g.Expect(err).NotTo(HaveOccurred())
	args := map[string]string{}
	common.ParseArgMap(args, deployment.Spec.Template.Spec.Containers[0].Args)
	if disabled {
		g.Expect(args).To(HaveKey("--controllers"))
		g.Expect(controllersSelectionEnablesAutoApprover(args["--controllers"])).To(BeFalse())
	} else if selection, ok := args["--controllers"]; ok {
		g.Expect(controllersSelectionEnablesAutoApprover(selection)).To(BeTrue())
	}
	const roleName = "cert-manager-controller-approve:cert-manager-io"
	_, roleErr := k8sClientSet.RbacV1().ClusterRoles().Get(ctx, roleName, metav1.GetOptions{})
	_, bindingErr := k8sClientSet.RbacV1().ClusterRoleBindings().Get(ctx, roleName, metav1.GetOptions{})
	if disabled {
		g.Expect(apierrors.IsNotFound(roleErr)).To(BeTrue())
		g.Expect(apierrors.IsNotFound(bindingErr)).To(BeTrue())
	} else {
		g.Expect(roleErr).NotTo(HaveOccurred())
		g.Expect(bindingErr).NotTo(HaveOccurred())
	}
}

// verifyAutoApproverIntent checks the durable handoff latch condition. While the built-in
// approver is disabled, intent must be True. Once restored (or never started), intent must
// not be True — either absent, or explicitly False after ApproverPolicyManager deletion.
func verifyAutoApproverIntent(g Gomega, cm *v1alpha1.CertManager, disabled bool) {
	g.Expect(cm).NotTo(BeNil())
	var intent *opv1.OperatorCondition
	for i := range cm.Status.Conditions {
		if cm.Status.Conditions[i].Type == "AutoApproverDisableRequested" {
			intent = &cm.Status.Conditions[i]
			break
		}
	}
	if disabled {
		g.Expect(intent).NotTo(BeNil())
		g.Expect(intent.Status).To(Equal(opv1.ConditionTrue))
		return
	}
	if intent != nil {
		g.Expect(intent.Status).NotTo(Equal(opv1.ConditionTrue))
	}
}

func listAutoApproverTransitionEvents(ctx context.Context, reason string) ([]corev1.Event, error) {
	cm, err := certmanageroperatorclient.OperatorV1alpha1().CertManagers().Get(ctx, "cluster", metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	// Cluster-scoped CertManager events are stored in the default namespace.
	events, err := k8sClientSet.CoreV1().Events(metav1.NamespaceDefault).List(ctx, metav1.ListOptions{
		FieldSelector: "involvedObject.kind=CertManager,involvedObject.name=cluster",
	})
	if err != nil {
		return nil, err
	}
	matched := make([]corev1.Event, 0, len(events.Items))
	for _, event := range events.Items {
		if event.InvolvedObject.UID != cm.UID || event.Reason != reason || event.Type != corev1.EventTypeNormal {
			continue
		}
		matched = append(matched, event)
	}
	return matched, nil
}

func waitForAutoApproverTransitionEvent(ctx context.Context, reason string) {
	Eventually(func(g Gomega) {
		events, err := listAutoApproverTransitionEvents(ctx, reason)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(events).NotTo(BeEmpty())
	}, highTimeout, fastPollInterval).Should(Succeed())
}

func autoApproverTransitionEventCount(ctx context.Context, reason string) int {
	events, err := listAutoApproverTransitionEvents(ctx, reason)
	Expect(err).NotTo(HaveOccurred())
	total := 0
	for _, event := range events {
		count := int(event.Count)
		if count == 0 {
			count = 1
		}
		total += count
	}
	return total
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

	// AfterEach runs before It-scoped DeferCleanup. If a spec disabled the feature gate and
	// failed before re-enabling it, finalizer-gated APM deletion would hang without this.
	ensureApproverPolicyManagerFeatureGateEnabled(ctx)
	// Drop any controller overrides left by a failed conflict/compatible override scenario so
	// the next Spec is not stuck Degraded on AutoApproverOverrideConflict.
	clearCertManagerControllerOverrides(ctx)

	_ = approverPolicyClient().Delete(ctx, "cluster", metav1.DeleteOptions{})
	Eventually(func() bool {
		_, err := approverPolicyClient().Get(ctx, "cluster", metav1.GetOptions{})
		return apierrors.IsNotFound(err)
	}, highTimeout, fastPollInterval).Should(BeTrue())
	Eventually(func(g Gomega) { verifyAutoApproverState(ctx, g, false) }, highTimeout, fastPollInterval).Should(Succeed())
}

func ensureApproverPolicyManagerFeatureGateEnabled(ctx context.Context) {
	current, err := getSubscriptionEnvVar(ctx, loader, "UNSUPPORTED_ADDON_FEATURES")
	Expect(err).NotTo(HaveOccurred())
	for _, gate := range strings.Split(current, ",") {
		if strings.TrimSpace(gate) == approverPolicyManagerFeatureGate+"=false" {
			setApproverPolicyManagerFeatureGate(ctx, true)
			return
		}
	}
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

// ---------------------------------------------------------------------------
// Auto-approver handoff certificate helpers
// ---------------------------------------------------------------------------

func createSelfSignedClusterIssuer(ctx context.Context, name string) {
	_, err := certmanagerClient.CertmanagerV1().ClusterIssuers().Create(ctx, &cmv1.ClusterIssuer{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: cmv1.IssuerSpec{
			IssuerConfig: cmv1.IssuerConfig{SelfSigned: &cmv1.SelfSignedIssuer{}},
		},
	}, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() {
		_ = certmanagerClient.CertmanagerV1().ClusterIssuers().Delete(context.Background(), name, metav1.DeleteOptions{})
	})
	Expect(waitForClusterIssuerReadiness(ctx, name)).To(Succeed())
}

func createDNSCertificate(ctx context.Context, namespace, name, secretName, issuerName, dnsName string) {
	_, err := certmanagerClient.CertmanagerV1().Certificates(namespace).Create(ctx, &cmv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: cmv1.CertificateSpec{
			SecretName: secretName,
			DNSNames:   []string{dnsName},
			IssuerRef:  cmmeta.IssuerReference{Name: issuerName, Kind: "ClusterIssuer", Group: "cert-manager.io"},
		},
	}, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())
}

func waitForCertificateOwnedRequest(ctx context.Context, namespace, certificateName string) *cmv1.CertificateRequest {
	var request *cmv1.CertificateRequest
	Eventually(func(g Gomega) {
		list, err := certmanagerClient.CertmanagerV1().CertificateRequests(namespace).List(ctx, metav1.ListOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		for i := range list.Items {
			if list.Items[i].Annotations["cert-manager.io/certificate-name"] == certificateName {
				request = &list.Items[i]
				return
			}
		}
		g.Expect(request).NotTo(BeNil(), "expected a CertificateRequest owned by Certificate %s", certificateName)
	}, highTimeout, fastPollInterval).Should(Succeed())
	return request
}

func certificateRequestHasDecision(cr *cmv1.CertificateRequest) bool {
	for _, condition := range cr.Status.Conditions {
		if condition.Status != cmmeta.ConditionTrue {
			continue
		}
		if condition.Type == cmv1.CertificateRequestConditionApproved || condition.Type == cmv1.CertificateRequestConditionDenied {
			return true
		}
	}
	return false
}

func createAllowingCertificateRequestPolicy(ctx context.Context, client crclient.Client, name string) {
	policy := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "policy.cert-manager.io/v1alpha1",
		"kind":       "CertificateRequestPolicy",
		"metadata":   map[string]interface{}{"name": name},
		"spec": map[string]interface{}{
			"allowed": map[string]interface{}{
				"dnsNames":   map[string]interface{}{"values": []interface{}{"*"}},
				"commonName": map[string]interface{}{"value": "*"},
				"usages": []interface{}{
					"digital signature",
					"key encipherment",
					"server auth",
					"client auth",
					"signing",
				},
			},
			"selector": map[string]interface{}{
				"issuerRef": map[string]interface{}{},
			},
		},
	}}
	Expect(client.Create(ctx, policy)).To(Succeed())
	DeferCleanup(func() {
		_ = client.Delete(context.Background(), policy)
	})
	Eventually(func(g Gomega) {
		current := &unstructured.Unstructured{}
		current.SetGroupVersionKind(policy.GroupVersionKind())
		g.Expect(client.Get(ctx, crclient.ObjectKey{Name: name}, current)).To(Succeed())
		conditions, found, err := unstructured.NestedSlice(current.Object, "status", "conditions")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(found).To(BeTrue())
		ready := false
		for _, raw := range conditions {
			condition, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			if condition["type"] == "Ready" && condition["status"] == "True" {
				ready = true
				break
			}
		}
		g.Expect(ready).To(BeTrue())
	}, highTimeout, fastPollInterval).Should(Succeed())
}

// createCertificateRequestPolicyUseRBAC grants "use" on the named policy to authenticated
// identities so Certificate-owned CertificateRequests (username = Certificate creator) can
// be evaluated by approver-policy.
func createCertificateRequestPolicyUseRBAC(ctx context.Context, policyName string) {
	roleName := policyName + "-use"
	role := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: roleName},
		Rules: []rbacv1.PolicyRule{{
			APIGroups:     []string{"policy.cert-manager.io"},
			Resources:     []string{"certificaterequestpolicies"},
			ResourceNames: []string{policyName},
			Verbs:         []string{"use"},
		}},
	}
	binding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: roleName},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: roleName},
		Subjects:   []rbacv1.Subject{{Kind: "Group", APIGroup: rbacv1.GroupName, Name: "system:authenticated"}},
	}
	_, err := k8sClientSet.RbacV1().ClusterRoles().Create(ctx, role, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())
	_, err = k8sClientSet.RbacV1().ClusterRoleBindings().Create(ctx, binding, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() {
		_ = k8sClientSet.RbacV1().ClusterRoleBindings().Delete(context.Background(), roleName, metav1.DeleteOptions{})
		_ = k8sClientSet.RbacV1().ClusterRoles().Delete(context.Background(), roleName, metav1.DeleteOptions{})
	})
}

// ---------------------------------------------------------------------------
// Auto-approver handoff edge-case helpers
// ---------------------------------------------------------------------------

const (
	autoApproverRBACName             = "cert-manager-controller-approve:cert-manager-io"
	certificateRequestApproverName   = "certificaterequests-approver"
	autoApproverOverrideConflictText = "AutoApproverOverrideConflict"
	approverPolicyManagerFeatureGate = "ApproverPolicyManager"
)

// controllersSelectionEnablesAutoApprover mirrors the operator's CSV-aware
// --controllers parsing (pflag StringSlice / cert-manager): exclusions win, and
// a list of only exclusions implicitly enables the default set.
func controllersSelectionEnablesAutoApprover(selection string) bool {
	controllers := []string{"*"}
	if selection != "" {
		if parsed, err := csv.NewReader(strings.NewReader(selection)).Read(); err == nil {
			controllers = parsed
		}
	}
	if slices.Contains(controllers, "-"+certificateRequestApproverName) {
		return false
	}
	if slices.Contains(controllers, "*") || slices.Contains(controllers, certificateRequestApproverName) {
		return true
	}
	for _, controller := range controllers {
		if !strings.HasPrefix(controller, "-") {
			return false
		}
	}
	return true
}

func waitForAutoApproverHandoff(ctx context.Context) {
	Eventually(func(g Gomega) { verifyAutoApproverState(ctx, g, true) }, highTimeout, fastPollInterval).Should(Succeed())
}

func setCertManagerControllerOverrideArgs(ctx context.Context, args []string) {
	Expect(addOverrideArgs(certmanageroperatorclient, certmanagerControllerDeployment, args)).To(Succeed())
}

func clearCertManagerControllerOverrides(ctx context.Context) {
	Expect(retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cm, err := certmanageroperatorclient.OperatorV1alpha1().CertManagers().Get(ctx, "cluster", metav1.GetOptions{})
		if err != nil {
			return err
		}
		updated := cm.DeepCopy()
		if updated.Spec.ControllerConfig != nil {
			updated.Spec.ControllerConfig.OverrideArgs = nil
		}
		updated.Spec.UnsupportedConfigOverrides = runtime.RawExtension{}
		_, err = certmanageroperatorclient.OperatorV1alpha1().CertManagers().Update(ctx, updated, metav1.UpdateOptions{})
		return err
	})).To(Succeed())
}

func verifyAutoApproverOverrideConflictReported(ctx context.Context, g Gomega, expected bool) {
	cm, err := certmanageroperatorclient.OperatorV1alpha1().CertManagers().Get(ctx, "cluster", metav1.GetOptions{})
	g.Expect(err).NotTo(HaveOccurred())
	found := false
	for _, condition := range cm.Status.Conditions {
		if condition.Status == opv1.ConditionTrue && strings.Contains(condition.Message, autoApproverOverrideConflictText) {
			found = true
			break
		}
	}
	g.Expect(found).To(Equal(expected))
}

func recreateBuiltinApproveRBAC(ctx context.Context) {
	role := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: autoApproverRBACName},
		Rules: []rbacv1.PolicyRule{{
			APIGroups:     []string{"cert-manager.io"},
			Resources:     []string{"signers"},
			ResourceNames: []string{"issuers.cert-manager.io/*", "clusterissuers.cert-manager.io/*"},
			Verbs:         []string{"approve"},
		}},
	}
	binding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: autoApproverRBACName},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: autoApproverRBACName},
		Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "cert-manager", Namespace: operandNamespace}},
	}
	_, err := k8sClientSet.RbacV1().ClusterRoles().Create(ctx, role, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())
	_, err = k8sClientSet.RbacV1().ClusterRoleBindings().Create(ctx, binding, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())
}

func stripCertManagerControllersArg(ctx context.Context) {
	Expect(retry.RetryOnConflict(retry.DefaultRetry, func() error {
		deployment, err := k8sClientSet.AppsV1().Deployments(operandNamespace).Get(ctx, certmanagerControllerDeployment, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if len(deployment.Spec.Template.Spec.Containers) == 0 {
			return fmt.Errorf("cert-manager Deployment has no containers")
		}
		args := deployment.Spec.Template.Spec.Containers[0].Args
		filtered := make([]string, 0, len(args))
		for _, arg := range args {
			if strings.HasPrefix(arg, "--controllers=") {
				continue
			}
			filtered = append(filtered, arg)
		}
		deployment.Spec.Template.Spec.Containers[0].Args = filtered
		_, err = k8sClientSet.AppsV1().Deployments(operandNamespace).Update(ctx, deployment, metav1.UpdateOptions{})
		return err
	})).To(Succeed())
}

func verifyCertManagerControllerArg(ctx context.Context, g Gomega, arg string) {
	deployment, err := k8sClientSet.AppsV1().Deployments(operandNamespace).Get(ctx, certmanagerControllerDeployment, metav1.GetOptions{})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(deployment.Spec.Template.Spec.Containers).NotTo(BeEmpty())
	g.Expect(deployment.Spec.Template.Spec.Containers[0].Args).To(ContainElement(arg))
}

// setApproverPolicyManagerFeatureGate updates only ApproverPolicyManager in the live
// Subscription env, preserving other gates. Callers that disable the gate mid-test must
// re-enable it before AfterEach deletes ApproverPolicyManager (finalizer needs the controller).
func setApproverPolicyManagerFeatureGate(ctx context.Context, enabled bool) {
	current, err := getSubscriptionEnvVar(ctx, loader, "UNSUPPORTED_ADDON_FEATURES")
	Expect(err).NotTo(HaveOccurred())
	var gates []string
	for _, gate := range strings.Split(current, ",") {
		if gate = strings.TrimSpace(gate); gate != "" && !strings.HasPrefix(gate, approverPolicyManagerFeatureGate+"=") {
			gates = append(gates, gate)
		}
	}
	gates = append(gates, fmt.Sprintf("%s=%t", approverPolicyManagerFeatureGate, enabled))
	value := strings.Join(gates, ",")
	By(fmt.Sprintf("setting %s=%t on the operator Subscription", approverPolicyManagerFeatureGate, enabled))
	Expect(patchSubscriptionWithEnvVars(ctx, loader, map[string]string{"UNSUPPORTED_ADDON_FEATURES": value})).To(Succeed())
	Expect(waitForDeploymentEnvVarAndRollout(ctx, operatorNamespace, operatorDeploymentName, "UNSUPPORTED_ADDON_FEATURES", value, highTimeout)).To(Succeed())
}
