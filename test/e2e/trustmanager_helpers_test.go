//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"encoding/pem"
	"strings"
	"time"

	policyv1alpha1 "github.com/cert-manager/approver-policy/pkg/apis/policy/v1alpha1"
	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	trustapi "github.com/cert-manager/trust-manager/pkg/apis/trust/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	operatorclientv1alpha1 "github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned/typed/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/test/library"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// ---------------------------------------------------------------------------
// TrustManager CR builder
// ---------------------------------------------------------------------------

type trustManagerCRBuilder struct {
	tm *v1alpha1.TrustManager
}

func newTrustManagerCR() *trustManagerCRBuilder {
	return &trustManagerCRBuilder{
		tm: &v1alpha1.TrustManager{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec: v1alpha1.TrustManagerSpec{
				TrustManagerConfig: v1alpha1.TrustManagerConfig{},
			},
		},
	}
}

func (b *trustManagerCRBuilder) WithResources(resources corev1.ResourceRequirements) *trustManagerCRBuilder {
	b.tm.Spec.TrustManagerConfig.Resources = resources
	return b
}

func (b *trustManagerCRBuilder) WithTolerations(tolerations []corev1.Toleration) *trustManagerCRBuilder {
	b.tm.Spec.TrustManagerConfig.Tolerations = tolerations
	return b
}

func (b *trustManagerCRBuilder) WithNodeSelector(nodeSelector map[string]string) *trustManagerCRBuilder {
	b.tm.Spec.TrustManagerConfig.NodeSelector = nodeSelector
	return b
}

func (b *trustManagerCRBuilder) WithAffinity(affinity *corev1.Affinity) *trustManagerCRBuilder {
	b.tm.Spec.TrustManagerConfig.Affinity = affinity
	return b
}

func (b *trustManagerCRBuilder) WithLabels(labels map[string]string) *trustManagerCRBuilder {
	b.tm.Spec.ControllerConfig.Labels = labels
	return b
}

func (b *trustManagerCRBuilder) WithAnnotations(annotations map[string]string) *trustManagerCRBuilder {
	b.tm.Spec.ControllerConfig.Annotations = annotations
	return b
}

func (b *trustManagerCRBuilder) WithTrustNamespace(trustNamespace string) *trustManagerCRBuilder {
	b.tm.Spec.TrustManagerConfig.TrustNamespace = trustNamespace
	return b
}

func (b *trustManagerCRBuilder) WithSecretTargets(policy v1alpha1.SecretTargetsPolicy, authorizedSecrets []string) *trustManagerCRBuilder {
	b.tm.Spec.TrustManagerConfig.SecretTargets = v1alpha1.SecretTargetsConfig{
		Policy:            policy,
		AuthorizedSecrets: authorizedSecrets,
	}
	return b
}

func (b *trustManagerCRBuilder) WithDefaultCAPackage(policy v1alpha1.Mode) *trustManagerCRBuilder {
	b.tm.Spec.TrustManagerConfig.DefaultCAPackage.Policy = policy
	return b
}

func (b *trustManagerCRBuilder) WithFilterExpiredCertificates(policy v1alpha1.Mode) *trustManagerCRBuilder {
	b.tm.Spec.TrustManagerConfig.FilterExpiredCertificates = policy
	return b
}

func (b *trustManagerCRBuilder) WithFilterNonCACerts(policy v1alpha1.Mode) *trustManagerCRBuilder {
	b.tm.Spec.TrustManagerConfig.FilterNonCACerts = policy
	return b
}

func (b *trustManagerCRBuilder) WithTargetNamespaces(namespaces ...string) *trustManagerCRBuilder {
	b.tm.Spec.TrustManagerConfig.TargetNamespaces = namespaces
	return b
}

func (b *trustManagerCRBuilder) Build() *v1alpha1.TrustManager {
	return b.tm
}

// ---------------------------------------------------------------------------
// TrustManager CR helpers
// ---------------------------------------------------------------------------

func trustManagerClient() operatorclientv1alpha1.TrustManagerInterface {
	return certmanageroperatorclient.OperatorV1alpha1().TrustManagers()
}

func waitForTrustManagerReady(ctx context.Context) v1alpha1.TrustManagerStatus {
	By("waiting for TrustManager CR to be ready")
	status, err := pollTillTrustManagerAvailable(ctx, trustManagerClient(), "cluster")
	Expect(err).Should(BeNil())
	return status
}

func createTrustManager(ctx context.Context, b *trustManagerCRBuilder) {
	By("creating TrustManager CR")
	_, err := trustManagerClient().Create(ctx, b.Build(), metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		// TrustManager is a cluster singleton named "cluster". Some CI jobs
		// (for example tls-scanner deploy-operand) may already have created it.
		By("TrustManager CR already exists; waiting for it to be ready")
		waitForTrustManagerReady(ctx)
		return
	}
	Expect(err).ShouldNot(HaveOccurred())
	waitForTrustManagerReady(ctx)
}

func deleteTrustManager(ctx context.Context) {
	By("deleting TrustManager CR")
	_ = trustManagerClient().Delete(ctx, "cluster", metav1.DeleteOptions{})
	Eventually(func() bool {
		_, err := trustManagerClient().Get(ctx, "cluster", metav1.GetOptions{})
		return apierrors.IsNotFound(err)
	}, lowTimeout, fastPollInterval).Should(BeTrue())

	cleanupTrustManagerOperandLeavings(ctx)
}

// cleanupTrustManagerOperandLeavings removes operand resources left behind when the TrustManager CR
// is deleted. The operator does not tear these down today (see controller cleanUp TODO).
// The Deployment is removed before the ServiceAccount so no pod is left mounted
// with a token for an account that is about to be deleted. Background deletion
// drops the Deployment object before its ReplicaSets and pods finish terminating,
// so those must be gone before the ServiceAccount is deleted.
func cleanupTrustManagerOperandLeavings(ctx context.Context) {
	By("cleaning up trust-manager operand Deployment if present")
	_ = k8sClientSet.AppsV1().Deployments(trustManagerNamespace).Delete(ctx, trustManagerDeploymentName, metav1.DeleteOptions{})
	Eventually(func() bool {
		_, err := k8sClientSet.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
		return apierrors.IsNotFound(err)
	}, lowTimeout, fastPollInterval).Should(BeTrue())

	// A ReplicaSet that is still present can create a replacement pod after the
	// current pods have disappeared, so both have to be gone.
	By("waiting for trust-manager ReplicaSets and pods to disappear")
	selector := "app=" + trustManagerCommonName
	Eventually(func() bool {
		replicaSets, err := k8sClientSet.AppsV1().ReplicaSets(trustManagerNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: selector,
		})
		if err != nil || len(replicaSets.Items) != 0 {
			return false
		}
		pods, err := k8sClientSet.CoreV1().Pods(trustManagerNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: selector,
		})
		if err != nil {
			return false
		}
		return len(pods.Items) == 0
	}, lowTimeout, fastPollInterval).Should(BeTrue())

	By("cleaning up trust-manager operand ServiceAccount if present")
	_ = k8sClientSet.CoreV1().ServiceAccounts(trustManagerNamespace).Delete(ctx, trustManagerServiceAccountName, metav1.DeleteOptions{})
	Eventually(func() bool {
		_, err := k8sClientSet.CoreV1().ServiceAccounts(trustManagerNamespace).Get(ctx, trustManagerServiceAccountName, metav1.GetOptions{})
		return apierrors.IsNotFound(err)
	}, lowTimeout, fastPollInterval).Should(BeTrue())

	deleteTrustManagerDefaultCAPackageConfigMap(ctx)
	cleanupTrustManagerApproverPolicyResources(ctx)

	By("cleaning up leftover target namespace Role and RoleBinding in the trust namespace")
	_ = k8sClientSet.RbacV1().Roles(trustManagerNamespace).Delete(ctx, "trust-manager-target", metav1.DeleteOptions{})
	_ = k8sClientSet.RbacV1().RoleBindings(trustManagerNamespace).Delete(ctx, "trust-manager-target", metav1.DeleteOptions{})
}

// deleteTrustManagerDefaultCAPackageConfigMap removes the operand ConfigMap created when
// default CAPackage is Enabled. The operator does not delete it when the CR is removed
// or policy is Disabled, so e2e tests must clean it up to avoid cross-suite pollution.
func deleteTrustManagerDefaultCAPackageConfigMap(ctx context.Context) {
	_ = k8sClientSet.CoreV1().ConfigMaps(trustManagerNamespace).Delete(ctx, defaultCAPackageConfigMapName, metav1.DeleteOptions{})
}

var certificateRequestPolicyGVR = schema.GroupVersionResource{
	Group:    "policy.cert-manager.io",
	Version:  "v1alpha1",
	Resource: "certificaterequestpolicies",
}

// certificateRequestPolicyAPIAvailable reports whether the CertificateRequestPolicy API is served.
func certificateRequestPolicyAPIAvailable(ctx context.Context) bool {
	resources, err := k8sClientSet.Discovery().ServerResourcesForGroupVersion(certificateRequestPolicyGVR.GroupVersion().String())
	if err != nil || resources == nil {
		return false
	}
	for _, r := range resources.APIResources {
		if r.Name == certificateRequestPolicyGVR.Resource {
			return true
		}
	}
	return false
}

func getCertificateRequestPolicy(ctx context.Context, name string) error {
	_, err := loader.DynamicClient.Resource(certificateRequestPolicyGVR).Get(ctx, name, metav1.GetOptions{})
	return err
}

func deleteCertificateRequestPolicy(ctx context.Context, name string) error {
	return loader.DynamicClient.Resource(certificateRequestPolicyGVR).Delete(ctx, name, metav1.DeleteOptions{})
}

// cleanupTrustManagerApproverPolicyResources removes bootstrap CRP/RBAC left behind when the
// TrustManager CR is deleted (operator cleanUp does not tear these down today).
func cleanupTrustManagerApproverPolicyResources(ctx context.Context) {
	if certificateRequestPolicyAPIAvailable(ctx) {
		_ = deleteCertificateRequestPolicy(ctx, trustManagerCertificateRequestPolicyName)
	}
	_ = k8sClientSet.RbacV1().ClusterRoleBindings().Delete(ctx, trustManagerPolicyClusterRoleBindingName, metav1.DeleteOptions{})
	_ = k8sClientSet.RbacV1().ClusterRoles().Delete(ctx, trustManagerPolicyClusterRoleName, metav1.DeleteOptions{})
}

// ---------------------------------------------------------------------------
// Bundle builder
// ---------------------------------------------------------------------------

// bundleBuilder provides a fluent API for constructing trust.cert-manager.io/v1alpha1 Bundle objects.
type bundleBuilder struct {
	bundle *trustapi.Bundle
}

func newBundle(name string) *bundleBuilder {
	return &bundleBuilder{
		bundle: &trustapi.Bundle{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec:       trustapi.BundleSpec{},
		},
	}
}

func (b *bundleBuilder) WithInLineSource(pemData string) *bundleBuilder {
	b.bundle.Spec.Sources = append(b.bundle.Spec.Sources, trustapi.BundleSource{InLine: pemData})
	return b
}

func (b *bundleBuilder) WithConfigMapSource(name, key string) *bundleBuilder {
	b.bundle.Spec.Sources = append(b.bundle.Spec.Sources, trustapi.BundleSource{
		ConfigMap: &trustapi.SourceObjectKeySelector{Name: name, Key: key},
	})
	return b
}

func (b *bundleBuilder) WithSecretSource(name, key string) *bundleBuilder {
	b.bundle.Spec.Sources = append(b.bundle.Spec.Sources, trustapi.BundleSource{
		Secret: &trustapi.SourceObjectKeySelector{Name: name, Key: key},
	})
	return b
}

func (b *bundleBuilder) WithUseDefaultCAs() *bundleBuilder {
	b.bundle.Spec.Sources = append(b.bundle.Spec.Sources, trustapi.BundleSource{UseDefaultCAs: ptr.To(true)})
	return b
}

func (b *bundleBuilder) WithConfigMapTarget(key string) *bundleBuilder {
	b.bundle.Spec.Target.ConfigMap = &trustapi.TargetTemplate{Key: key}
	return b
}

func (b *bundleBuilder) WithSecretTarget(key string) *bundleBuilder {
	b.bundle.Spec.Target.Secret = &trustapi.TargetTemplate{Key: key}
	return b
}

func (b *bundleBuilder) WithTargetMetadata(labels, annotations map[string]string) *bundleBuilder {
	meta := trustapi.TargetMetadata{Labels: labels, Annotations: annotations}
	if b.bundle.Spec.Target.ConfigMap != nil {
		b.bundle.Spec.Target.ConfigMap.Metadata = meta
	}
	if b.bundle.Spec.Target.Secret != nil {
		b.bundle.Spec.Target.Secret.Metadata = meta
	}
	return b
}

func (b *bundleBuilder) WithNamespaceSelector(matchLabels map[string]string) *bundleBuilder {
	b.bundle.Spec.Target.NamespaceSelector = &metav1.LabelSelector{MatchLabels: matchLabels}
	return b
}

func (b *bundleBuilder) Build() *trustapi.Bundle {
	return b.bundle
}

// ---------------------------------------------------------------------------
// Bundle helpers
// ---------------------------------------------------------------------------

// waitForBundleCondition polls until the Bundle has a status condition matching
// the given type and status, or until timeout.
func waitForBundleCondition(ctx context.Context, cl crclient.Client, bundleName, conditionType string, conditionStatus metav1.ConditionStatus, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, fastPollInterval, timeout, true, func(pollCtx context.Context) (bool, error) {
		var bundle trustapi.Bundle
		if err := cl.Get(pollCtx, crclient.ObjectKey{Name: bundleName}, &bundle); err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		for _, c := range bundle.Status.Conditions {
			if c.Type == conditionType && c.Status == conditionStatus {
				return true, nil
			}
		}
		return false, nil
	})
}

// waitForConfigMapTarget polls until a ConfigMap with the Bundle name exists in the
// given namespace and its data key contains the expected content.
func waitForConfigMapTarget(ctx context.Context, cl crclient.Client, bundleName, namespace, key, expectedContent string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, fastPollInterval, timeout, true, func(pollCtx context.Context) (bool, error) {
		var cm corev1.ConfigMap
		if err := cl.Get(pollCtx, crclient.ObjectKey{Namespace: namespace, Name: bundleName}, &cm); err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		data, ok := cm.Data[key]
		if !ok {
			return false, nil
		}
		if expectedContent != "" {
			return strings.Contains(strings.TrimSpace(data), strings.TrimSpace(expectedContent)), nil
		}
		return len(data) > 0, nil
	})
}

// waitForSecretTarget polls until a Secret with the Bundle name exists in the
// given namespace and its data key contains the expected content.
func waitForSecretTarget(ctx context.Context, cl crclient.Client, bundleName, namespace, key, expectedContent string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, fastPollInterval, timeout, true, func(pollCtx context.Context) (bool, error) {
		var secret corev1.Secret
		if err := cl.Get(pollCtx, crclient.ObjectKey{Namespace: namespace, Name: bundleName}, &secret); err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		data, ok := secret.Data[key]
		if !ok {
			return false, nil
		}
		if expectedContent != "" {
			return strings.Contains(strings.TrimSpace(string(data)), strings.TrimSpace(expectedContent)), nil
		}
		return len(data) > 0, nil
	})
}

// waitForTargetRemoved polls until the target ConfigMap or Secret with the
// Bundle name no longer exists in the given namespace.
func waitForTargetRemoved(ctx context.Context, cl crclient.Client, bundleName, namespace string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, fastPollInterval, timeout, true, func(pollCtx context.Context) (bool, error) {
		var cm corev1.ConfigMap
		cmErr := cl.Get(pollCtx, crclient.ObjectKey{Namespace: namespace, Name: bundleName}, &cm)
		var secret corev1.Secret
		secretErr := cl.Get(pollCtx, crclient.ObjectKey{Namespace: namespace, Name: bundleName}, &secret)
		return apierrors.IsNotFound(cmErr) && apierrors.IsNotFound(secretErr), nil
	})
}

// containsPEMCertificates returns true if the data contains at least one
// valid PEM-encoded CERTIFICATE block.
func containsPEMCertificates(data string) bool {
	block, _ := pem.Decode([]byte(data))
	return block != nil && block.Type == "CERTIFICATE"
}

func deleteBundle(ctx context.Context, name string) {
	var bundle trustapi.Bundle
	bundle.Name = name
	_ = bundleClient.Delete(ctx, &bundle)
	Eventually(func() bool {
		err := bundleClient.Get(ctx, crclient.ObjectKey{Name: name}, &trustapi.Bundle{})
		return apierrors.IsNotFound(err)
	}, lowTimeout, fastPollInterval).Should(BeTrue())
}

func createBundleWithCleanup(ctx context.Context, bundle *trustapi.Bundle) {
	Eventually(func() error {
		return bundleClient.Create(ctx, bundle)
	}, lowTimeout, fastPollInterval).Should(Succeed(), "failed to create Bundle %q (webhook may not be ready yet)", bundle.Name)
	DeferCleanup(func() { deleteBundle(ctx, bundle.Name) })
}

func createNamespaceWithCleanup(ctx context.Context, prefix string, labels map[string]string) *corev1.Namespace {
	ns, err := k8sClientSet.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{GenerateName: prefix, Labels: labels},
	}, metav1.CreateOptions{})
	Expect(err).ShouldNot(HaveOccurred())
	DeferCleanup(func() {
		_ = k8sClientSet.CoreV1().Namespaces().Delete(ctx, ns.Name, metav1.DeleteOptions{})
	})
	return ns
}

func createSourceConfigMap(ctx context.Context, namespace, name, key, data string) {
	Expect(library.UpsertConfigMap(ctx, k8sClientSet, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Data:       map[string]string{key: data},
	})).ShouldNot(HaveOccurred())
	DeferCleanup(func() {
		_ = k8sClientSet.CoreV1().ConfigMaps(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	})
}

func createSourceSecret(ctx context.Context, namespace, name, key, data string) {
	Expect(library.UpsertSecret(ctx, k8sClientSet, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Data:       map[string][]byte{key: []byte(data)},
	})).ShouldNot(HaveOccurred())
	DeferCleanup(func() {
		_ = k8sClientSet.CoreV1().Secrets(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	})
}

func verifyBundleSynced(ctx context.Context, bundleName string) {
	By("verifying Bundle status shows Synced")
	err := waitForBundleCondition(ctx, bundleClient, bundleName, trustapi.BundleConditionSynced, metav1.ConditionTrue, lowTimeout)
	Expect(err).ShouldNot(HaveOccurred())
}

func verifyBundleNeverSynced(ctx context.Context, bundleName string) {
	Consistently(func() bool {
		var b trustapi.Bundle
		if err := bundleClient.Get(ctx, crclient.ObjectKey{Name: bundleName}, &b); err != nil {
			return false
		}
		for _, c := range b.Status.Conditions {
			if c.Type == trustapi.BundleConditionSynced && c.Status == metav1.ConditionTrue {
				return false
			}
		}
		return true
	}, "60s", fastPollInterval).Should(BeTrue())
}

// ---------------------------------------------------------------------------
// TrustManager approver-policy integration helpers
// ---------------------------------------------------------------------------

// trustManagerApproverPolicyAfterEach restores Managed integration and deletes
// ApproverPolicyManager. It waits for built-in approval and policy/RBAC cleanup
// before deleting TrustManager, keeping its controller active during cleanup.
func trustManagerApproverPolicyAfterEach(ctx context.Context, client crclient.Client) {
	if client == nil {
		return
	}

	By("restoring Managed integration before removing approver-policy")
	if _, err := trustManagerClient().Get(ctx, "cluster", metav1.GetOptions{}); err == nil {
		setTrustManagerApproverPolicyState(ctx, v1alpha1.ApproverPolicyManagementStateManaged)
	}
	Expect(crclient.IgnoreNotFound(approverPolicyClient().Delete(ctx, "cluster", metav1.DeleteOptions{}))).To(Succeed())
	Eventually(func() bool {
		_, err := approverPolicyClient().Get(ctx, "cluster", metav1.GetOptions{})
		return apierrors.IsNotFound(err)
	}, highTimeout, fastPollInterval).Should(BeTrue())
	Eventually(func(g Gomega) { verifyAutoApproverState(ctx, g, false) }, highTimeout, fastPollInterval).Should(Succeed())
	Eventually(func(g Gomega) { verifyTrustManagerPolicyResources(ctx, g, client, false) }, highTimeout, fastPollInterval).Should(Succeed())
	deleteTrustManager(ctx)
}

// trustManagerPolicyResources returns name-only objects for TrustManager's
// CertificateRequestPolicy, ClusterRole, and ClusterRoleBinding for use in API calls.
func trustManagerPolicyResources() []crclient.Object {
	return []crclient.Object{
		&policyv1alpha1.CertificateRequestPolicy{ObjectMeta: metav1.ObjectMeta{Name: "trust-manager-policy"}},
		&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "trust-manager-policy-role"}},
		&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "trust-manager-policy-binding"}},
	}
}

// verifyTrustManagerPolicyResources checks whether all integration resources are
// present or absent as requested. When present, the binding must reference only
// the cert-manager ServiceAccount in the cert-manager namespace.
func verifyTrustManagerPolicyResources(ctx context.Context, g Gomega, c crclient.Client, present bool) {
	for _, obj := range trustManagerPolicyResources() {
		err := c.Get(ctx, crclient.ObjectKeyFromObject(obj), obj)
		if !present {
			g.Expect(apierrors.IsNotFound(err)).To(BeTrue(), "%s should be absent: %v", obj.GetName(), err)
			continue
		}
		g.Expect(err).NotTo(HaveOccurred())
		if binding, ok := obj.(*rbacv1.ClusterRoleBinding); ok {
			g.Expect(binding.Subjects).To(Equal([]rbacv1.Subject{{Kind: "ServiceAccount", Name: "cert-manager", Namespace: "cert-manager"}}))
		}
	}
}

// setTrustManagerApproverPolicyState updates the singleton's approver-policy
// management state, re-reading the CR on update conflicts to preserve other fields.
func setTrustManagerApproverPolicyState(ctx context.Context, state v1alpha1.ApproverPolicyManagementState) {
	Expect(retry.RetryOnConflict(retry.DefaultRetry, func() error {
		tm, err := trustManagerClient().Get(ctx, "cluster", metav1.GetOptions{})
		if err != nil {
			return err
		}
		tm.Spec.TrustManagerConfig.WebhookTLS.ApproverPolicy.ManagementState = state
		_, err = trustManagerClient().Update(ctx, tm, metav1.UpdateOptions{})
		return err
	})).To(Succeed())
}

// trustManagerCertificateRequestUIDs snapshots the current webhook CertificateRequest
// UIDs so issuance and renewal checks can exclude requests that already exist.
func trustManagerCertificateRequestUIDs(ctx context.Context) map[types.UID]bool {
	requests, err := certmanagerClient.CertmanagerV1().CertificateRequests(operandNamespace).List(ctx, metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred())
	uids := map[types.UID]bool{}
	for _, request := range requests.Items {
		if request.Annotations["cert-manager.io/certificate-name"] == trustManagerCertificateName {
			uids[request.UID] = true
		}
	}
	return uids
}

// waitForTrustManagerPolicyApproval waits for a webhook request outside the supplied
// UID snapshot to be approved by trust-manager-policy and contain the issued certificate.
func waitForTrustManagerPolicyApproval(ctx context.Context, previousRequests map[types.UID]bool) {
	Eventually(func(g Gomega) {
		requests, err := certmanagerClient.CertmanagerV1().CertificateRequests(operandNamespace).List(ctx, metav1.ListOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		for _, request := range requests.Items {
			if previousRequests[request.UID] || request.Annotations["cert-manager.io/certificate-name"] != trustManagerCertificateName {
				continue
			}
			for _, condition := range request.Status.Conditions {
				if condition.Type == cmv1.CertificateRequestConditionApproved && condition.Status == cmmeta.ConditionTrue {
					g.Expect(condition.Reason).To(Equal("policy.cert-manager.io"))
					g.Expect(condition.Message).To(ContainSubstring("trust-manager-policy"))
					g.Expect(request.Status.Certificate).NotTo(BeEmpty(), "approved request must also be signed")
					return
				}
			}
		}
		g.Expect(false).To(BeTrue(), "expected a new signed webhook CertificateRequest approved by trust-manager-policy")
	}, highTimeout, fastPollInterval).Should(Succeed())
}

// waitForTrustManagerCertificateRequest returns a webhook request created after
// the supplied UID snapshot, avoiding requests left behind by earlier specs.
func waitForTrustManagerCertificateRequest(ctx context.Context, previousRequests map[types.UID]bool) *cmv1.CertificateRequest {
	var request *cmv1.CertificateRequest
	Eventually(func(g Gomega) {
		requests, err := certmanagerClient.CertmanagerV1().CertificateRequests(operandNamespace).List(ctx, metav1.ListOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		for i := range requests.Items {
			candidate := &requests.Items[i]
			if !previousRequests[candidate.UID] && candidate.Annotations["cert-manager.io/certificate-name"] == trustManagerCertificateName {
				request = candidate
				return
			}
		}
		g.Expect(request).NotTo(BeNil(), "expected a new webhook CertificateRequest")
	}, highTimeout, fastPollInterval).Should(Succeed())
	return request
}

// verifyTrustManagerPolicyResourceRecreation deletes each integration resource in
// turn and waits for it to return with a new UID, without modifying TrustManager.
func verifyTrustManagerPolicyResourceRecreation(ctx context.Context, client crclient.Client) {
	for _, obj := range trustManagerPolicyResources() {
		Expect(client.Get(ctx, crclient.ObjectKeyFromObject(obj), obj)).To(Succeed())
		uid := obj.GetUID()
		Expect(client.Delete(ctx, obj)).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(client.Get(ctx, crclient.ObjectKeyFromObject(obj), obj)).To(Succeed())
			g.Expect(obj.GetUID()).NotTo(Equal(uid))
		}, highTimeout, fastPollInterval).Should(Succeed())
	}
}
