package trustmanager

import (
	"context"
	"testing"

	networkingv1 "k8s.io/api/networking/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/pkg/controller/common/fakes"
)

func TestNetworkPolicyFromAsset(t *testing.T) {
	tests := []struct {
		name       string
		assetPath  string
		wantName   string
		wantPort   int32
		wantEgress bool
	}{
		{
			name:      "deny all",
			assetPath: trustManagerDenyAllNetworkPolicyAssetName,
			wantName:  "trust-manager-deny-all",
		},
		{
			name:       "api egress",
			assetPath:  trustManagerAllowEgressAPIServerNetworkPolicyAssetName,
			wantName:   "trust-manager-allow-egress-to-api-server",
			wantPort:   6443,
			wantEgress: true,
		},
		{
			name:      "webhook ingress",
			assetPath: trustManagerAllowIngressWebhookNetworkPolicyAssetName,
			wantName:  "trust-manager-allow-ingress-to-webhook",
			wantPort:  6443,
		},
		{
			name:      "metrics ingress",
			assetPath: trustManagerAllowIngressMetricsNetworkPolicyAssetName,
			wantName:  "trust-manager-allow-ingress-to-metrics",
			wantPort:  9402,
		},
	}

	r := testReconciler(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy, err := r.getNetworkPolicyFromAsset(tt.assetPath, testResourceLabels())
			if err != nil {
				t.Fatalf("getNetworkPolicyFromAsset: %v", err)
			}
			if policy.Namespace != operandNamespace {
				t.Errorf("namespace: got %q want %q", policy.Namespace, operandNamespace)
			}
			if policy.Name != tt.wantName {
				t.Errorf("name: got %q want %q", policy.Name, tt.wantName)
			}
			if got := policy.Spec.PodSelector.MatchLabels["app"]; got != trustManagerCommonName {
				t.Errorf("pod selector app: got %q want %q", got, trustManagerCommonName)
			}
			if policy.Labels[trustManagerNetworkPolicyOwnedByLabelKey] != trustManagerNetworkPolicyOwnedByValue {
				t.Errorf("owned-by label missing or wrong")
			}
			if tt.wantPort != 0 {
				var port int32
				if tt.wantEgress {
					if len(policy.Spec.Egress) == 0 || len(policy.Spec.Egress[0].Ports) == 0 {
						t.Fatal("expected egress port")
					}
					port = policy.Spec.Egress[0].Ports[0].Port.IntVal
				} else {
					if len(policy.Spec.Ingress) == 0 || len(policy.Spec.Ingress[0].Ports) == 0 {
						t.Fatal("expected ingress port")
					}
					port = policy.Spec.Ingress[0].Ports[0].Port.IntVal
				}
				if port != tt.wantPort {
					t.Errorf("port: got %d want %d", port, tt.wantPort)
				}
			}
		})
	}
}

func TestCreateOrApplyNetworkPolicies(t *testing.T) {
	tests := []struct {
		name            string
		tmBuilder       *trustManagerBuilder
		preReq          func(*Reconciler, *fakes.FakeCtrlClient)
		wantPatchCount  int
		wantExistsCount int
		wantErr         string
	}{
		{
			name: "apply all four policies when missing",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					return false, nil
				})
			},
			wantExistsCount: 4,
			wantPatchCount:  4,
		},
		{
			name: "skip apply when all policies match",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				call := 0
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					call++
					labels := testResourceLabels()
					policy, err := r.getNetworkPolicyFromAsset(trustManagerNetworkPolicyAssets[call-1], labels)
					if err != nil {
						return false, err
					}
					policy.DeepCopyInto(obj.(*networkingv1.NetworkPolicy))
					return true, nil
				})
			},
			wantExistsCount: 4,
			wantPatchCount:  0,
		},
		{
			name: "apply when spec drift on deny-all",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				call := 0
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					call++
					labels := testResourceLabels()
					policy, err := r.getNetworkPolicyFromAsset(trustManagerNetworkPolicyAssets[call-1], labels)
					if err != nil {
						return false, err
					}
					if call == 1 {
						policy.Spec.PolicyTypes = []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}
					}
					policy.DeepCopyInto(obj.(*networkingv1.NetworkPolicy))
					return true, nil
				})
			},
			wantExistsCount: 4,
			wantPatchCount:  1,
		},
		{
			name:      "nil user labels do not panic",
			tmBuilder: testTrustManager().WithLabels(nil),
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					return false, nil
				})
			},
			wantExistsCount: 4,
			wantPatchCount:  4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testReconciler(t)
			mock := &fakes.FakeCtrlClient{}
			if tt.preReq != nil {
				tt.preReq(r, mock)
			}
			r.CtrlClient = mock

			tmBuilder := tt.tmBuilder
			if tmBuilder == nil {
				tmBuilder = testTrustManager()
			}
			tm := tmBuilder.Build()
			err := r.createOrApplyNetworkPolicies(tm, getResourceLabels(tm))
			assertError(t, err, tt.wantErr)

			if got := mock.ExistsCallCount(); got != tt.wantExistsCount {
				t.Errorf("Exists calls: got %d want %d", tt.wantExistsCount, got)
			}
			if got := mock.PatchCallCount(); got != tt.wantPatchCount {
				t.Errorf("Patch calls: got %d want %d", tt.wantPatchCount, got)
			}
			for i := 0; i < mock.PatchCallCount(); i++ {
				_, _, patch, _ := mock.PatchArgsForCall(i)
				if patch != client.Apply {
					t.Errorf("Patch call %d: expected client.Apply patch type", i)
				}
			}
		})
	}
}
