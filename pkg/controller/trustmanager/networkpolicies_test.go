package trustmanager

import (
	"context"
	"fmt"
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/pkg/controller/common/fakes"
)

var reservedTrustManagerNetworkPolicyNames = []string{
	"trust-manager-deny-all",
	"trust-manager-allow-egress-to-api-server",
	"trust-manager-allow-ingress-to-webhook",
	"trust-manager-allow-ingress-to-metrics",
}

func TestTrustManagerNetworkPolicy_CreatesFourReservedPolicies(t *testing.T) {
	t.Setenv(trustManagerImageVersionEnvVarName, "v0.25.0")
	r := testReconciler(t)
	mock := &fakes.FakeCtrlClient{}
	store := map[client.ObjectKey]*networkingv1.NetworkPolicy{}

	mock.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
		if np, ok := store[key]; ok {
			np.DeepCopyInto(obj.(*networkingv1.NetworkPolicy))
			return true, nil
		}
		return false, nil
	})
	mock.CreateCalls(func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
		np := obj.(*networkingv1.NetworkPolicy)
		store[client.ObjectKeyFromObject(np)] = np.DeepCopy()
		return nil
	})
	r.CtrlClient = mock

	tm := testTrustManager().Build()
	labels := getResourceLabels(tm)
	if err := r.createOrApplyNetworkPolicies(tm, labels); err != nil {
		t.Fatalf("createOrApplyNetworkPolicies: %v", err)
	}

	if got := mock.CreateCallCount(); got != 4 {
		t.Fatalf("expected 4 Create calls, got %d", got)
	}

	for _, name := range reservedTrustManagerNetworkPolicyNames {
		key := client.ObjectKey{Namespace: operandNamespace, Name: name}
		np, ok := store[key]
		if !ok {
			t.Fatalf("missing policy %s", name)
		}
		if np.Labels[trustManagerOwnedByLabelKey] != trustManagerOwnedByLabelValue {
			t.Errorf("%s: expected owned-by label", name)
		}
		if np.Spec.PodSelector.MatchLabels["app"] != trustManagerCommonName {
			t.Errorf("%s: wrong pod selector", name)
		}
	}
}

func TestTrustManagerNetworkPolicy_RestoresAfterExternalEdit(t *testing.T) {
	t.Setenv(trustManagerImageVersionEnvVarName, "v0.25.0")
	r := testReconciler(t)
	mock := &fakes.FakeCtrlClient{}
	store := map[client.ObjectKey]*networkingv1.NetworkPolicy{}

	for _, assetPath := range trustManagerNetworkPolicyAssets {
		desired, err := getNetworkPolicyFromAsset(assetPath, testResourceLabels())
		if err != nil {
			t.Fatalf("getNetworkPolicyFromAsset: %v", err)
		}
		tampered := desired.DeepCopy()
		tampered.Spec.PodSelector.MatchLabels["app"] = "tampered"
		store[client.ObjectKeyFromObject(tampered)] = tampered
	}

	mock.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
		if np, ok := store[key]; ok {
			np.DeepCopyInto(obj.(*networkingv1.NetworkPolicy))
			return true, nil
		}
		return false, nil
	})
	mock.UpdateWithRetryCalls(func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
		np := obj.(*networkingv1.NetworkPolicy)
		store[client.ObjectKeyFromObject(np)] = np.DeepCopy()
		return nil
	})
	r.CtrlClient = mock

	tm := testTrustManager().Build()
	if err := r.createOrApplyNetworkPolicies(tm, getResourceLabels(tm)); err != nil {
		t.Fatalf("createOrApplyNetworkPolicies: %v", err)
	}

	if got := mock.UpdateWithRetryCallCount(); got != 4 {
		t.Fatalf("expected 4 UpdateWithRetry calls, got %d", got)
	}

	for _, name := range reservedTrustManagerNetworkPolicyNames {
		np := store[client.ObjectKey{Namespace: operandNamespace, Name: name}]
		if np.Spec.PodSelector.MatchLabels["app"] != trustManagerCommonName {
			t.Errorf("%s: spec not restored", name)
		}
	}
}

func TestTrustManagerNetworkPolicy_OperatorMetadataOverridesUserLabels(t *testing.T) {
	t.Setenv(trustManagerImageVersionEnvVarName, "v0.25.0")
	tm := testTrustManager().
		WithLabels(map[string]string{
			"app":                          "user-override",
			trustManagerOwnedByLabelKey:    "user-override",
			"app.kubernetes.io/instance":   "user-override",
			"app.kubernetes.io/managed-by": "user-override",
		}).
		Build()
	labels := getResourceLabels(tm)

	policy, err := getNetworkPolicyFromAsset(trustManagerNetworkPolicyAssets[0], labels)
	if err != nil {
		t.Fatalf("getNetworkPolicyFromAsset: %v", err)
	}

	if policy.Labels["app"] != trustManagerCommonName {
		t.Errorf("expected operator app label, got %q", policy.Labels["app"])
	}
	if policy.Labels[trustManagerOwnedByLabelKey] != trustManagerOwnedByLabelValue {
		t.Errorf("expected operator owned-by label, got %q", policy.Labels[trustManagerOwnedByLabelKey])
	}
	if policy.Labels["app.kubernetes.io/instance"] != trustManagerCommonName {
		t.Errorf("expected operator instance label, got %q", policy.Labels["app.kubernetes.io/instance"])
	}
}

func TestTrustManagerNetworkPolicy_DoesNotDeleteUnmanagedPolicy(t *testing.T) {
	r := testReconciler(t)
	mock := &fakes.FakeCtrlClient{}
	mock.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
		return false, nil
	})
	mock.CreateCalls(func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
		return nil
	})
	r.CtrlClient = mock

	tm := testTrustManager().Build()
	if err := r.createOrApplyNetworkPolicies(tm, getResourceLabels(tm)); err != nil {
		t.Fatalf("createOrApplyNetworkPolicies: %v", err)
	}
	if mock.DeleteCallCount() != 0 {
		t.Errorf("expected no Delete calls, got %d", mock.DeleteCallCount())
	}
}

func TestTrustManagerNetworkPolicy_IgnoresCertManagerDefaultNetworkPolicy(t *testing.T) {
	// Network policy reconcile is unconditional on TrustManager; no CertManager API is consulted.
	if got := len(trustManagerNetworkPolicyAssets); got != 4 {
		t.Fatalf("expected four static assets, got %d", got)
	}
}

func TestTrustManagerNetworkPolicy_MetricsIngressFromUWMOnly(t *testing.T) {
	path := "networkpolicies/trust-manager-allow-ingress-to-metrics-networkpolicy.yaml"
	policy, err := getNetworkPolicyFromAsset(path, testResourceLabels())
	if err != nil {
		t.Fatalf("getNetworkPolicyFromAsset: %v", err)
	}
	if len(policy.Spec.Ingress) != 1 {
		t.Fatalf("expected one ingress rule, got %d", len(policy.Spec.Ingress))
	}
	rule := policy.Spec.Ingress[0]
	if len(rule.From) != 1 {
		t.Fatalf("expected namespace-restricted ingress, got %d peers", len(rule.From))
	}
	nsSel := rule.From[0].NamespaceSelector
	if nsSel == nil || nsSel.MatchLabels["kubernetes.io/metadata.name"] != "openshift-user-workload-monitoring" {
		t.Errorf("unexpected namespace selector: %+v", nsSel)
	}
	if len(rule.Ports) != 1 || rule.Ports[0].Port.IntVal != 9402 {
		t.Errorf("expected metrics port 9402, got %+v", rule.Ports)
	}
}

func TestTrustManagerNetworkPolicy_WebhookAndAPIPorts(t *testing.T) {
	webhookPath := "networkpolicies/trust-manager-allow-ingress-to-webhook-networkpolicy.yaml"
	webhook, err := getNetworkPolicyFromAsset(webhookPath, testResourceLabels())
	if err != nil {
		t.Fatalf("webhook asset: %v", err)
	}
	if len(webhook.Spec.Ingress) != 1 {
		t.Fatalf("expected one webhook ingress rule")
	}
	if len(webhook.Spec.Ingress[0].From) != 0 {
		t.Errorf("webhook ingress must not restrict source peers")
	}
	if webhook.Spec.Ingress[0].Ports[0].Port.IntVal != 6443 {
		t.Errorf("expected webhook port 6443")
	}

	egressPath := "networkpolicies/trust-manager-allow-egress-to-api-server-networkpolicy.yaml"
	egress, err := getNetworkPolicyFromAsset(egressPath, testResourceLabels())
	if err != nil {
		t.Fatalf("egress asset: %v", err)
	}
	if len(egress.Spec.Egress) != 1 || egress.Spec.Egress[0].Ports[0].Port.IntVal != 6443 {
		t.Errorf("expected API egress on TCP 6443")
	}
}

func TestNetworkPolicyFromAssetsCoversReservedNames(t *testing.T) {
	t.Setenv(trustManagerImageVersionEnvVarName, "v0.25.0")
	var names []string
	for _, asset := range trustManagerNetworkPolicyAssets {
		policy, err := getNetworkPolicyFromAsset(asset, testResourceLabels())
		if err != nil {
			t.Fatalf("%s: %v", asset, err)
		}
		if policy.Namespace != operandNamespace {
			t.Errorf("%s: namespace %q", asset, policy.Namespace)
		}
		names = append(names, policy.Name)
	}
	if !slices.Equal(names, reservedTrustManagerNetworkPolicyNames) {
		t.Errorf("names mismatch: got %v", names)
	}
}

func TestNetworkPolicyChanged(t *testing.T) {
	desired, err := getNetworkPolicyFromAsset(trustManagerNetworkPolicyAssets[0], testResourceLabels())
	if err != nil {
		t.Fatal(err)
	}
	fetched := desired.DeepCopy()
	if networkPolicyChanged(desired, fetched) {
		t.Error("expected no change for equal specs")
	}
	fetched.Spec.PodSelector.MatchLabels["app"] = "other"
	if !networkPolicyChanged(desired, fetched) {
		t.Error("expected change when spec drifts")
	}
}

// Compile-time check that reconcile does not depend on cert-manager default network policy types.
var _ = fmt.Sprintf("%v", corev1.ProtocolTCP)