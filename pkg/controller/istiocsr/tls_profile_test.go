package istiocsr

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1 "github.com/openshift/api/config/v1"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common/fakes"
)

func apiserverCluster(tlsProfile *configv1.TLSSecurityProfile, tlsAdherence configv1.TLSAdherencePolicy) *configv1.APIServer {
	return &configv1.APIServer{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterAPIServerName,
		},
		Spec: configv1.APIServerSpec{
			TLSSecurityProfile: tlsProfile,
			TLSAdherence:       tlsAdherence,
		},
	}
}

func TestClusterTLSProfileArgs(t *testing.T) {
	tests := []struct {
		name         string
		apiServer    *configv1.APIServer
		wantErr      bool
		wantArgsNil  bool
		wantContains []string
	}{
		{
			name:        "no tlsAdherence set skips cluster TLS profile",
			apiServer:   apiserverCluster(nil, configv1.TLSAdherencePolicyNoOpinion),
			wantArgsNil: true,
		},
		{
			name:        "LegacyAdheringComponentsOnly skips cluster TLS profile for istio-csr",
			apiServer:   apiserverCluster(nil, configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly),
			wantArgsNil: true,
		},
		{
			name: "StrictAllComponents with Modern profile returns serving TLS args",
			apiServer: apiserverCluster(
				&configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType},
				configv1.TLSAdherencePolicyStrictAllComponents,
			),
			wantContains: []string{
				"--serving-tls-min-version=VersionTLS13",
				"--serving-tls-curve-preferences=X25519",
			},
		},
		{
			name: "StrictAllComponents with nil profile defaults to Intermediate",
			apiServer: apiserverCluster(
				nil,
				configv1.TLSAdherencePolicyStrictAllComponents,
			),
			wantContains: []string{
				"--serving-tls-min-version=VersionTLS12",
			},
		},
		{
			name: "StrictAllComponents with invalid custom profile returns error",
			apiServer: apiserverCluster(
				&configv1.TLSSecurityProfile{Type: configv1.TLSProfileCustomType},
				configv1.TLSAdherencePolicyStrictAllComponents,
			),
			wantErr: true,
		},
		{
			name: "unknown tlsAdherence value is treated as StrictAllComponents",
			apiServer: apiserverCluster(
				&configv1.TLSSecurityProfile{Type: configv1.TLSProfileIntermediateType},
				configv1.TLSAdherencePolicy("SomeFutureValue"),
			),
			wantContains: []string{
				"--serving-tls-min-version=VersionTLS12",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testReconciler(t)
			mock := &fakes.FakeCtrlClient{}
			mock.GetCalls(func(ctx context.Context, key types.NamespacedName, obj client.Object) error {
				if apiServer, ok := obj.(*configv1.APIServer); ok {
					tt.apiServer.DeepCopyInto(apiServer)
				}
				return nil
			})
			r.CtrlClient = mock

			args, err := r.clusterTLSProfileArgs()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantArgsNil {
				if args != nil {
					t.Fatalf("expected nil args, got %#v", args)
				}
				return
			}

			for _, want := range tt.wantContains {
				if !containsArg(args, want) {
					t.Errorf("expected to find %q in args, got %#v", want, args)
				}
			}
		})
	}
}

func TestClusterTLSProfileArgs_getError(t *testing.T) {
	r := testReconciler(t)
	mock := &fakes.FakeCtrlClient{}
	mock.GetCalls(func(ctx context.Context, key types.NamespacedName, obj client.Object) error {
		return errTestClient
	})
	r.CtrlClient = mock

	if _, err := r.clusterTLSProfileArgs(); err == nil {
		t.Fatal("expected error when fetching apiserver.config.openshift.io/cluster fails")
	}
}

func TestEnqueueAllIstioCSRRequests(t *testing.T) {
	r := testReconciler(t)
	mock := &fakes.FakeCtrlClient{}
	mock.ListCalls(func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
		istiocsrList, ok := list.(*v1alpha1.IstioCSRList)
		if !ok {
			return nil
		}
		istiocsrList.Items = []v1alpha1.IstioCSR{
			*testIstioCSR(),
		}
		return nil
	})
	r.CtrlClient = mock

	requests := r.enqueueAllIstioCSRRequests(context.Background(), &configv1.APIServer{})
	if len(requests) != 1 {
		t.Fatalf("expected 1 reconcile request, got %d", len(requests))
	}
	want := testIstioCSR()
	if requests[0].Name != want.GetName() || requests[0].Namespace != want.GetNamespace() {
		t.Fatalf("unexpected reconcile request: %#v", requests[0])
	}
}

func TestEnqueueAllIstioCSRRequests_listError(t *testing.T) {
	r := testReconciler(t)
	mock := &fakes.FakeCtrlClient{}
	mock.ListCalls(func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
		return errTestClient
	})
	r.CtrlClient = mock

	requests := r.enqueueAllIstioCSRRequests(context.Background(), &configv1.APIServer{})
	if requests != nil {
		t.Fatalf("expected nil requests on list error, got %#v", requests)
	}
}
