package istiocsr

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common/fakes"
)

const (
	grpcEndpoint = "cert-manager-istio-csr.istiocsr-test-ns.svc:443"
)

func TestCreateOrApplyServices(t *testing.T) {
	tests := []struct {
		name             string
		preReq           func(*Reconciler, *fakes.FakeCtrlClient)
		updateIstioCSR   func(*v1alpha1.IstioCSR)
		wantGRPCEndpoint string
		wantErr          string
	}{
		{
			name: "service reconciliation successful",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *corev1.Service:
						service := testService()
						service.DeepCopyInto(o)
					}
					return true, nil
				})
			},
			wantGRPCEndpoint: grpcEndpoint,
		},
		{
			name: "service reconciliation fails while checking if exists",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch obj.(type) {
					case *corev1.Service:
						return false, errTestClient
					}
					return false, nil
				})
			},
			wantErr: `failed to check if Service "istiocsr-test-ns/cert-manager-istio-csr" exists: test client error`,
		},
		{
			name: "service reconciliation fails while applying to desired state",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.PatchReturns(errTestClient)
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *corev1.Service:
						service := testService()
						service.SetLabels(nil)
						service.DeepCopyInto(o)
						return true, nil
					}
					return false, nil
				})
			},
			wantErr: `failed to apply Service "istiocsr-test-ns/cert-manager-istio-csr": test client error`,
		},
		{
			name: "service reconciliation fails while creating",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.PatchReturns(errTestClient)
			},
			wantErr: `failed to apply Service "istiocsr-test-ns/cert-manager-istio-csr": test client error`,
		},
		{
			name: "service reconciliation when server config is not empty",
			updateIstioCSR: func(i *v1alpha1.IstioCSR) {
				i.Spec.IstioCSRConfig.Server = &v1alpha1.ServerConfig{
					Port: 1234,
				}
			},
			wantGRPCEndpoint: "cert-manager-istio-csr.istiocsr-test-ns.svc:1234",
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
			istiocsr := testIstioCSR()
			if tt.updateIstioCSR != nil {
				tt.updateIstioCSR(istiocsr)
			}
			err := r.createOrApplyServices(istiocsr, controllerDefaultResourceLabels)
			if (tt.wantErr != "" || err != nil) && (err == nil || err.Error() != tt.wantErr) {
				t.Errorf("createOrApplyServices() err: %v, wantErr: %v", err, tt.wantErr)
			}
			if tt.wantErr == "" {
				if istiocsr.Status.IstioCSRGRPCEndpoint != tt.wantGRPCEndpoint {
					t.Errorf("createOrApplyServices() got: %v, want: %s", istiocsr.Status.IstioCSRGRPCEndpoint, tt.wantGRPCEndpoint)
				}
			}
		})
	}
}
