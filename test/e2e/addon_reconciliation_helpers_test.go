//go:build e2e

package e2e

import (
	"context"
	"maps"
	"slices"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	clientscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

const (
	addonManagedMetadataKey = "e2e.cert-manager-operator.openshift.io/managed"
	addonForeignMetadataKey = "e2e.cert-manager-operator.openshift.io/foreign"
	addonForeignFinalizer   = "e2e.cert-manager-operator.openshift.io/foreign-finalizer"
)

func newAddonTestClient() crclient.Client {
	scheme := runtime.NewScheme()
	Expect(clientscheme.AddToScheme(scheme)).To(Succeed())
	Expect(certmanagerv1.AddToScheme(scheme)).To(Succeed())
	Expect(v1alpha1.AddToScheme(scheme)).To(Succeed())
	client, err := crclient.New(cfg, crclient.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred())
	return client
}

func updateAddonObject(ctx context.Context, client crclient.Client, obj crclient.Object, mutate func()) {
	Expect(retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := client.Get(ctx, crclient.ObjectKeyFromObject(obj), obj); err != nil {
			return err
		}
		mutate()
		return client.Update(ctx, obj)
	})).To(Succeed())
}

// Observe the real API value, change one managed field, and wait for its repair.
// Expectations are independent of the production resource comparison helpers.
func verifyAddonFieldRestored[T any](ctx context.Context, client crclient.Client, obj crclient.Object, fieldName string, field func() *T, tampered T) {
	By("repairing " + fieldName + " drift on " + resourceDescription(obj))
	Expect(client.Get(ctx, crclient.ObjectKeyFromObject(obj), obj)).To(Succeed())
	original, uid := *field(), obj.GetUID()
	Expect(tampered).NotTo(Equal(original), "the test must change the field")
	updateAddonObject(ctx, client, obj, func() { *field() = tampered })
	Eventually(func(g Gomega) {
		g.Expect(client.Get(ctx, crclient.ObjectKeyFromObject(obj), obj)).To(Succeed())
		g.Expect(obj.GetUID()).To(Equal(uid), "repair the existing resource")
		g.Expect(*field()).To(Equal(original))
	}, lowTimeout, fastPollInterval).Should(Succeed())
}

func verifyAddonSpecDrift(ctx context.Context, client crclient.Client, resources []crclient.Object) {
	for _, resource := range resources {
		switch obj := resource.(type) {
		case *appsv1.Deployment:
			waitForAddonDeploymentReady(ctx, client, obj)
			verifyAddonFieldRestored(ctx, client, obj, "replicas", func() **int32 { return &obj.Spec.Replicas }, ptr.To(int32(2)))
			verifyAddonFieldRestored(ctx, client, obj, "container args", func() *[]string { return &obj.Spec.Template.Spec.Containers[0].Args }, []string{"--help"})
			verifyAddonFieldRestored(ctx, client, obj, "readiness path", func() *string { return &obj.Spec.Template.Spec.Containers[0].ReadinessProbe.HTTPGet.Path }, "/e2e-tampered")
			waitForAddonDeploymentReady(ctx, client, obj)
		case *corev1.ServiceAccount:
			Expect(client.Get(ctx, crclient.ObjectKeyFromObject(obj), obj)).To(Succeed())
			// ApproverPolicy leaves this field unspecified; TrustManager manages it.
			if obj.AutomountServiceAccountToken != nil {
				verifyAddonFieldRestored(ctx, client, obj, "automountServiceAccountToken", func() **bool { return &obj.AutomountServiceAccountToken }, ptr.To(!*obj.AutomountServiceAccountToken))
			}
		case *corev1.Service:
			Expect(client.Get(ctx, crclient.ObjectKeyFromObject(obj), obj)).To(Succeed())
			tamperedSelector := maps.Clone(obj.Spec.Selector)
			for key := range tamperedSelector {
				tamperedSelector[key] = "e2e-tampered"
			}
			verifyAddonFieldRestored(ctx, client, obj, "selector", func() *map[string]string { return &obj.Spec.Selector }, tamperedSelector)
			verifyAddonFieldRestored(ctx, client, obj, "targetPort", func() *intstr.IntOrString { return &obj.Spec.Ports[0].TargetPort }, intstr.FromInt32(1))
		case *rbacv1.ClusterRole:
			verifyAddonFieldRestored(ctx, client, obj, "rules", func() *[]rbacv1.PolicyRule { return &obj.Rules }, []rbacv1.PolicyRule(nil))
		case *rbacv1.Role:
			verifyAddonFieldRestored(ctx, client, obj, "rules", func() *[]rbacv1.PolicyRule { return &obj.Rules }, []rbacv1.PolicyRule(nil))
		case *rbacv1.ClusterRoleBinding:
			verifyAddonFieldRestored(ctx, client, obj, "subjects", func() *[]rbacv1.Subject { return &obj.Subjects }, []rbacv1.Subject(nil))
		case *rbacv1.RoleBinding:
			verifyAddonFieldRestored(ctx, client, obj, "subjects", func() *[]rbacv1.Subject { return &obj.Subjects }, []rbacv1.Subject(nil))
		case *admissionregistrationv1.ValidatingWebhookConfiguration:
			verifyAddonFieldRestored(ctx, client, obj, "service path", func() **string { return &obj.Webhooks[0].ClientConfig.Service.Path }, ptr.To("/e2e-tampered"))
			verifyAddonFieldRestored(ctx, client, obj, "rule scope", func() **admissionregistrationv1.ScopeType { return &obj.Webhooks[0].Rules[0].Scope }, ptr.To(admissionregistrationv1.NamespacedScope))
		}
	}
}

func waitForAddonDeploymentReady(ctx context.Context, client crclient.Client, deployment *appsv1.Deployment) {
	Eventually(func(g Gomega) {
		g.Expect(client.Get(ctx, crclient.ObjectKeyFromObject(deployment), deployment)).To(Succeed())
		replicas := ptr.Deref(deployment.Spec.Replicas, 1)
		g.Expect(deployment.Status.ObservedGeneration).To(Equal(deployment.Generation))
		g.Expect(deployment.Status.Replicas).To(Equal(replicas))
		g.Expect(deployment.Status.UpdatedReplicas).To(Equal(replicas))
		g.Expect(deployment.Status.AvailableReplicas).To(Equal(replicas))
	}, highTimeout, fastPollInterval).Should(Succeed())
}

// Both CRs are created with addonManagedMetadataKey in labels and annotations.
func verifyAddonMetadataRemoval(ctx context.Context, client crclient.Client, resources []crclient.Object, removeFromCR func()) {
	var deployment *appsv1.Deployment
	for _, obj := range resources {
		By("adding foreign metadata to " + resourceDescription(obj))
		Eventually(func(g Gomega) {
			g.Expect(client.Get(ctx, crclient.ObjectKeyFromObject(obj), obj)).To(Succeed())
			g.Expect(obj.GetLabels()).To(HaveKey(addonManagedMetadataKey))
			g.Expect(obj.GetAnnotations()).To(HaveKey(addonManagedMetadataKey))
		}, lowTimeout, fastPollInterval).Should(Succeed())
		By("repairing an annotation-only edit on " + resourceDescription(obj))
		updateAddonObject(ctx, client, obj, func() { obj.GetAnnotations()[addonManagedMetadataKey] = "tampered" })
		Eventually(func(g Gomega) {
			g.Expect(client.Get(ctx, crclient.ObjectKeyFromObject(obj), obj)).To(Succeed())
			g.Expect(obj.GetAnnotations()).To(HaveKeyWithValue(addonManagedMetadataKey, "remove"))
		}, lowTimeout, fastPollInterval).Should(Succeed())
		updateAddonObject(ctx, client, obj, func() {
			obj.SetLabels(maps.Clone(obj.GetLabels()))
			obj.GetLabels()[addonForeignMetadataKey] = "keep"
			obj.SetAnnotations(maps.Clone(obj.GetAnnotations()))
			obj.GetAnnotations()[addonForeignMetadataKey] = "keep"
			if dep, ok := obj.(*appsv1.Deployment); ok {
				deployment = dep
				dep.Spec.Template.Labels[addonForeignMetadataKey] = "keep"
				if dep.Spec.Template.Annotations == nil {
					dep.Spec.Template.Annotations = map[string]string{}
				}
				dep.Spec.Template.Annotations[addonForeignMetadataKey] = "keep"
			}
		})
	}

	By("removing the operator-configured metadata from the CR")
	removeFromCR()
	for _, obj := range resources {
		By("checking owned-key removal and foreign-key preservation on " + resourceDescription(obj))
		Eventually(func(g Gomega) {
			g.Expect(client.Get(ctx, crclient.ObjectKeyFromObject(obj), obj)).To(Succeed())
			g.Expect(obj.GetLabels()).NotTo(HaveKey(addonManagedMetadataKey))
			g.Expect(obj.GetAnnotations()).NotTo(HaveKey(addonManagedMetadataKey))
			g.Expect(obj.GetLabels()).To(HaveKeyWithValue(addonForeignMetadataKey, "keep"))
			g.Expect(obj.GetAnnotations()).To(HaveKeyWithValue(addonForeignMetadataKey, "keep"))
		}, lowTimeout, fastPollInterval).Should(Succeed())
	}
	Expect(deployment).NotTo(BeNil())
	waitForAddonDeploymentReady(ctx, client, deployment)
	Expect(deployment.Spec.Template.Labels).NotTo(HaveKey(addonManagedMetadataKey))
	Expect(deployment.Spec.Template.Annotations).NotTo(HaveKey(addonManagedMetadataKey))
	generation := deployment.Generation
	// Span the controllers' 30-second retry interval to catch repeated rollouts.
	Consistently(func(g Gomega) {
		g.Expect(client.Get(ctx, crclient.ObjectKeyFromObject(deployment), deployment)).To(Succeed())
		g.Expect(deployment.Generation).To(Equal(generation))
		g.Expect(deployment.Spec.Template.Labels).To(HaveKeyWithValue(addonForeignMetadataKey, "keep"))
		g.Expect(deployment.Spec.Template.Annotations).To(HaveKeyWithValue(addonForeignMetadataKey, "keep"))
	}, 40*time.Second, fastPollInterval).Should(Succeed())
}

func verifyAddonServiceAccountRollout(ctx context.Context, client crclient.Client, account *corev1.ServiceAccount, deployment *appsv1.Deployment, annotation string) {
	waitForAddonDeploymentReady(ctx, client, deployment)
	Expect(client.Get(ctx, crclient.ObjectKeyFromObject(account), account)).To(Succeed())
	oldUID, oldGeneration := account.UID, deployment.Generation
	selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
	Expect(err).NotTo(HaveOccurred())
	pods := &corev1.PodList{}
	listOptions := []crclient.ListOption{crclient.InNamespace(deployment.Namespace), crclient.MatchingLabelsSelector{Selector: selector}}
	Expect(client.List(ctx, pods, listOptions...)).To(Succeed())
	Expect(pods.Items).NotTo(BeEmpty())
	oldPods := make([]types.UID, 0, len(pods.Items))
	for _, pod := range pods.Items {
		oldPods = append(oldPods, pod.UID)
	}

	By("recreating the ServiceAccount and waiting for fresh pod credentials")
	Expect(client.Delete(ctx, account)).To(Succeed())
	Eventually(func(g Gomega) {
		g.Expect(client.Get(ctx, crclient.ObjectKeyFromObject(account), account)).To(Succeed())
		g.Expect(account.UID).NotTo(Equal(oldUID))
		g.Expect(client.Get(ctx, crclient.ObjectKeyFromObject(deployment), deployment)).To(Succeed())
		g.Expect(deployment.Spec.Template.Annotations).To(HaveKeyWithValue(annotation, string(account.UID)))
		g.Expect(deployment.Generation).To(BeNumerically(">", oldGeneration))
	}, lowTimeout, fastPollInterval).Should(Succeed())
	waitForAddonDeploymentReady(ctx, client, deployment)
	Eventually(func(g Gomega) {
		g.Expect(client.List(ctx, pods, listOptions...)).To(Succeed())
		g.Expect(pods.Items).NotTo(BeEmpty())
		for _, pod := range pods.Items {
			g.Expect(oldPods).NotTo(ContainElement(pod.UID), "old pods must terminate after the account is replaced")
		}
	}, highTimeout, fastPollInterval).Should(Succeed())
}

func verifyAddonForeignFinalizer(ctx context.Context, client crclient.Client, instance crclient.Object, operatorFinalizer string, waitReady func()) {
	instance.SetFinalizers([]string{addonForeignFinalizer})
	instance.SetLabels(map[string]string{addonForeignMetadataKey: "keep"})
	Expect(client.Create(ctx, instance)).To(Succeed())
	// A Go defer releases the hold before the suite's AfterEach waits for deletion,
	// including when an assertion fails partway through this helper.
	defer func() {
		Expect(retry.RetryOnConflict(retry.DefaultRetry, func() error {
			if err := client.Get(ctx, crclient.ObjectKeyFromObject(instance), instance); err != nil {
				return crclient.IgnoreNotFound(err)
			}
			instance.SetFinalizers(slices.DeleteFunc(instance.GetFinalizers(), func(value string) bool { return value == addonForeignFinalizer }))
			return client.Update(ctx, instance)
		})).To(Succeed())
	}()
	waitReady()
	Expect(client.Get(ctx, crclient.ObjectKeyFromObject(instance), instance)).To(Succeed())
	Expect(instance.GetFinalizers()).To(ContainElements(addonForeignFinalizer, operatorFinalizer))
	Expect(client.Delete(ctx, instance)).To(Succeed())
	Eventually(func(g Gomega) {
		g.Expect(client.Get(ctx, crclient.ObjectKeyFromObject(instance), instance)).To(Succeed())
		g.Expect(instance.GetDeletionTimestamp().IsZero()).To(BeFalse())
		g.Expect(instance.GetFinalizers()).To(ConsistOf(addonForeignFinalizer))
		g.Expect(instance.GetLabels()).To(HaveKeyWithValue(addonForeignMetadataKey, "keep"))
	}, highTimeout, fastPollInterval).Should(Succeed())
	By("releasing only the test's finalizer after the operator finishes its cleanup")
	updateAddonObject(ctx, client, instance, func() {
		instance.SetFinalizers(slices.DeleteFunc(instance.GetFinalizers(), func(value string) bool { return value == addonForeignFinalizer }))
	})
	Eventually(func() bool {
		return apierrors.IsNotFound(client.Get(ctx, crclient.ObjectKeyFromObject(instance), instance))
	}, lowTimeout, fastPollInterval).Should(BeTrue())
}
