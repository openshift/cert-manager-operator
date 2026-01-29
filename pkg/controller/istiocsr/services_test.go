package istiocsr

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/istiocsr/fakes"
)

const (
	grpcEndpoint = "cert-manager-istio-csr.istiocsr-test-ns.svc:443"
)

// Helper functions to reduce cognitive complexity

func setupExistsCallsForService(m *fakes.FakeCtrlClient, exists bool, err error, modifyService func(*corev1.Service)) {
	m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
		if svc, ok := obj.(*corev1.Service); ok {
			if modifyService != nil {
				modifyService(svc)
			}
			return exists, err
		}
		return false, nil
	})
}

func setupCreateCallsForService(m *fakes.FakeCtrlClient, err error) {
	m.CreateCalls(func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
		if _, ok := obj.(*corev1.Service); ok {
			return err
		}
		return nil
	})
}

func setupUpdateWithRetryCallsForService(m *fakes.FakeCtrlClient, err error) {
	m.UpdateWithRetryCalls(func(ctx context.Context, obj client.Object, option ...client.UpdateOption) error {
		if _, ok := obj.(*corev1.Service); ok {
			return err
		}
		return nil
	})
}

func runServiceTest(t *testing.T, r *Reconciler, mock *fakes.FakeCtrlClient, istiocsr *v1alpha1.IstioCSR, wantGRPCEndpoint, wantErr string) {
	r.ctrlClient = mock
	err := r.createOrApplyServices(istiocsr, controllerDefaultResourceLabels, false)
	if (wantErr != "" || err != nil) && (err == nil || err.Error() != wantErr) {
		t.Errorf("createOrApplyServices() err: %v, wantErr: %v", err, wantErr)
		return
	}
	if wantErr == "" && istiocsr.Status.IstioCSRGRPCEndpoint != wantGRPCEndpoint {
		t.Errorf("createOrApplyServices() got: %v, want: %s", istiocsr.Status.IstioCSRGRPCEndpoint, wantGRPCEndpoint)
	}
}

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
				setupExistsCallsForService(m, true, nil, func(svc *corev1.Service) {
					testService().DeepCopyInto(svc)
				})
			},
			wantGRPCEndpoint: grpcEndpoint,
		},
		{
			name: "service reconciliation fails while checking if exists",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				setupExistsCallsForService(m, false, testError, nil)
			},
			wantErr: `failed to check istiocsr-test-ns/cert-manager-istio-csr service resource already exists: test client error`,
		},
		{
			name: "service reconciliation fails while updating to desired state",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				setupUpdateWithRetryCallsForService(m, testError)
				setupExistsCallsForService(m, true, nil, func(svc *corev1.Service) {
					testSvc := testService()
					testSvc.SetLabels(nil)
					testSvc.DeepCopyInto(svc)
				})
			},
			wantErr: `failed to update istiocsr-test-ns/cert-manager-istio-csr service resource: test client error`,
		},
		{
			name: "service reconciliation fails while creating",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				setupCreateCallsForService(m, testError)
			},
			wantErr: `failed to create istiocsr-test-ns/cert-manager-istio-csr service resource: test client error`,
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
			istiocsr := testIstioCSR()
			if tt.updateIstioCSR != nil {
				tt.updateIstioCSR(istiocsr)
			}
			runServiceTest(t, r, mock, istiocsr, tt.wantGRPCEndpoint, tt.wantErr)
		})
	}
}
