package trustmanager

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common/fakes"
)

func TestReconcile(t *testing.T) {
	tests := []struct {
		name    string
		preReq  func(*Reconciler, *fakes.FakeCtrlClient)
		wantErr string
	}{
		{
			name: "resource not found returns no error",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
					return apierrors.NewNotFound(v1alpha1.Resource("trustmanager"), trustManagerObjectName)
				})
			},
		},
		{
			name: "failed to fetch resource propagates error",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
					switch obj.(type) {
					case *v1alpha1.TrustManager:
						return apierrors.NewBadRequest("test error")
					}
					return nil
				})
			},
			wantErr: "failed to fetch trustmanager.openshift.operator.io",
		},
		{
			name: "resource marked for deletion without finalizer",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
					switch o := obj.(type) {
					case *v1alpha1.TrustManager:
						tm := testTrustManager().Build()
						tm.DeletionTimestamp = &metav1.Time{Time: time.Now()}
						tm.DeepCopyInto(o)
					}
					return nil
				})
			},
		},
		{
			name: "remove finalizer fails on deletion",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
					switch o := obj.(type) {
					case *v1alpha1.TrustManager:
						tm := testTrustManager().Build()
						tm.DeletionTimestamp = &metav1.Time{Time: time.Now()}
						tm.Finalizers = []string{finalizer}
						tm.DeepCopyInto(o)
					}
					return nil
				})
				m.UpdateWithRetryCalls(func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					return errTestClient
				})
			},
			wantErr: "failed to remove finalizers",
		},
		{
			name: "adding finalizer fails",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
					switch o := obj.(type) {
					case *v1alpha1.TrustManager:
						tm := testTrustManager().Build()
						tm.DeepCopyInto(o)
					}
					return nil
				})
				m.UpdateWithRetryCalls(func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					return errTestClient
				})
			},
			wantErr: `failed to update "/cluster" trustmanager.openshift.operator.io with finalizers`,
		},
		{
			name: "status update failure",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
					switch o := obj.(type) {
					case *v1alpha1.TrustManager:
						tm := testTrustManager().Build()
						tm.Spec.TrustManagerConfig = v1alpha1.TrustManagerConfig{}
						tm.Finalizers = []string{finalizer}
						tm.DeepCopyInto(o)
					}
					return nil
				})
				m.StatusUpdateCalls(func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
					return apierrors.NewBadRequest("test error")
				})
			},
			wantErr: "failed to update cluster status",
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

			_, err := r.Reconcile(context.Background(),
				ctrl.Request{
					NamespacedName: types.NamespacedName{Name: trustManagerObjectName},
				},
			)
			assertError(t, err, tt.wantErr)
		})
	}
}

func TestProcessReconcileRequest(t *testing.T) {
	t.Setenv(trustManagerImageNameEnvVarName, testImage)

	tests := []struct {
		name            string
		getTrustManager func() *v1alpha1.TrustManager
		preReq          func(*Reconciler, *fakes.FakeCtrlClient)
		wantConditions  []metav1.Condition
		wantErr         string
	}{
		{
			name: "successful reconciliation sets ready true",
			getTrustManager: func() *v1alpha1.TrustManager {
				return testTrustManager().Build()
			},
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
					switch o := obj.(type) {
					case *v1alpha1.TrustManager:
						testTrustManager().Build().DeepCopyInto(o)
					}
					return nil
				})
				// Namespace exists; all other resources return not-found so they
				// are created via SSA Patch (which succeeds by default).
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					switch obj.(type) {
					case *corev1.Namespace:
						return true, nil
					}
					return false, nil
				})
			},
			wantConditions: []metav1.Condition{
				{
					Type:   v1alpha1.Degraded,
					Status: metav1.ConditionFalse,
					Reason: v1alpha1.ReasonReady,
				},
				{
					Type:    v1alpha1.Ready,
					Status:  metav1.ConditionTrue,
					Reason:  v1alpha1.ReasonReady,
					Message: "reconciliation successful",
				},
			},
		},
		{
			name: "irrecoverable error sets degraded true",
			getTrustManager: func() *v1alpha1.TrustManager {
				// Empty TrustManagerConfig triggers validateTrustManagerConfig failure,
				// which is wrapped as an irrecoverable error in reconcileTrustManagerDeployment.
				tm := testTrustManager().Build()
				tm.Spec.TrustManagerConfig = v1alpha1.TrustManagerConfig{}
				return tm
			},
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
					switch o := obj.(type) {
					case *v1alpha1.TrustManager:
						tm := testTrustManager().Build()
						tm.Spec.TrustManagerConfig = v1alpha1.TrustManagerConfig{}
						tm.DeepCopyInto(o)
					}
					return nil
				})
			},
			wantConditions: []metav1.Condition{
				{
					Type:   v1alpha1.Degraded,
					Status: metav1.ConditionTrue,
					Reason: v1alpha1.ReasonFailed,
				},
				{
					Type:   v1alpha1.Ready,
					Status: metav1.ConditionFalse,
					Reason: v1alpha1.ReasonFailed,
				},
			},
		},
		{
			name: "recoverable error sets in progress",
			getTrustManager: func() *v1alpha1.TrustManager {
				return testTrustManager().Build()
			},
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
					switch o := obj.(type) {
					case *v1alpha1.TrustManager:
						testTrustManager().Build().DeepCopyInto(o)
					}
					return nil
				})
				// Namespace Exists succeeds (passes validateTrustNamespace), but
				// ServiceAccount Exists returns a FromClientError
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					switch obj.(type) {
					case *corev1.Namespace:
						return true, nil
					}
					return false, errTestClient
				})
			},
			wantConditions: []metav1.Condition{
				{
					Type:   v1alpha1.Degraded,
					Status: metav1.ConditionFalse,
					Reason: v1alpha1.ReasonReady,
				},
				{
					Type:   v1alpha1.Ready,
					Status: metav1.ConditionFalse,
					Reason: v1alpha1.ReasonInProgress,
				},
			},
			wantErr: "failed to check if serviceaccount",
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

			tm := tt.getTrustManager()
			_, err := r.processReconcileRequest(tm, types.NamespacedName{Name: tm.GetName()})
			assertError(t, err, tt.wantErr)

			for _, want := range tt.wantConditions {
				found := false
				for _, got := range tm.Status.Conditions {
					if got.Type == want.Type {
						found = true
						if got.Status != want.Status {
							t.Errorf("condition %s: expected status %s, got %s", want.Type, want.Status, got.Status)
						}
						if got.Reason != want.Reason {
							t.Errorf("condition %s: expected reason %s, got %s", want.Type, want.Reason, got.Reason)
						}
						if want.Message != "" && got.Message != want.Message {
							t.Errorf("condition %s: expected message %q, got %q", want.Type, want.Message, got.Message)
						}
					}
				}
				if !found {
					t.Errorf("expected condition %s not found in status conditions %v", want.Type, tm.Status.Conditions)
				}
			}
		})
	}
}
