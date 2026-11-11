package trustmanager

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

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
				m.UpdateCalls(func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
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
				m.UpdateCalls(func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
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

func TestReconcileSkipsInstallationWhenDeletionStartsDuringFinalizerUpdate(t *testing.T) {
	r := testReconciler(t)
	c := &fakes.FakeCtrlClient{}
	r.CtrlClient = c
	stored := testTrustManager().Build()
	stored.UID = "original"
	stored.Finalizers = []string{"example.test/other"}
	c.GetCalls(func(_ context.Context, _ client.ObjectKey, obj client.Object) error {
		stored.DeepCopyInto(obj.(*v1alpha1.TrustManager))
		if c.GetCallCount() == 1 {
			// Deletion starts between the initial read and the finalizer read.
			now := metav1.Now()
			stored.DeletionTimestamp = &now
		}
		return nil
	})

	result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKey{Name: trustManagerObjectName}})
	require.NoError(t, err)
	assert.Equal(t, defaultRequeueTime, result.RequeueAfter)
	assert.Equal(t, []string{"example.test/other"}, stored.Finalizers)
	assert.Zero(t, c.UpdateCallCount())
	assert.Zero(t, c.ExistsCallCount(), "do not begin reconciling operand resources")
	assert.Zero(t, c.PatchCallCount())
	assert.Zero(t, c.StatusUpdateCallCount())
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
		},
		{
			name: "trust namespace does not exist sets degraded true",
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
				// Namespace does not exist - validateTrustNamespace will fail
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					switch obj.(type) {
					case *corev1.Namespace:
						return false, nil
					}
					return false, nil
				})
			},
			wantConditions: []metav1.Condition{
				{
					Type:   v1alpha1.Degraded,
					Status: metav1.ConditionTrue,
					Reason: v1alpha1.ReasonFailed,
					Message: fmt.Sprintf(
						"reconciliation failed with irrecoverable error not retrying: trust namespace %q validation failed: trust namespace %q does not exist, create the namespace before creating TrustManager CR",
						defaultTrustNamespace,
						defaultTrustNamespace,
					),
				},
				{
					Type:   v1alpha1.Ready,
					Status: metav1.ConditionFalse,
					Reason: v1alpha1.ReasonFailed,
				},
			},
		},
		{
			name: "custom trust namespace configures resources correctly",
			getTrustManager: func() *v1alpha1.TrustManager {
				tm := testTrustManager().Build()
				tm.Spec.TrustManagerConfig.TrustNamespace = "custom-trust-ns"
				return tm
			},
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
					switch o := obj.(type) {
					case *v1alpha1.TrustManager:
						tm := testTrustManager().Build()
						tm.Spec.TrustManagerConfig.TrustNamespace = "custom-trust-ns"
						tm.DeepCopyInto(o)
					}
					return nil
				})
				// Custom namespace exists; so SSA Patch will create or update all resources successfully.
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

func TestCleanUp(t *testing.T) {
	tests := []struct {
		name         string
		trustManager *v1alpha1.TrustManager
		wantRequeue  bool
		wantErr      bool
	}{
		{
			name: "returns false and nil",
			trustManager: &v1alpha1.TrustManager{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			},
			wantRequeue: false,
			wantErr:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Reconciler{eventRecorder: record.NewFakeRecorder(10)}
			requeue, err := r.cleanUp(tt.trustManager)
			if (err != nil) != tt.wantErr {
				t.Errorf("cleanUp() error = %v, wantErr %v", err, tt.wantErr)
			}
			if requeue != tt.wantRequeue {
				t.Errorf("cleanUp() requeue = %v, want %v", requeue, tt.wantRequeue)
			}
		})
	}
}

func TestTargetNamespaceWatchPredicate(t *testing.T) {
	listed := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "app-ns"}}
	unlisted := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kube-system"}}

	newPredicate := func(t *testing.T, get func(context.Context, client.ObjectKey, client.Object) error) predicate.Funcs {
		t.Helper()
		r := testReconciler(t)
		mock := &fakes.FakeCtrlClient{}
		mock.GetCalls(get)
		r.CtrlClient = mock
		return r.targetNamespaceWatchPredicate()
	}

	withTrustManager := func(namespaces ...string) func(context.Context, client.ObjectKey, client.Object) error {
		tm := testTrustManager().WithTargetNamespaces(namespaces...).Build()
		return func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
			if key.Name != trustManagerObjectName {
				return errTestClient
			}
			tm.DeepCopyInto(obj.(*v1alpha1.TrustManager))
			return nil
		}
	}

	p := newPredicate(t, withTrustManager("app-ns", "other-ns"))
	if !p.Create(event.CreateEvent{Object: listed}) {
		t.Fatal("create of a listed target namespace should match")
	}
	if p.Create(event.CreateEvent{Object: unlisted}) {
		t.Fatal("create of an unlisted namespace should not match")
	}
	if p.Create(event.CreateEvent{}) {
		t.Fatal("create with no object should not match")
	}
	if p.Delete(event.DeleteEvent{Object: listed}) {
		t.Fatal("delete of a listed target namespace should not match")
	}
	if p.Update(event.UpdateEvent{ObjectOld: listed, ObjectNew: listed}) {
		t.Fatal("namespace update should not match")
	}
	if p.Generic(event.GenericEvent{Object: listed}) {
		t.Fatal("generic namespace event should not match")
	}

	empty := newPredicate(t, withTrustManager())
	if empty.Create(event.CreateEvent{Object: listed}) {
		t.Fatal("create should not match when targetNamespaces is empty")
	}

	unreadable := newPredicate(t, func(context.Context, client.ObjectKey, client.Object) error {
		return errTestClient
	})
	if unreadable.Create(event.CreateEvent{Object: listed}) {
		t.Fatal("create should not match when the TrustManager cannot be read")
	}
}
