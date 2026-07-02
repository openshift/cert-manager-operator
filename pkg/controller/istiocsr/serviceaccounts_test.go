package istiocsr

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common/fakes"
)

const (
	serviceAccountName = "cert-manager-istio-csr"
)

func TestCreateOrApplyServiceAccounts(t *testing.T) {
	tests := []struct {
		name                string
		preReq              func(*Reconciler, *fakes.FakeCtrlClient)
		istioCSRCreateRecon bool
		wantErr             string
		assertEvents        func(t *testing.T, r *Reconciler)
		assertCalls         func(t *testing.T, mock *fakes.FakeCtrlClient)
	}{
		{
			name: "serviceaccount reconciliation successful",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) (bool, error) {
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
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) (bool, error) {
					switch obj.(type) {
					case *corev1.ServiceAccount:
						return false, errTestClient
					}
					return false, nil
				})
			},
			wantErr: `failed to check istiocsr-test-ns/cert-manager-istio-csr serviceaccount resource already exists: test client error`,
		},
		{
			name: "serviceaccount reconciliation fails while updating status",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.StatusUpdateCalls(func(_ context.Context, obj client.Object, _ ...client.SubResourceUpdateOption) error {
					switch obj.(type) {
					case *v1alpha1.IstioCSR:
						return errTestClient
					}
					return nil
				})
			},
			wantErr: `failed to update istiocsr-test-ns/istiocsr-test-resource istiocsr status with istiocsr-test-ns/cert-manager-istio-csr serviceaccount resource name: failed to update status for "istiocsr-test-ns/istiocsr-test-resource": failed to update istiocsr.openshift.operator.io "istiocsr-test-ns/istiocsr-test-resource" status: test client error`,
		},
		{
			name:                "serviceaccount already exists with istioCSRCreateRecon true records ResourceAlreadyExists event",
			istioCSRCreateRecon: true,
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *corev1.ServiceAccount:
						serviceaccount := testServiceAccount()
						serviceaccount.DeepCopyInto(o)
					}
					return true, nil
				})
			},
			assertEvents: func(t *testing.T, r *Reconciler) {
				rec := r.eventRecorder.(*record.FakeRecorder)
				select {
				case evt := <-rec.Events:
					if !strings.Contains(evt, "ResourceAlreadyExists") || !strings.Contains(evt, "serviceaccount resource already exists") {
						t.Errorf("createOrApplyServiceAccounts() event: %q, want ResourceAlreadyExists and serviceaccount already exists", evt)
					}
				case <-time.After(time.Second):
					t.Error("expected ResourceAlreadyExists event but none received")
				}
			},
		},
		{
			name: "serviceaccount exists but modified is updated and reconciled",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *corev1.ServiceAccount:
						serviceaccount := testServiceAccount()
						serviceaccount.SetLabels(map[string]string{"modified": "label"})
						serviceaccount.DeepCopyInto(o)
					}
					return true, nil
				})
				m.UpdateWithRetryReturns(nil)
			},
			assertEvents: func(t *testing.T, r *Reconciler) {
				rec := r.eventRecorder.(*record.FakeRecorder)
				select {
				case evt := <-rec.Events:
					if !strings.Contains(evt, "Reconciled") || !strings.Contains(evt, "reconciled back to desired state") {
						t.Errorf("createOrApplyServiceAccounts() event: %q, want Reconciled and reconciled back to desired state", evt)
					}
				case <-time.After(time.Second):
					t.Error("expected Reconciled event but none received")
				}
			},
			assertCalls: func(t *testing.T, mock *fakes.FakeCtrlClient) {
				if mock.UpdateWithRetryCallCount() != 1 {
					t.Errorf("createOrApplyServiceAccounts() UpdateWithRetry call count: %d, want 1", mock.UpdateWithRetryCallCount())
				}
			},
		},
		{
			name: "serviceaccount reconciliation fails while updating modified serviceaccount",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *corev1.ServiceAccount:
						serviceaccount := testServiceAccount()
						serviceaccount.SetLabels(map[string]string{"modified": "label"})
						serviceaccount.DeepCopyInto(o)
					}
					return true, nil
				})
				m.UpdateWithRetryCalls(func(_ context.Context, obj client.Object, _ ...client.UpdateOption) error {
					switch obj.(type) {
					case *corev1.ServiceAccount:
						return errTestClient
					}
					return nil
				})
			},
			wantErr: `failed to update istiocsr-test-ns/cert-manager-istio-csr serviceaccount resource: test client error`,
		},
		{
			name: "serviceaccount created when it does not exist",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) (bool, error) {
					switch obj.(type) {
					case *corev1.ServiceAccount:
						return false, nil
					}
					return false, nil
				})
			},
			assertCalls: func(t *testing.T, mock *fakes.FakeCtrlClient) {
				if mock.CreateCallCount() != 1 {
					t.Errorf("createOrApplyServiceAccounts() Create call count: %d, want 1", mock.CreateCallCount())
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
			err := r.createOrApplyServiceAccounts(istiocsr, controllerDefaultResourceLabels, tt.istioCSRCreateRecon)
			if (tt.wantErr != "" || err != nil) && (err == nil || err.Error() != tt.wantErr) {
				t.Errorf("createOrApplyServiceAccounts() err: %v, wantErr: %v", err, tt.wantErr)
			}
			if tt.wantErr == "" {
				if istiocsr.Status.ServiceAccount != serviceAccountName {
					t.Errorf("createOrApplyServiceAccounts() got: %v, want: %s", istiocsr.Status.ServiceAccount, serviceAccountName)
				}
			}
			if tt.assertEvents != nil {
				tt.assertEvents(t, r)
			}
			if tt.assertCalls != nil {
				tt.assertCalls(t, mock)
			}
		})
	}
}
