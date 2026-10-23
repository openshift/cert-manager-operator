package apis

import (
	"context"
	"maps"
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/openshift/cert-manager-operator/pkg/controller/common"
)

// envtestCtrlClient adapts the suite's client to the common.CtrlClient
// interface the reconcilers use.
type envtestCtrlClient struct {
	client.Client
}

func (c *envtestCtrlClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object) error {
	return c.Client.Get(ctx, key, obj)
}

func (c *envtestCtrlClient) UpdateWithRetry(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	return c.Client.Update(ctx, obj, opts...)
}

func (c *envtestCtrlClient) StatusUpdate(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	return c.Client.Status().Update(ctx, obj, opts...)
}

func (c *envtestCtrlClient) Exists(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
	if err := c.Client.Get(ctx, key, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Regression tests for ApplyResource drift correction.
//
//  1. The ISTIOCSR-018 e2e failure ("should reconcile gRPC service drift"): a
//     Service port drifted by a client-side Update could not be corrected by
//     re-applying it. Service spec.ports is a granular list keyed by port and
//     protocol; once the e2e test changed the port, the operator's SSA apply of
//     the desired entry was treated as adding a second entry and was rejected
//     with `spec.ports[1].name: Duplicate value: "web"` on every reconcile.
//     ApplyResource must fall back to a JSON merge patch when the SSA apply
//     fails, which replaces lists wholesale.
//
//  2. The tech-preview e2e failure ("should reconcile deployment when default
//     CA package policy transitions to Disabled"): an annotation this manager
//     previously applied but no longer desires must be pruned when drift is
//     corrected, matching plain SSA behavior. A JSON-merge-only drift
//     correction merges annotation maps key-by-key and would leave the stale
//     annotation behind.
var _ = Describe("ApplyResource drift correction", Ordered, func() {
	var (
		ctrlClient *envtestCtrlClient
		recorder   *record.FakeRecorder
		ns         *corev1.Namespace
	)

	hasChanged := func(desired, existing *corev1.Service) bool {
		return !reflect.DeepEqual(desired.Spec.Ports, existing.Spec.Ports) ||
			common.ObjectMetadataModified(desired, existing) ||
			!maps.Equal(desired.GetAnnotations(), existing.GetAnnotations())
	}

	desiredService := func() *corev1.Service {
		return &corev1.Service{
			// TypeMeta is populated by the YAML decoder in the real operator
			// (asset manifests carry apiVersion/kind).
			TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cert-manager-istio-csr",
				Namespace: ns.Name,
				Labels: map[string]string{
					"app":                          "cert-manager-istio-csr",
					"app.kubernetes.io/name":       "cert-manager-istio-csr",
					"app.kubernetes.io/instance":   "cert-manager-istio-csr",
					"app.kubernetes.io/version":    "v0.16.0",
					"app.kubernetes.io/managed-by": "cert-manager-operator",
				},
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeClusterIP,
				Ports: []corev1.ServicePort{{
					Name:       "web",
					Port:       443,
					TargetPort: intstr.FromInt32(6443),
					Protocol:   corev1.ProtocolTCP,
				}},
				Selector: map[string]string{"app": "cert-manager-istio-csr"},
			},
		}
	}

	apply := func(desired *corev1.Service) error {
		return common.ApplyResource(
			ctx, ctrlClient, logf.Log, recorder,
			ns, desired, &corev1.Service{}, "istio-csr-controller", hasChanged,
		)
	}

	BeforeAll(func() {
		ctrlClient = &envtestCtrlClient{Client: k8sClient}
		recorder = record.NewFakeRecorder(10)

		ns = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "applyresource-test-"}}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
	})

	AfterAll(func() {
		Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
	})

	It("creates the service via server-side apply", func() {
		Expect(apply(desiredService())).To(Succeed())

		live := &corev1.Service{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(desiredService()), live)).To(Succeed())
		Expect(live.Spec.Ports).To(HaveLen(1))
		Expect(live.Spec.Ports[0].Port).To(Equal(int32(443)))
		Expect(live.Spec.ClusterIP).NotTo(BeEmpty(), "apiserver must assign a clusterIP")
	})

	It("reverts a drifted service port", func() {
		// Drift the port the way the e2e test does: client-side Update.
		live := &corev1.Service{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(desiredService()), live)).To(Succeed())
		live.Spec.Ports[0].Port = 9443
		Expect(k8sClient.Update(ctx, live, client.FieldOwner("e2e-test"))).To(Succeed())

		Expect(apply(desiredService())).To(Succeed())

		corrected := &corev1.Service{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(desiredService()), corrected)).To(Succeed())
		Expect(corrected.Spec.Ports).To(HaveLen(1), "drifted entry must be replaced, not appended")
		Expect(corrected.Spec.Ports[0].Port).To(Equal(int32(443)))
		Expect(corrected.Spec.ClusterIP).To(Equal(live.Spec.ClusterIP), "merge patch must preserve assigned clusterIP")
	})

	It("restores a drifted label value", func() {
		live := &corev1.Service{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(desiredService()), live)).To(Succeed())
		live.Labels["app.kubernetes.io/instance"] = "modified-value"
		Expect(k8sClient.Update(ctx, live, client.FieldOwner("e2e-test"))).To(Succeed())

		Expect(apply(desiredService())).To(Succeed())

		corrected := &corev1.Service{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(desiredService()), corrected)).To(Succeed())
		Expect(corrected.Labels["app.kubernetes.io/instance"]).To(Equal("cert-manager-istio-csr"))
	})

	It("prunes an annotation the manager no longer desires", func() {
		// Apply a variant carrying the default CA package hash annotation,
		// the way the operator does while the feature is enabled.
		annotated := desiredService()
		annotated.Annotations = map[string]string{
			"operator.openshift.io/default-ca-package-hash": "hash-1",
		}
		Expect(apply(annotated)).To(Succeed())

		present := &corev1.Service{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(annotated), present)).To(Succeed())
		Expect(present.Annotations).To(HaveKeyWithValue("operator.openshift.io/default-ca-package-hash", "hash-1"))

		// Re-applying the plain desired object (feature disabled) must prune
		// the annotation, matching plain SSA behavior. A JSON merge patch
		// would merge the annotation maps and leave it behind.
		Expect(apply(desiredService())).To(Succeed())

		corrected := &corev1.Service{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(annotated), corrected)).To(Succeed())
		Expect(corrected.Annotations).NotTo(HaveKey("operator.openshift.io/default-ca-package-hash"))
		Expect(corrected.Spec.Ports).To(HaveLen(1))
		Expect(corrected.Spec.Ports[0].Port).To(Equal(int32(443)))
	})

	It("is a no-op when already in the desired state", func() {
		Expect(apply(desiredService())).To(Succeed())
		Expect(apply(desiredService())).To(Succeed())

		// The two applies above must not have emitted events; the recorder
		// only holds the five events from the mutations in the earlier specs.
		Expect(recorder.Events).To(HaveLen(5),
			"one event per mutation: create, port drift, label drift, annotation add, annotation prune")
	})
})
