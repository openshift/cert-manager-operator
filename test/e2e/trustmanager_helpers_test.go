//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"encoding/pem"
	"strings"
	"time"

	trustapi "github.com/cert-manager/trust-manager/pkg/apis/trust/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/test/library"
	operatorclientv1alpha1 "github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned/typed/operator/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
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

func (b *trustManagerCRBuilder) WithDefaultCAPackage(policy v1alpha1.DefaultCAPackagePolicy) *trustManagerCRBuilder {
	b.tm.Spec.TrustManagerConfig.DefaultCAPackage.Policy = policy
	return b
}

func (b *trustManagerCRBuilder) WithFilterExpiredCertificates(policy v1alpha1.FilterExpiredCertificatesPolicy) *trustManagerCRBuilder {
	b.tm.Spec.TrustManagerConfig.FilterExpiredCertificates = policy
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
	b.bundle.Spec.Sources = append(b.bundle.Spec.Sources, trustapi.BundleSource{InLine: &pemData})
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
	meta := &trustapi.TargetMetadata{Labels: labels, Annotations: annotations}
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
