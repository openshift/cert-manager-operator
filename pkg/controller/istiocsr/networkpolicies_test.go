package istiocsr

import (
	"testing"

	networkingv1 "k8s.io/api/networking/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openshift/cert-manager-operator/pkg/controller/common/fakes"
)

func TestGetNetworkPolicyFromAsset(t *testing.T) {
	r := testReconciler(t)
	istiocsr := testIstioCSR()
	labels := map[string]string{"app": "istio-csr", "custom": "label"}

	for _, assetPath := range istioCSRNetworkPolicyAssets {
		t.Run(assetPath, func(t *testing.T) {
			policy, err := r.getNetworkPolicyFromAsset(assetPath, istiocsr, labels)
			require.NoError(t, err)
			require.IsType(t, &networkingv1.NetworkPolicy{}, policy)

			assert.Equal(t, testIstioCSRNamespace, policy.Namespace,
				"policy namespace should match istiocsr namespace")
			assert.Equal(t, "istio-csr", policy.Labels["app"])
			assert.Equal(t, "label", policy.Labels["custom"])
		})
	}
}

func TestGetNetworkPolicyFromAsset_FallbackNamespace(t *testing.T) {
	r := testReconciler(t)
	istiocsr := testIstioCSR()
	istiocsr.Namespace = ""

	policy, err := r.getNetworkPolicyFromAsset(istioCSRNetworkPolicyAssets[0], istiocsr, nil)
	require.NoError(t, err)
	assert.Equal(t, testIstiodNamespace, policy.Namespace,
		"should fall back to Istio namespace when istiocsr namespace is empty")
}

func TestCreateOrApplyNetworkPolicies(t *testing.T) {
	fakeClient := &fakes.FakeCtrlClient{}
	fakeClient.ExistsReturns(false, nil)
	fakeClient.PatchReturns(nil)

	r := testReconciler(t)
	r.CtrlClient = fakeClient
	istiocsr := testIstioCSR()
	labels := map[string]string{"app": "istio-csr"}

	err := r.createOrApplyNetworkPolicies(istiocsr, labels)
	require.NoError(t, err)
	assert.Equal(t, len(istioCSRNetworkPolicyAssets), fakeClient.PatchCallCount(),
		"should apply one patch per network policy asset")
}

func TestCreateOrApplyNetworkPolicies_PatchError(t *testing.T) {
	fakeClient := &fakes.FakeCtrlClient{}
	fakeClient.ExistsReturns(false, nil)
	fakeClient.PatchReturns(errTestClient)

	r := testReconciler(t)
	r.CtrlClient = fakeClient
	istiocsr := testIstioCSR()

	err := r.createOrApplyNetworkPolicies(istiocsr, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to apply network policy")
}
