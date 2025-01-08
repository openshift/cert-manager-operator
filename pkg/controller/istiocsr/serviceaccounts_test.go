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
	serviceAccountName = "cert-manager-istio-csr"
)

func TestCreateOrApplyServiceAccounts(t *testing.T) {
	tests := []struct {
		name    string
		preReq  func(*Reconciler, *fakes.FakeCtrlClient)
		wantErr string
	}{
		{
			name: "serviceaccount reconciliation successful",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *corev1.ServiceAccount:
						serviceaccount := testServiceAccount(t)
						serviceaccount.DeepCopyInto(o)
					}
					return true, nil
				})
			},
		},
		{
			name: "serviceaccount reconciliation fails while checking if exists",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch obj.(type) {
					case *corev1.ServiceAccount:
						return false, testError
					}
					return false, nil
				})
			},
			wantErr: `failed to check istiocsr-test-ns/cert-manager-istio-csr serviceaccount resource already exists: test client error`,
		},
		{
			name: "serviceaccount reconciliation fails while updating status",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.StatusUpdateCalls(func(ctx context.Context, obj client.Object, option ...client.SubResourceUpdateOption) error {
					switch obj.(type) {
					case *v1alpha1.IstioCSR:
						return testError
					}
					return nil
				})
			},
			wantErr: `failed to update istiocsr-test-ns/istiocsr-test-resource istiocsr status with istiocsr-test-ns/cert-manager-istio-csr serviceaccount resource name: failed to update istiocsr.openshift.operator.io "istiocsr-test-ns/istiocsr-test-resource" status: test client error`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testReconciler(t)
			mock := &fakes.FakeCtrlClient{}
			if tt.preReq != nil {
				tt.preReq(r, mock)
			}
			r.ctrlClient = mock
			istiocsr := testIstioCSR(t)
			err := r.createOrApplyServiceAccounts(istiocsr, controllerDefaultResourceLabels, false)
			if (tt.wantErr != "" || err != nil) && (err == nil || err.Error() != tt.wantErr) {
				t.Errorf("createOrApplyServiceAccounts() err: %v, wantErr: %v", err, tt.wantErr)
			}
			if tt.wantErr == "" {
				if istiocsr.Status.ServiceAccount != serviceAccountName {
					t.Errorf("createOrApplyServiceAccounts() got: %v, want: %s", istiocsr.Status.ServiceAccount, serviceAccountName)
				}
			}
		})
	}
}
