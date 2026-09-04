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
	serviceAccountName = "cert-manager-istio-csr"
)

func TestCreateOrApplyServiceAccounts(t *testing.T) {
	tests := []struct {
		name         string
		preReq       func(*Reconciler, *fakes.FakeCtrlClient)
		wantErr      string
		assertCalls  func(t *testing.T, mock *fakes.FakeCtrlClient)
	}{
		{
			name: "serviceaccount reconciliation successful",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *corev1.ServiceAccount:
						serviceaccount := testServiceAccount()
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
						return false, errTestClient
					}
					return false, nil
				})
			},
			wantErr: `failed to check if ServiceAccount "istiocsr-test-ns/cert-manager-istio-csr" exists: test client error`,
		},
		{
			name: "serviceaccount reconciliation fails while updating status",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.StatusUpdateCalls(func(ctx context.Context, obj client.Object, option ...client.SubResourceUpdateOption) error {
					switch obj.(type) {
					case *v1alpha1.IstioCSR:
						return errTestClient
					}
					return nil
				})
			},
			wantErr: `failed to update istiocsr-test-ns/istiocsr-test-resource istiocsr status with cert-manager-istio-csr serviceaccount resource name: failed to update status for "istiocsr-test-ns/istiocsr-test-resource": failed to update istiocsr.openshift.operator.io "istiocsr-test-ns/istiocsr-test-resource" status: test client error`,
		},
		{
			name: "serviceaccount exists but modified is applied and reconciled",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *corev1.ServiceAccount:
						serviceaccount := testServiceAccount()
						serviceaccount.SetLabels(map[string]string{"modified": "label"})
						serviceaccount.DeepCopyInto(o)
					}
					return true, nil
				})
			},
			assertCalls: func(t *testing.T, mock *fakes.FakeCtrlClient) {
				if mock.PatchCallCount() != 1 {
					t.Errorf("createOrApplyServiceAccounts() Patch call count: %d, want 1", mock.PatchCallCount())
				}
			},
		},
		{
			name: "serviceaccount reconciliation fails while applying modified serviceaccount",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *corev1.ServiceAccount:
						serviceaccount := testServiceAccount()
						serviceaccount.SetLabels(map[string]string{"modified": "label"})
						serviceaccount.DeepCopyInto(o)
					}
					return true, nil
				})
				m.PatchReturns(errTestClient)
			},
			wantErr: `failed to apply ServiceAccount "istiocsr-test-ns/cert-manager-istio-csr": test client error`,
		},
		{
			name: "serviceaccount created when it does not exist",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch obj.(type) {
					case *corev1.ServiceAccount:
						return false, nil
					}
					return false, nil
				})
			},
			assertCalls: func(t *testing.T, mock *fakes.FakeCtrlClient) {
				if mock.PatchCallCount() != 1 {
					t.Errorf("createOrApplyServiceAccounts() Patch call count: %d, want 1", mock.PatchCallCount())
				}
			},
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
			err := r.createOrApplyServiceAccounts(istiocsr, controllerDefaultResourceLabels)
			if (tt.wantErr != "" || err != nil) && (err == nil || err.Error() != tt.wantErr) {
				t.Errorf("createOrApplyServiceAccounts() err: %v, wantErr: %v", err, tt.wantErr)
			}
			if tt.wantErr == "" {
				if istiocsr.Status.ServiceAccount != serviceAccountName {
					t.Errorf("createOrApplyServiceAccounts() got: %v, want: %s", istiocsr.Status.ServiceAccount, serviceAccountName)
				}
			}
			if tt.assertCalls != nil {
				tt.assertCalls(t, mock)
			}
		})
	}
}
