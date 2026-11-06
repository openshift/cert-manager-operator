package trustmanager

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
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
			policy, err := r.getNetworkPolicyFromAsset(tt.assetPath, testResourceLabels(), testResourceAnnotations())
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
					policy, err := r.getNetworkPolicyFromAsset(trustManagerNetworkPolicyAssets[call-1], labels, testResourceAnnotations())
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
					policy, err := r.getNetworkPolicyFromAsset(trustManagerNetworkPolicyAssets[call-1], labels, testResourceAnnotations())
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
		{
			name:      "apply when existing has annotation drift",
			tmBuilder: testTrustManager().WithAnnotations(map[string]string{"user-annotation": "original"}),
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				call := 0
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					call++
					tm := testTrustManager().WithAnnotations(map[string]string{"user-annotation": "original"}).Build()
					policy, err := r.getNetworkPolicyFromAsset(trustManagerNetworkPolicyAssets[call-1], getResourceLabels(tm), getResourceAnnotations(tm))
					if err != nil {
						return false, err
					}
					if call == 1 {
						policy.Annotations["user-annotation"] = "tampered"
					}
					policy.DeepCopyInto(obj.(*networkingv1.NetworkPolicy))
					return true, nil
				})
			},
			wantExistsCount: 4,
			wantPatchCount:  1,
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
			err := r.createOrApplyNetworkPolicies(tm, getResourceLabels(tm), getResourceAnnotations(tm))
			assertError(t, err, tt.wantErr)

			if got := mock.ExistsCallCount(); got != tt.wantExistsCount {
				t.Errorf("Exists calls: got %d want %d", got, tt.wantExistsCount)
			}
			if got := mock.PatchCallCount(); got != tt.wantPatchCount {
				t.Errorf("Patch calls: got %d want %d", got, tt.wantPatchCount)
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

func TestGetNetworkPolicyFromAsset_operatorOwnedByLabelWinsOverUserLabels(t *testing.T) {
	r := testReconciler(t)
	tm := testTrustManager().WithLabels(map[string]string{
		"app":                                    "user-app",
		"app.kubernetes.io/name":                 "user-name",
		trustManagerNetworkPolicyOwnedByLabelKey: "user-override",
		"team":                                   "platform",
	}).Build()

	policy, err := r.getNetworkPolicyFromAsset(trustManagerDenyAllNetworkPolicyAssetName, getResourceLabels(tm), getResourceAnnotations(tm))
	if err != nil {
		t.Fatalf("getNetworkPolicyFromAsset: %v", err)
	}
	if got := policy.Labels["app"]; got != trustManagerCommonName {
		t.Errorf("app: got %q want %q", got, trustManagerCommonName)
	}
	if got := policy.Labels["app.kubernetes.io/name"]; got != trustManagerCommonName {
		t.Errorf("app.kubernetes.io/name: got %q want %q", got, trustManagerCommonName)
	}
	if got := policy.Labels[trustManagerNetworkPolicyOwnedByLabelKey]; got != trustManagerNetworkPolicyOwnedByValue {
		t.Errorf("owned-by: got %q want %q", got, trustManagerNetworkPolicyOwnedByValue)
	}
	if got := policy.Labels["team"]; got != "platform" {
		t.Errorf("extra user label: got %q want %q", got, "platform")
	}
}

func TestGetNetworkPolicyFromAsset_appliesUserAnnotations(t *testing.T) {
	r := testReconciler(t)
	tm := testTrustManager().WithAnnotations(map[string]string{
		"user-annotation": "test-value",
	}).Build()

	policy, err := r.getNetworkPolicyFromAsset(trustManagerDenyAllNetworkPolicyAssetName, getResourceLabels(tm), getResourceAnnotations(tm))
	if err != nil {
		t.Fatalf("getNetworkPolicyFromAsset: %v", err)
	}
	if got := policy.Annotations["user-annotation"]; got != "test-value" {
		t.Errorf("user annotation: got %q want %q", got, "test-value")
	}

	empty, err := r.getNetworkPolicyFromAsset(trustManagerDenyAllNetworkPolicyAssetName, getResourceLabels(tm), nil)
	if err != nil {
		t.Fatalf("getNetworkPolicyFromAsset with nil annotations: %v", err)
	}
	if len(empty.Annotations) != 0 {
		t.Errorf("nil annotations: got %v want empty", empty.Annotations)
	}
}

func TestReconcileTrustManagerDeployment_appliesFourPoliciesWithoutCertManager(t *testing.T) {
	t.Setenv(trustManagerImageNameEnvVarName, testImage)

	r := testReconciler(t)
	mock := &fakes.FakeCtrlClient{}
	mock.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
		if _, ok := obj.(*corev1.Namespace); ok {
			return true, nil
		}
		return false, nil
	})
	mock.GetCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
		if _, ok := obj.(*v1alpha1.CertManager); ok {
			t.Errorf("reconcile consulted CertManager %s; trust-manager policies ignore spec.defaultNetworkPolicy", key)
		}
		return nil
	})
	r.CtrlClient = mock

	if err := r.reconcileTrustManagerDeployment(testTrustManager().Build()); err != nil {
		t.Fatalf("reconcileTrustManagerDeployment: %v", err)
	}
	if got := mock.DeleteCallCount(); got != 0 {
		t.Errorf("Delete calls: got %d want 0", got)
	}

	wantNames := map[string]struct{}{
		"trust-manager-deny-all":                   {},
		"trust-manager-allow-egress-to-api-server": {},
		"trust-manager-allow-ingress-to-webhook":   {},
		"trust-manager-allow-ingress-to-metrics":   {},
	}
	gotNames := map[string]struct{}{}
	for i := 0; i < mock.PatchCallCount(); i++ {
		_, obj, patch, _ := mock.PatchArgsForCall(i)
		policy, ok := obj.(*networkingv1.NetworkPolicy)
		if !ok {
			continue
		}
		if patch != client.Apply {
			t.Errorf("network policy %s: expected client.Apply", policy.Name)
		}
		if policy.Namespace != operandNamespace {
			t.Errorf("%s namespace: got %q want %q", policy.Name, policy.Namespace, operandNamespace)
		}
		if got := policy.Spec.PodSelector.MatchLabels["app"]; got != trustManagerCommonName {
			t.Errorf("%s selector app: got %q want %q", policy.Name, got, trustManagerCommonName)
		}
		if got := policy.Labels[trustManagerNetworkPolicyOwnedByLabelKey]; got != trustManagerNetworkPolicyOwnedByValue {
			t.Errorf("%s owned-by: got %q want %q", policy.Name, got, trustManagerNetworkPolicyOwnedByValue)
		}
		gotNames[policy.Name] = struct{}{}
	}
	if len(gotNames) != len(wantNames) {
		t.Fatalf("applied network policies: got %v want %v", gotNames, wantNames)
	}
	for name := range wantNames {
		if _, ok := gotNames[name]; !ok {
			t.Errorf("missing network policy %s", name)
		}
	}
}

func TestCreateOrApplyNetworkPolicies_doesNotDeleteUnrelatedNetworkPolicy(t *testing.T) {
	r := testReconciler(t)
	mock := &fakes.FakeCtrlClient{}
	mock.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
		return false, nil
	})
	r.CtrlClient = mock

	tm := testTrustManager().Build()
	if err := r.createOrApplyNetworkPolicies(tm, getResourceLabels(tm), getResourceAnnotations(tm)); err != nil {
		t.Fatalf("createOrApplyNetworkPolicies: %v", err)
	}
	if got := mock.DeleteCallCount(); got != 0 {
		t.Errorf("Delete calls: got %d want 0", got)
	}
	if got := mock.PatchCallCount(); got != 4 {
		t.Errorf("Patch calls: got %d want 4", got)
	}
}

func TestNetworkPolicyModified_detectsSpecDrift(t *testing.T) {
	desired := &networkingv1.NetworkPolicy{
		Spec: networkingv1.NetworkPolicySpec{
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress},
		},
	}
	existing := desired.DeepCopy()
	existing.Spec.PolicyTypes = []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}
	if !networkPolicyModified(desired, existing) {
		t.Error("expected modified when policyTypes differ")
	}
}
