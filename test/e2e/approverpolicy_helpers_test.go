//go:build e2e

package e2e

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	opv1 "github.com/openshift/api/operator/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"

	policyv1alpha1 "github.com/cert-manager/approver-policy/pkg/apis/policy/v1alpha1"
	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	cmclient "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	operatorclientv1alpha1 "github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned/typed/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/test/library"
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

// verifyAutoApproverState checks that CertManager status, controller arguments, and
// approval RBAC agree on whether the built-in approver is disabled.
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
func approverPolicyManagerBeforeAll(
	ctx context.Context,
	originalGates, originalOperatorLogLevel *string,
	enabled bool,
) crclient.Client {
	By("verifying no pre-existing ApproverPolicyManager singleton or operand resources")
	_, err := approverPolicyClient().Get(ctx, "cluster", metav1.GetOptions{})
	Expect(apierrors.IsNotFound(err)).To(BeTrue(), "suite requires an absent singleton; never replace an existing user's instance")

	Expect(policyv1alpha1.AddToScheme(clientscheme.Scheme)).To(Succeed())
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

// cleanupApproverPolicyManager releases any test-only hold finalizer, removes
// conflicting controller overrides, deletes the ApproverPolicyManager CR, and
// waits for built-in approval to return. It can run after a test or an entire suite.
func cleanupApproverPolicyManager(ctx context.Context, client crclient.Client) {
	releaseApproverPolicyTLSSecretHold(ctx, client)

	// Keep the controller enabled for finalizer-gated deletion, including when a
	// failed feature-gate test did not re-enable it.
	ensureApproverPolicyManagerFeatureGateEnabled(ctx)
	// Drop controller overrides left by a failed conflict/compatible override scenario so
	// the next Spec is not stuck Degraded on AutoApproverOverrideConflict. Non-controller
	// UnsupportedConfigOverrides (webhook/CA-injector) are preserved.
	clearCertManagerControllerOverrides(ctx)

	_ = approverPolicyClient().Delete(ctx, "cluster", metav1.DeleteOptions{})
	Eventually(func() bool {
		_, err := approverPolicyClient().Get(ctx, "cluster", metav1.GetOptions{})
		return apierrors.IsNotFound(err)
	}, highTimeout, fastPollInterval).Should(BeTrue())
	Eventually(func(g Gomega) {
		verifyAutoApproverState(ctx, g, false)
	}, highTimeout, fastPollInterval).Should(Succeed())
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
// findSignerRule returns the first RBAC rule whose only resource is signers.
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

// createSelfSignedClusterIssuer creates a self-signed issuer, registers its
// deletion with Ginkgo, and waits for it to become ready for issuance.
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

// createDNSCertificate submits a Certificate for one DNS name using a ClusterIssuer.
// Callers check its approval or issuance outcome and clean up its namespace.
func createDNSCertificate(ctx context.Context, namespace, name, secretName, issuerName, dnsName string) {
	_, err := certmanagerClient.CertmanagerV1().Certificates(namespace).Create(ctx, &cmv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: cmv1.CertificateSpec{
			SecretName: secretName,
			DNSNames:   []string{dnsName},
			IssuerRef: cmmeta.IssuerReference{
				Name:  issuerName,
				Kind:  "ClusterIssuer",
				Group: "cert-manager.io",
			},
		},
	}, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())
}

// waitForCertificateOwnedRequest finds a request by cert-manager's certificate-name
// annotation. Pass an empty previous UID for initial issuance, or the previous
// request's UID to wait for a different request during reissuance.
func waitForCertificateOwnedRequest(
	ctx context.Context,
	namespace, certificateName string,
	previous types.UID,
) *cmv1.CertificateRequest {
	var request *cmv1.CertificateRequest
	Eventually(func(g Gomega) {
		list, err := certmanagerClient.CertmanagerV1().CertificateRequests(namespace).List(ctx, metav1.ListOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		for i := range list.Items {
			if list.Items[i].UID != previous && list.Items[i].Annotations["cert-manager.io/certificate-name"] == certificateName {
				request = &list.Items[i]
				return
			}
		}
		g.Expect(request).NotTo(BeNil(), "expected a CertificateRequest owned by Certificate %s", certificateName)
	}, highTimeout, fastPollInterval).Should(Succeed())
	return request
}

// certificateRequestHasDecision reports whether Approved or Denied is True.
// Other conditions, including Ready, do not indicate an approval decision.
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

// createAllowingCertificateRequestPolicy creates a policy for the given namespace,
// issuer, and DNS name, waits for readiness, and registers cleanup.
func createAllowingCertificateRequestPolicy(
	ctx context.Context,
	client crclient.Client,
	name, namespace, issuerName, dnsName string,
) {
	policy := newCertificateRequestPolicy(namespace, cmmeta.IssuerReference{
		Name:  issuerName,
		Kind:  "ClusterIssuer",
		Group: "cert-manager.io",
	})
	policy.Name = name
	policy.Spec.Allowed.DNSNames.Values = ptr.To([]string{dnsName})
	policy.Spec.Allowed.Usages = ptr.To([]cmv1.KeyUsage{
		cmv1.UsageDigitalSignature,
		cmv1.UsageKeyEncipherment,
		cmv1.UsageServerAuth,
		cmv1.UsageClientAuth,
		cmv1.UsageSigning,
	})

	createCertificateRequestPolicy(ctx, client, policy)
}

// createCertificateRequestPolicyUseRBAC grants "use" on the named policy only to
// cert-manager's ServiceAccount, which creates CertificateRequests for Certificates.
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
		Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "cert-manager", Namespace: operandNamespace}},
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

// waitForAutoApproverHandoff waits for status, controller arguments, and RBAC to
// confirm that cert-manager's built-in approver has been disabled.
func waitForAutoApproverHandoff(ctx context.Context) {
	Eventually(func(g Gomega) {
		verifyAutoApproverState(ctx, g, true)
	}, highTimeout, fastPollInterval).Should(Succeed())
}

// setCertManagerControllerUnsupportedArgs sets break-glass controller args via
// UnsupportedConfigOverrides. --controllers is intentionally not allowlisted on
// ControllerConfig.OverrideArgs, so conflict/compatible handoff tests must use this path.
// Webhook and CA-injector overrides already present on the CR are preserved.
func setCertManagerControllerUnsupportedArgs(ctx context.Context, args []string) {
	Expect(retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cm, err := certmanageroperatorclient.OperatorV1alpha1().CertManagers().Get(ctx, "cluster", metav1.GetOptions{})
		if err != nil {
			return err
		}
		updated := cm.DeepCopy()
		overrides, err := parseUnsupportedConfigOverrides(updated.Spec.UnsupportedConfigOverrides)
		if err != nil {
			return err
		}
		overrides.Controller.Args = args
		if err := applyUnsupportedConfigOverrides(updated, overrides); err != nil {
			return err
		}
		_, err = certmanageroperatorclient.OperatorV1alpha1().CertManagers().Update(ctx, updated, metav1.UpdateOptions{})
		return err
	})).To(Succeed())
}

// clearCertManagerControllerOverrides removes only controller OverrideArgs and
// unsupported controller args installed by these tests. Webhook and CA-injector
// UnsupportedConfigOverrides are left untouched.
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
		overrides, err := parseUnsupportedConfigOverrides(updated.Spec.UnsupportedConfigOverrides)
		if err != nil {
			return err
		}
		overrides.Controller = v1alpha1.UnsupportedConfigOverridesForCertManagerController{}
		if err := applyUnsupportedConfigOverrides(updated, overrides); err != nil {
			return err
		}
		_, err = certmanageroperatorclient.OperatorV1alpha1().CertManagers().Update(ctx, updated, metav1.UpdateOptions{})
		return err
	})).To(Succeed())
}

func parseUnsupportedConfigOverrides(raw runtime.RawExtension) (*v1alpha1.UnsupportedConfigOverrides, error) {
	overrides := &v1alpha1.UnsupportedConfigOverrides{}
	if len(raw.Raw) == 0 {
		return overrides, nil
	}
	if err := json.Unmarshal(raw.Raw, overrides); err != nil {
		return nil, err
	}
	return overrides, nil
}

func applyUnsupportedConfigOverrides(cm *v1alpha1.CertManager, overrides *v1alpha1.UnsupportedConfigOverrides) error {
	if overrides == nil || (len(overrides.Controller.Args) == 0 && len(overrides.Webhook.Args) == 0 && len(overrides.CAInjector.Args) == 0) {
		cm.Spec.UnsupportedConfigOverrides = runtime.RawExtension{}
		return nil
	}
	raw, err := json.Marshal(overrides)
	if err != nil {
		return err
	}
	cm.Spec.UnsupportedConfigOverrides = runtime.RawExtension{Raw: raw}
	return nil
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

// ---------------------------------------------------------------------------
// Policy evaluation and user request helpers
// ---------------------------------------------------------------------------

// operandPolicyObservationTime bounds checks that a request stays pending or
// retains its terminal decision after a policy or RBAC change.
const operandPolicyObservationTime = 10 * time.Second

// newOperandCertificate returns a one-hour server Certificate with explicit key
// settings and usages. Callers can adjust its spec before submitting it.
func newOperandCertificate(namespace, name string, issuer cmmeta.IssuerReference) *cmv1.Certificate {
	return &cmv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: cmv1.CertificateSpec{
			SecretName: name + "-tls",
			DNSNames:   []string{"workload.policy.example.com"},
			IssuerRef:  issuer,
			Duration:   &metav1.Duration{Duration: time.Hour},
			PrivateKey: &cmv1.CertificatePrivateKey{
				Algorithm: cmv1.RSAKeyAlgorithm,
				Size:      2048,
			},
			Usages: []cmv1.KeyUsage{
				cmv1.UsageDigitalSignature,
				cmv1.UsageKeyEncipherment,
				cmv1.UsageServerAuth,
			},
		},
	}
}

// createOperandCertificate submits a Certificate and waits for cert-manager to
// create its request. Namespace cleanup removes the Certificate and its Secrets.
func createOperandCertificate(ctx context.Context, certificate *cmv1.Certificate) *cmv1.CertificateRequest {
	_, err := certmanagerClient.CertmanagerV1().Certificates(certificate.Namespace).Create(
		ctx, certificate, metav1.CreateOptions{},
	)
	Expect(err).NotTo(HaveOccurred())
	return waitForCertificateOwnedRequest(ctx, certificate.Namespace, certificate.Name, "")
}

// waitForOperandCertificateIssued checks approval of the original request,
// Certificate readiness, and a usable TLS Secret, returning the issued certificate.
func waitForOperandCertificateIssued(
	ctx context.Context,
	certificate *cmv1.Certificate,
	request *cmv1.CertificateRequest,
) *x509.Certificate {
	waitForApproverPolicyDecision(ctx, request, cmv1.CertificateRequestConditionApproved)
	Expect(waitForCertificateReadiness(ctx, certificate.Name, certificate.Namespace)).To(Succeed())
	return waitForOperandSecret(ctx, certificate.Namespace, certificate.Spec.SecretName, certificate.Spec.DNSNames[0], nil)
}

// expectOperandCertificatePending checks that issuance remains undecided and no
// TLS Secret exists. Callers then remove the blocker and verify actual issuance.
func expectOperandCertificatePending(ctx context.Context, certificate *cmv1.Certificate, request *cmv1.CertificateRequest) {
	expectOperandRequestPending(ctx, request)
	_, err := k8sClientSet.CoreV1().Secrets(certificate.Namespace).Get(ctx, certificate.Spec.SecretName, metav1.GetOptions{})
	Expect(apierrors.IsNotFound(err)).To(BeTrue(), "pending Certificate must not have a TLS Secret: %v", err)
}

// expectOperandCertificateDenied checks that the request is denied and stays
// unsigned, with no TLS Secret during the observation window. It does not assume
// that cert-manager will never retry issuance after its failure backoff expires.
func expectOperandCertificateDenied(ctx context.Context, certificate *cmv1.Certificate, request *cmv1.CertificateRequest) {
	waitForApproverPolicyDecision(ctx, request, cmv1.CertificateRequestConditionDenied)
	Consistently(func(g Gomega) {
		verifyApproverPolicyDecision(ctx, g, request, cmv1.CertificateRequestConditionDenied)
		_, err := k8sClientSet.CoreV1().Secrets(certificate.Namespace).Get(ctx, certificate.Spec.SecretName, metav1.GetOptions{})
		g.Expect(apierrors.IsNotFound(err)).To(BeTrue(), "denied Certificate must not have a TLS Secret: %v", err)
	}, operandPolicyObservationTime, fastPollInterval).Should(Succeed())
}

// operandPolicyRequest holds a CSR template and its CertificateRequest fields.
// createOperandRequest generates the PEM CSR and populates Spec.Request on submission.
type operandPolicyRequest struct {
	CSR  x509.CertificateRequest
	Spec cmv1.CertificateRequestSpec
}

// newOperandPolicyRequest returns default CSR fields and a request for a one-hour
// TLS certificate. Direct-request cases only check approval, so they do not need
// a running issuer; Certificate workflows use newOperandCertificate instead.
func newOperandPolicyRequest(issuer cmmeta.IssuerReference) operandPolicyRequest {
	return operandPolicyRequest{
		CSR: x509.CertificateRequest{DNSNames: []string{"workload.policy.example.com"}},
		Spec: cmv1.CertificateRequestSpec{
			IssuerRef: issuer,
			Duration:  &metav1.Duration{Duration: time.Hour},
			Usages:    []cmv1.KeyUsage{cmv1.UsageDigitalSignature, cmv1.UsageKeyEncipherment},
		},
	}
}

// newCertificateRequestPolicy returns a policy allowing the default request's DNS
// names and key usages, scoped to the test namespace and exact issuer reference.
func newCertificateRequestPolicy(namespace string, issuer cmmeta.IssuerReference) *policyv1alpha1.CertificateRequestPolicy {
	return &policyv1alpha1.CertificateRequestPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: namespace + "-policy"},
		Spec: policyv1alpha1.CertificateRequestPolicySpec{
			Allowed: &policyv1alpha1.CertificateRequestPolicyAllowed{
				DNSNames: &policyv1alpha1.CertificateRequestPolicyAllowedStringSlice{
					Values:   ptr.To([]string{"*.policy.example.com"}),
					Required: ptr.To(true),
				},
				Usages: ptr.To([]cmv1.KeyUsage{
					cmv1.UsageDigitalSignature,
					cmv1.UsageKeyEncipherment,
					cmv1.UsageServerAuth,
				}),
			},
			Selector: policyv1alpha1.CertificateRequestPolicySelector{
				IssuerRef: &policyv1alpha1.CertificateRequestPolicySelectorIssuerRef{
					Name:  ptr.To(issuer.Name),
					Kind:  ptr.To(issuer.Kind),
					Group: ptr.To(issuer.Group),
				},
				Namespace: &policyv1alpha1.CertificateRequestPolicySelectorNamespace{
					MatchNames: []string{namespace},
				},
			},
		},
	}
}

// createCertificateRequestPolicy creates the policy, registers its deletion with
// Ginkgo, and waits for the operand to report it ready at the current generation.
func createCertificateRequestPolicy(
	ctx context.Context,
	client crclient.Client,
	policy *policyv1alpha1.CertificateRequestPolicy,
) {
	Expect(client.Create(ctx, policy)).To(Succeed())
	DeferCleanup(func() {
		Expect(crclient.IgnoreNotFound(client.Delete(ctx, policy))).To(Succeed())
	})

	waitForCertificateRequestPolicyReady(ctx, client, policy)
}

// waitForCertificateRequestPolicyReady refreshes policy from the API until Ready
// is True for its current generation, so a previous spec's readiness cannot pass.
func waitForCertificateRequestPolicyReady(
	ctx context.Context,
	client crclient.Client,
	policy *policyv1alpha1.CertificateRequestPolicy,
) {
	Eventually(func(g Gomega) {
		g.Expect(client.Get(ctx, crclient.ObjectKeyFromObject(policy), policy)).To(Succeed())
		for _, condition := range policy.Status.Conditions {
			if condition.Type == policyv1alpha1.ConditionTypeReady {
				g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(condition.ObservedGeneration).To(Equal(policy.Generation))
				return
			}
		}
		g.Expect(false).To(BeTrue(), "policy %s has no Ready condition", policy.Name)
	}, highTimeout, fastPollInterval).Should(Succeed())
}

// updateCertificateRequestPolicy refetches the policy, applies mutate, and waits
// for the updated spec to become ready. Conflicts can cause mutate to run again.
func updateCertificateRequestPolicy(
	ctx context.Context,
	client crclient.Client,
	policy *policyv1alpha1.CertificateRequestPolicy,
	mutate func(*policyv1alpha1.CertificateRequestPolicy),
) {
	Expect(retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := client.Get(ctx, crclient.ObjectKeyFromObject(policy), policy); err != nil {
			return err
		}
		mutate(policy)
		return client.Update(ctx, policy)
	})).To(Succeed())

	waitForCertificateRequestPolicyReady(ctx, client, policy)
}

// updateApproverPolicySignerNames updates the operand's allowed signers and waits
// for readiness. Tests verify the change through request approval; revocation
// tests also wait for authorization caches before submitting a new request.
func updateApproverPolicySignerNames(ctx context.Context, names []string) {
	updateApproverPolicyManager(ctx, func(current *v1alpha1.ApproverPolicyManager) {
		current.Spec.ApproverPolicyConfig.ApproveSignerNames = names
	})
	waitForApproverPolicyReady(ctx)
}

// newOperandRequester creates a ServiceAccount with request-creation permission
// in its test namespace and returns a client impersonating it. Policy-use and
// approval permissions are not granted; namespace cleanup removes its resources.
func newOperandRequester(ctx context.Context, namespace string) cmclient.Interface {
	_, err := k8sClientSet.CoreV1().ServiceAccounts(namespace).Create(ctx, &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: "requester"},
	}, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())

	_, err = k8sClientSet.RbacV1().Roles(namespace).Create(ctx, &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: "submit-requests"},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"cert-manager.io"},
				Resources: []string{"certificaterequests"},
				Verbs:     []string{"create"},
			},
		},
	}, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())

	_, err = k8sClientSet.RbacV1().RoleBindings(namespace).Create(ctx, &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "submit-requests"},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     "submit-requests",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "requester",
				Namespace: namespace,
			},
		},
	}, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())

	config := rest.CopyConfig(cfg)
	config.Impersonate = rest.ImpersonationConfig{
		UserName: "system:serviceaccount:" + namespace + ":requester",
		Groups: []string{
			"system:serviceaccounts",
			"system:serviceaccounts:" + namespace,
			"system:authenticated",
		},
	}
	waitForOperandAccess(
		ctx,
		rbacv1.Subject{
			Kind:      "ServiceAccount",
			Name:      "requester",
			Namespace: namespace,
		},
		authorizationv1.ResourceAttributes{
			Namespace: namespace,
			Group:     "cert-manager.io",
			Resource:  "certificaterequests",
			Verb:      "create",
		},
		true,
	)

	client, err := cmclient.NewForConfig(config)
	Expect(err).NotTo(HaveOccurred())
	return client
}

// grantOperandPolicyUse authorizes only the named subjects to use one policy for
// requests in this namespace. The namespace cleanup also removes this RBAC.
func grantOperandPolicyUse(ctx context.Context, namespace, policyName string, subjects ...rbacv1.Subject) {
	// Policies are cluster-scoped, but approver-policy checks "use" in the request's
	// namespace. A Role and RoleBinding therefore keep this grant local to the test.
	_, err := k8sClientSet.RbacV1().Roles(namespace).Create(ctx, &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: policyName},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups:     []string{"policy.cert-manager.io"},
				Resources:     []string{"certificaterequestpolicies"},
				ResourceNames: []string{policyName},
				Verbs:         []string{"use"},
			},
		},
	}, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())

	_, err = k8sClientSet.RbacV1().RoleBindings(namespace).Create(ctx, &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: policyName},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     policyName,
		},
		Subjects: subjects,
	}, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())

	for _, subject := range subjects {
		waitForOperandPolicyUse(ctx, namespace, policyName, subject, true)
	}
}

// waitForOperandPolicyUse waits for RBAC propagation before submitting a request
// whose expected decision depends on a grant or revocation.
func waitForOperandPolicyUse(ctx context.Context, namespace, policyName string, subject rbacv1.Subject, allowed bool) {
	waitForOperandAccess(ctx, subject, authorizationv1.ResourceAttributes{
		Namespace: namespace,
		Group:     "policy.cert-manager.io",
		Resource:  "certificaterequestpolicies",
		Name:      policyName,
		Verb:      "use",
	}, allowed)
}

// waitForOperandAccess polls SubjectAccessReviews until a ServiceAccount has the
// expected access. subject must identify a ServiceAccount; its standard groups
// are included to match the identity used by the authorization checks.
func waitForOperandAccess(
	ctx context.Context,
	subject rbacv1.Subject,
	attributes authorizationv1.ResourceAttributes,
	allowed bool,
) {
	check := func(g Gomega) {
		review, err := k8sClientSet.AuthorizationV1().SubjectAccessReviews().Create(
			ctx,
			&authorizationv1.SubjectAccessReview{
				Spec: authorizationv1.SubjectAccessReviewSpec{
					User: "system:serviceaccount:" + subject.Namespace + ":" + subject.Name,
					Groups: []string{
						"system:serviceaccounts",
						"system:serviceaccounts:" + subject.Namespace,
						"system:authenticated",
					},
					ResourceAttributes: &attributes,
				},
			},
			metav1.CreateOptions{},
		)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(review.Status.Allowed).To(
			Equal(allowed),
			"authorization for %s/%s: %+v",
			subject.Namespace, subject.Name, attributes,
		)
	}

	Eventually(check, lowTimeout, fastPollInterval).Should(Succeed())
	if !allowed {
		// cert-manager's approval webhook caches authorization for one second.
		// Observe stable revocation beyond that window before creating a request.
		Consistently(check, 2*time.Second, fastPollInterval).Should(Succeed())
	}
}

// createOperandRequest generates a CSR with a 2048-bit RSA key and submits it
// through the workload client. It verifies the requesting ServiceAccount identity
// populated by cert-manager's webhook. Namespace cleanup removes the request.
func createOperandRequest(
	ctx context.Context,
	requester cmclient.Interface,
	namespace, name string,
	request operandPolicyRequest,
) *cmv1.CertificateRequest {
	csr, err := library.GenerateCSR(&request.CSR)
	Expect(err).NotTo(HaveOccurred())

	// GenerateCSR escapes PEM newlines; the typed API requires the original PEM bytes.
	request.Spec.Request = []byte(strings.ReplaceAll(csr, "\\n", "\n"))
	created, err := requester.CertmanagerV1().CertificateRequests(namespace).Create(ctx, &cmv1.CertificateRequest{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       request.Spec,
	}, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())
	Expect(created.Spec.Username).To(Equal("system:serviceaccount:" + namespace + ":requester"))
	return created
}

// verifyApproverPolicyDecision checks the original request's terminal decision
// without depending on condition message formatting. A denied request must not
// contain an issued certificate.
func verifyApproverPolicyDecision(
	ctx context.Context,
	g Gomega,
	request *cmv1.CertificateRequest,
	decision cmv1.CertificateRequestConditionType,
) {
	current, err := certmanagerClient.CertmanagerV1().CertificateRequests(request.Namespace).Get(
		ctx, request.Name, metav1.GetOptions{},
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(current.UID).To(Equal(request.UID))

	var found bool
	for _, condition := range current.Status.Conditions {
		if condition.Status != cmmeta.ConditionTrue {
			continue
		}
		if condition.Type != cmv1.CertificateRequestConditionApproved && condition.Type != cmv1.CertificateRequestConditionDenied {
			continue
		}

		g.Expect(condition.Type).To(Equal(decision), "unexpected decision: %+v", condition)
		g.Expect(condition.Reason).To(Equal("policy.cert-manager.io"))
		found = true
	}

	g.Expect(found).To(BeTrue(), "request has no %s decision: %+v", decision, current.Status.Conditions)
	if decision == cmv1.CertificateRequestConditionDenied {
		g.Expect(current.Status.Certificate).To(BeEmpty())
	}
}

// waitForApproverPolicyDecision waits for an actual approval/denial by approver-policy.
func waitForApproverPolicyDecision(
	ctx context.Context,
	request *cmv1.CertificateRequest,
	decision cmv1.CertificateRequestConditionType,
) {
	Eventually(func(g Gomega) {
		verifyApproverPolicyDecision(ctx, g, request, decision)
	}, highTimeout, fastPollInterval).Should(Succeed())
}

// expectOperandRequestPending observes an undecided, unsigned request for a bounded
// period. Tests subsequently unblock it to prove the operand is processing requests.
func expectOperandRequestPending(ctx context.Context, request *cmv1.CertificateRequest) {
	Consistently(func(g Gomega) {
		current, err := certmanagerClient.CertmanagerV1().CertificateRequests(request.Namespace).Get(
			ctx, request.Name, metav1.GetOptions{},
		)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(current.UID).To(Equal(request.UID))
		g.Expect(certificateRequestHasDecision(current)).To(BeFalse(), "unexpected decision: %+v", current.Status.Conditions)
		g.Expect(current.Status.Certificate).To(BeEmpty())
	}, operandPolicyObservationTime, fastPollInterval).Should(Succeed())
}

// createOperandExternalIssuerAPI registers a unique namespaced issuer CRD, waits
// for it to be established, and returns an issuer reference for approval tests.
// The webhook uses API discovery to determine signer names and scope. Ginkgo
// deletes the CRD afterward; no issuer instance or signing controller is created.
func createOperandExternalIssuerAPI(ctx context.Context, client crclient.Client, namespace string) cmmeta.IssuerReference {
	group := namespace + ".example.com"
	crd := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: "externalissuers." + group},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: group,
			Scope: apiextensionsv1.NamespaceScoped,
			Names: apiextensionsv1.CustomResourceDefinitionNames{
				Plural:   "externalissuers",
				Singular: "externalissuer",
				Kind:     "ExternalIssuer",
			},
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Name:    "v1alpha1",
					Served:  true,
					Storage: true,
					Schema: &apiextensionsv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
							Type: "object",
						},
					},
				},
			},
		},
	}
	Expect(client.Create(ctx, crd)).To(Succeed())
	DeferCleanup(func() {
		Expect(crclient.IgnoreNotFound(client.Delete(ctx, crd))).To(Succeed())
	})

	Eventually(func(g Gomega) {
		g.Expect(client.Get(ctx, crclient.ObjectKeyFromObject(crd), crd)).To(Succeed())
		for _, condition := range crd.Status.Conditions {
			if condition.Type == apiextensionsv1.Established {
				g.Expect(condition.Status).To(Equal(apiextensionsv1.ConditionTrue))
				return
			}
		}
		g.Expect(false).To(BeTrue(), "external issuer CRD is not established")
	}, highTimeout, fastPollInterval).Should(Succeed())
	return cmmeta.IssuerReference{Name: "allowed", Kind: "ExternalIssuer", Group: group}
}

// waitForOperandSecret waits for a TLS Secret with a matching private key and an
// unexpired certificate valid for dnsName, then returns that certificate. For reissuance,
// pass the previous leaf certificate's DER bytes to require a different certificate.
func waitForOperandSecret(ctx context.Context, namespace, name, dnsName string, previous []byte) *x509.Certificate {
	var certificate *x509.Certificate
	Eventually(func(g Gomega) {
		secret, err := k8sClientSet.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		_, err = tls.X509KeyPair(secret.Data[corev1.TLSCertKey], secret.Data[corev1.TLSPrivateKeyKey])
		g.Expect(err).NotTo(HaveOccurred())
		certificate, err = library.GetX509Certificate(secret)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(certificate.VerifyHostname(dnsName)).To(Succeed())
		g.Expect(certificate.NotAfter.After(time.Now())).To(BeTrue(), "issued certificate must not be expired")
		if len(previous) > 0 {
			g.Expect(certificate.Raw).NotTo(Equal(previous))
		}
	}, highTimeout, fastPollInterval).Should(Succeed())
	return certificate
}

// deleteOperandRequest waits for removal before reusing the user-chosen name.
func deleteOperandRequest(ctx context.Context, request *cmv1.CertificateRequest) {
	Expect(certmanagerClient.CertmanagerV1().CertificateRequests(request.Namespace).Delete(
		ctx, request.Name, metav1.DeleteOptions{},
	)).To(Succeed())

	Eventually(func() error {
		_, err := certmanagerClient.CertmanagerV1().CertificateRequests(request.Namespace).Get(
			ctx, request.Name, metav1.GetOptions{},
		)
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("request %s still exists or cannot be read: %v", request.Name, err)
	}, highTimeout, fastPollInterval).Should(Succeed())
}
