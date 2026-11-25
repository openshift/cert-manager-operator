package trustmanager

import (
	"context"
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	"github.com/openshift/cert-manager-operator/pkg/controller/common/fakes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestCreateOrApplyNetworkPolicies_CreatesFourPolicies(t *testing.T) {
	t.Setenv(trustManagerImageVersionEnvVarName, "v0.25.0")

	r := testReconciler(t)
	mock := &fakes.FakeCtrlClient{}
	r.CtrlClient = mock

	var created int
	mock.CreateCalls(func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
		if _, ok := obj.(*networkingv1.NetworkPolicy); ok {
			created++
		}
		return nil
	})
	mock.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
		return false, nil
	})

	tm := testTrustManager().Build()
	labels := getResourceLabels(tm)

	if err := r.createOrApplyNetworkPolicies(tm, labels, false); err != nil {
		t.Fatalf("createOrApplyNetworkPolicies: %v", err)
	}
	if created != len(trustManagerNetworkPolicyAssets) {
		t.Fatalf("Create network policies = %d, want %d", created, len(trustManagerNetworkPolicyAssets))
	}
}

func TestCreateOrUpdateNetworkPolicy_RepairsDrift(t *testing.T) {
	r := testReconciler(t)
	mock := &fakes.FakeCtrlClient{}
	r.CtrlClient = mock

	desired := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "trust-manager-deny-all",
			Namespace: operandNamespace,
			Labels: map[string]string{
				trustManagerOwnedByLabelKey: trustManagerOwnedByLabelValue,
				"app":                        trustManagerCommonName,
			},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"app": trustManagerCommonName}},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress},
		},
	}
	fetched := desired.DeepCopy()
	fetched.Spec.PolicyTypes = []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}

	mock.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
		if policy, ok := obj.(*networkingv1.NetworkPolicy); ok {
			fetched.DeepCopyInto(policy)
		}
		return true, nil
	})

	var updated bool
	mock.UpdateWithRetryCalls(func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
		updated = true
		return nil
	})

	tm := testTrustManager().Build()
	recorder := record.NewFakeRecorder(10)
	r.eventRecorder = recorder

	if err := r.createOrUpdateNetworkPolicy(tm, desired, false); err != nil {
		t.Fatalf("createOrUpdateNetworkPolicy: %v", err)
	}
	if !updated {
		t.Fatal("expected UpdateWithRetry when spec drifted")
	}
}

func TestNetworkPolicy_OperatorLabelsPrecedence(t *testing.T) {
	t.Setenv(trustManagerImageVersionEnvVarName, "v0.25.0")

	tm := testTrustManager().WithLabels(map[string]string{
		trustManagerOwnedByLabelKey: "user-value",
		"app":                        "user-app",
	}).Build()
	labels := getResourceLabels(tm)

	policy, err := (&Reconciler{}).getNetworkPolicyFromAsset(trustManagerNetworkPolicyAssets[0], labels)
	if err != nil {
		t.Fatalf("getNetworkPolicyFromAsset: %v", err)
	}
	if policy.Labels[trustManagerOwnedByLabelKey] != trustManagerOwnedByLabelValue {
		t.Errorf("owned-by = %q, want %q", policy.Labels[trustManagerOwnedByLabelKey], trustManagerOwnedByLabelValue)
	}
	if policy.Labels["app"] != trustManagerCommonName {
		t.Errorf("app = %q, want %q", policy.Labels["app"], trustManagerCommonName)
	}
}

func TestNetworkPolicy_PodSelectorOnlyTrustManager(t *testing.T) {
	for _, asset := range trustManagerNetworkPolicyAssets {
		policy, err := (&Reconciler{}).getNetworkPolicyFromAsset(asset, controllerDefaultResourceLabels)
		if err != nil {
			t.Fatalf("asset %s: %v", asset, err)
		}
		got := policy.Spec.PodSelector.MatchLabels["app"]
		if got != trustManagerCommonName {
			t.Errorf("%s podSelector app = %q, want %q", asset, got, trustManagerCommonName)
		}
	}
}