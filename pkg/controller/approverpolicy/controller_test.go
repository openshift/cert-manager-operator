package approverpolicy

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/controller/common/fakes"
	"github.com/openshift/cert-manager-operator/pkg/features"
)

func setFeatureGate(t *testing.T, enabled bool) {
	t.Helper()
	old := features.IsApproverPolicyManagerFeatureGateEnabled()
	require.NoError(t, features.SetupWithFlagValue(fmt.Sprintf("ApproverPolicyManager=%t", enabled)))
	t.Cleanup(func() {
		require.NoError(t, features.SetupWithFlagValue(fmt.Sprintf("ApproverPolicyManager=%t", old)))
	})
}

func testInstance() *v1alpha1.ApproverPolicyManager {
	return &v1alpha1.ApproverPolicyManager{ObjectMeta: metav1.ObjectMeta{
		Name: approverPolicyManagerObjectName, UID: "singleton-uid", Generation: 1,
	}}
}

// testReconciler keeps a stored object separate from every read and write so
// idempotency and fresh-object retry tests exercise persistence semantics.
func testReconciler(t *testing.T, stored *v1alpha1.ApproverPolicyManager) (*Reconciler, *fakes.FakeCtrlClient) {
	t.Helper()
	c := &fakes.FakeCtrlClient{}
	c.GetCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		require.Equal(t, client.ObjectKey{Name: approverPolicyManagerObjectName}, key)
		if stored == nil {
			return apierrors.NewNotFound(v1alpha1.Resource("approverpolicymanagers"), key.Name)
		}
		stored.DeepCopyInto(obj.(*v1alpha1.ApproverPolicyManager))
		return nil
	})
	c.UpdateCalls(func(_ context.Context, obj client.Object, _ ...client.UpdateOption) error {
		obj.(*v1alpha1.ApproverPolicyManager).DeepCopyInto(stored)
		return nil
	})
	c.StatusUpdateCalls(func(_ context.Context, obj client.Object, _ ...client.SubResourceUpdateOption) error {
		obj.(*v1alpha1.ApproverPolicyManager).Status.DeepCopyInto(&stored.Status)
		return nil
	})
	return &Reconciler{CtrlClient: c, log: logr.Discard()}, c
}

func singletonRequest() ctrl.Request {
	return ctrl.Request{NamespacedName: types.NamespacedName{Name: approverPolicyManagerObjectName}}
}

func assertCondition(t *testing.T, instance *v1alpha1.ApproverPolicyManager, typ string, status metav1.ConditionStatus, reason string) {
	t.Helper()
	condition := meta.FindStatusCondition(instance.Status.Conditions, typ)
	require.NotNil(t, condition)
	assert.Equal(t, status, condition.Status)
	assert.Equal(t, reason, condition.Reason)
	assert.Equal(t, instance.Generation, condition.ObservedGeneration)
}

func TestReconcileIgnoresDisabledAndNonSingleton(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled bool
		key     types.NamespacedName
	}{
		{"disabled", false, types.NamespacedName{Name: "cluster"}},
		{"wrong name", true, types.NamespacedName{Name: "other"}},
		{"unexpected namespace", true, types.NamespacedName{Name: "cluster", Namespace: "cert-manager"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setFeatureGate(t, tc.enabled)
			r, c := testReconciler(t, testInstance())
			result, err := r.Reconcile(t.Context(), ctrl.Request{NamespacedName: tc.key})
			require.NoError(t, err)
			assert.Zero(t, result)
			assert.Empty(t, c.Invocations(), "disabled or invalid requests must perform no client I/O")
		})
	}
}

func TestReconcileLifecycle(t *testing.T) {
	setFeatureGate(t, true)
	for _, tc := range []struct {
		name           string
		prepare        func(*v1alpha1.ApproverPolicyManager)
		deleting       bool
		wantFinalizers []string
	}{
		{"new singleton", func(*v1alpha1.ApproverPolicyManager) {}, false, []string{finalizer}},
		{"foreign finalizer", func(o *v1alpha1.ApproverPolicyManager) { o.Finalizers = []string{"other.example/finalizer"} }, false, []string{"other.example/finalizer", finalizer}},
		{"existing finalizer", func(o *v1alpha1.ApproverPolicyManager) { o.Finalizers = []string{finalizer} }, false, []string{finalizer}},
		{"deleting", func(o *v1alpha1.ApproverPolicyManager) { o.Finalizers = []string{finalizer} }, true, []string{}},
		{"deleting with foreign finalizer", func(o *v1alpha1.ApproverPolicyManager) { o.Finalizers = []string{finalizer, "other.example/finalizer"} }, true, []string{"other.example/finalizer"}},
		{"deleting without our finalizer", func(*v1alpha1.ApproverPolicyManager) {}, true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stored := testInstance()
			tc.prepare(stored)
			if tc.deleting {
				now := metav1.Now()
				stored.DeletionTimestamp = &now
			}
			r, c := testReconciler(t, stored)
			result, err := r.Reconcile(t.Context(), singletonRequest())
			require.NoError(t, err)
			assert.Equal(t, tc.wantFinalizers, stored.Finalizers)
			if tc.deleting {
				assert.Zero(t, result)
				assert.Zero(t, c.StatusUpdateCallCount())
			} else {
				assert.Equal(t, defaultRequeueTime, result.RequeueAfter)
				assertCondition(t, stored, v1alpha1.Ready, metav1.ConditionFalse, v1alpha1.ReasonInProgress)
				assertCondition(t, stored, v1alpha1.Degraded, metav1.ConditionFalse, v1alpha1.ReasonReady)
				assert.Contains(t, stored.Status.GetCondition(v1alpha1.Ready).Message, "installation is pending")
			}
			updates, statuses := c.UpdateCallCount(), c.StatusUpdateCallCount()
			_, err = r.Reconcile(t.Context(), singletonRequest())
			require.NoError(t, err)
			assert.Equal(t, updates, c.UpdateCallCount())
			assert.Equal(t, statuses, c.StatusUpdateCallCount())
			assert.Zero(t, c.CreateCallCount())
			assert.Zero(t, c.PatchCallCount())
			assert.Zero(t, c.DeleteCallCount())
		})
	}
}

func TestReconcileReadErrors(t *testing.T) {
	setFeatureGate(t, true)
	t.Run("not found", func(t *testing.T) {
		r, c := testReconciler(t, nil)
		result, err := r.Reconcile(t.Context(), singletonRequest())
		require.NoError(t, err)
		assert.Zero(t, result)
		assert.Zero(t, c.UpdateCallCount())
	})
	t.Run("read failed", func(t *testing.T) {
		r, c := testReconciler(t, testInstance())
		failure := errors.New("read failed")
		c.GetReturns(failure)
		_, err := r.Reconcile(t.Context(), singletonRequest())
		require.ErrorIs(t, err, failure)
		assert.Zero(t, c.UpdateCallCount())
	})
	t.Run("canceled context", func(t *testing.T) {
		r, c := testReconciler(t, testInstance())
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, err := r.Reconcile(ctx, singletonRequest())
		require.ErrorIs(t, err, context.Canceled)
		assert.Zero(t, c.UpdateCallCount())
	})
}

func TestReconcileFinalizerFailures(t *testing.T) {
	setFeatureGate(t, true)
	for _, deleting := range []bool{false, true} {
		for _, permanent := range []bool{false, true} {
			t.Run(fmt.Sprintf("deleting=%t/permanent=%t", deleting, permanent), func(t *testing.T) {
				stored := testInstance()
				if deleting {
					now := metav1.Now()
					stored.DeletionTimestamp = &now
					stored.Finalizers = []string{finalizer}
				}
				r, c := testReconciler(t, stored)
				failure := errors.New("temporary failure")
				if permanent {
					failure = apierrors.NewForbidden(v1alpha1.Resource("approverpolicymanagers"), "cluster", failure)
				}
				c.UpdateReturns(failure)
				result, err := r.Reconcile(t.Context(), singletonRequest())
				require.NoError(t, err)
				if permanent {
					assert.Zero(t, result)
					assertCondition(t, stored, v1alpha1.Degraded, metav1.ConditionTrue, v1alpha1.ReasonFailed)
					assertCondition(t, stored, v1alpha1.Ready, metav1.ConditionFalse, v1alpha1.ReasonFailed)
				} else {
					assert.Equal(t, defaultRequeueTime, result.RequeueAfter)
					assertCondition(t, stored, v1alpha1.Degraded, metav1.ConditionFalse, v1alpha1.ReasonReady)
					assertCondition(t, stored, v1alpha1.Ready, metav1.ConditionFalse, v1alpha1.ReasonInProgress)
				}
				if deleting {
					assert.Contains(t, stored.Finalizers, finalizer)
				}
			})
		}
	}
}

func TestReconcileStatusFailure(t *testing.T) {
	setFeatureGate(t, true)
	stored := testInstance()
	r, c := testReconciler(t, stored)
	failure := errors.New("status update failed")
	c.StatusUpdateReturns(failure)
	result, err := r.Reconcile(t.Context(), singletonRequest())
	require.ErrorIs(t, err, failure)
	assert.Zero(t, result, "return error or timed requeue, not both")
	assert.Contains(t, stored.Finalizers, finalizer)
}

func TestHandleReconcileResultUpdatesMessagesAndGeneration(t *testing.T) {
	stored := testInstance()
	stored.Status.ApproverPolicyImage = "preserve-image"
	stored.Status.Conditions = []metav1.Condition{{Type: "Other", Status: metav1.ConditionTrue, Reason: "OtherWriter", LastTransitionTime: metav1.Now()}}
	r, c := testReconciler(t, stored)
	for i, message := range []string{"first failure", "changed failure"} {
		stored.Generation = int64(i + 1)
		err := common.NewRetryRequiredError(errors.New(message), "retry")
		_, updateErr := r.handleReconcileResult(t.Context(), stored.DeepCopy(), err)
		require.NoError(t, updateErr)
		assert.Contains(t, stored.Status.GetCondition(v1alpha1.Ready).Message, message)
		assertCondition(t, stored, v1alpha1.Ready, metav1.ConditionFalse, v1alpha1.ReasonInProgress)
	}
	assert.Equal(t, 2, c.StatusUpdateCallCount())
	assert.Equal(t, "preserve-image", stored.Status.ApproverPolicyImage)
	assert.NotNil(t, stored.Status.GetCondition("Other"))
	before := stored.Status.GetCondition(v1alpha1.Ready).LastTransitionTime
	_, err := r.processReconcileRequest(t.Context(), stored.DeepCopy())
	require.NoError(t, err)
	assert.Equal(t, before, stored.Status.GetCondition(v1alpha1.Ready).LastTransitionTime)
}

func TestPrimaryResourcePredicate(t *testing.T) {
	p := primaryResourcePredicate()
	for _, tc := range []struct {
		name   string
		mutate func(*v1alpha1.ApproverPolicyManager)
		want   bool
	}{
		{"status only", func(o *v1alpha1.ApproverPolicyManager) { o.Status.ApproverPolicyImage = "other" }, false},
		{"resourceVersion only", func(o *v1alpha1.ApproverPolicyManager) { o.ResourceVersion = "2" }, false},
		{"generation", func(o *v1alpha1.ApproverPolicyManager) { o.Generation++ }, true},
		{"labels", func(o *v1alpha1.ApproverPolicyManager) { o.Labels = map[string]string{"x": "y"} }, true},
		{"annotations", func(o *v1alpha1.ApproverPolicyManager) { o.Annotations = map[string]string{"x": "y"} }, true},
		{"finalizer", func(o *v1alpha1.ApproverPolicyManager) { o.Finalizers = []string{finalizer} }, true},
		{"deletion", func(o *v1alpha1.ApproverPolicyManager) { now := metav1.Now(); o.DeletionTimestamp = &now }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := testInstance()
			updated := old.DeepCopy()
			tc.mutate(updated)
			assert.Equal(t, tc.want, p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: updated}))
		})
	}
	assert.True(t, p.Create(event.CreateEvent{Object: testInstance()}))
	assert.True(t, p.Delete(event.DeleteEvent{Object: testInstance()}))
	assert.True(t, p.Generic(event.GenericEvent{Object: testInstance()}))
	assert.False(t, p.Update(event.UpdateEvent{ObjectNew: testInstance()}))
	assert.False(t, p.Update(event.UpdateEvent{ObjectOld: testInstance()}))
}

func TestManagedResourcePredicate(t *testing.T) {
	r := &Reconciler{log: logr.Discard()}
	p := r.managedResourcePredicate()
	managed := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{common.ManagedResourceLabelKey: RequestEnqueueLabelValue}}}
	unmanaged := &appsv1.Deployment{}
	foreign := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{common.ManagedResourceLabelKey: "cert-manager-trust-manager"}}}
	for _, obj := range []client.Object{managed, unmanaged, foreign, nil} {
		want := obj == managed
		assert.Equal(t, want, p.Create(event.CreateEvent{Object: obj}))
		assert.Equal(t, want, p.Delete(event.DeleteEvent{Object: obj}))
		assert.Equal(t, want, p.Generic(event.GenericEvent{Object: obj}))
	}
	assert.True(t, p.Update(event.UpdateEvent{ObjectOld: managed, ObjectNew: unmanaged}), "label removal")
	assert.True(t, p.Update(event.UpdateEvent{ObjectOld: unmanaged, ObjectNew: managed}), "label addition")
	assert.False(t, p.Update(event.UpdateEvent{ObjectOld: unmanaged, ObjectNew: foreign}))
	secret := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Labels: managed.Labels}}
	assert.True(t, p.Create(event.CreateEvent{Object: secret}), "metadata-only Secret events")
}

func TestDeploymentPredicate(t *testing.T) {
	r := &Reconciler{log: logr.Discard()}
	p := r.deploymentPredicate()
	managed := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
		Name: "approver-policy", Namespace: "cert-manager", Generation: 1,
		Labels: map[string]string{common.ManagedResourceLabelKey: RequestEnqueueLabelValue},
	}}
	for _, tc := range []struct {
		name   string
		mutate func(*appsv1.Deployment)
		want   bool
	}{
		{"unchanged", func(*appsv1.Deployment) {}, false},
		{"status only", func(o *appsv1.Deployment) { o.Status.ReadyReplicas = 1; o.Status.AvailableReplicas = 1 }, false},
		{"resourceVersion only", func(o *appsv1.Deployment) { o.ResourceVersion = "2" }, false},
		{"spec generation", func(o *appsv1.Deployment) { o.Generation++ }, true},
		{"labels", func(o *appsv1.Deployment) { o.Labels["x"] = "y" }, true},
		{"annotations", func(o *appsv1.Deployment) { o.Annotations = map[string]string{"x": "y"} }, true},
		{"managed label removed", func(o *appsv1.Deployment) { o.Labels = nil }, true},
		{"managed label replaced", func(o *appsv1.Deployment) { o.Labels[common.ManagedResourceLabelKey] = "cert-manager-trust-manager" }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			updated := managed.DeepCopy()
			tc.mutate(updated)
			assert.Equal(t, tc.want, p.Update(event.UpdateEvent{ObjectOld: managed, ObjectNew: updated}))
		})
	}
	unmanaged := managed.DeepCopy()
	unmanaged.Labels = nil
	assert.True(t, p.Update(event.UpdateEvent{ObjectOld: unmanaged, ObjectNew: managed}), "managed label added")
	updated := unmanaged.DeepCopy()
	updated.Generation++
	assert.False(t, p.Update(event.UpdateEvent{ObjectOld: unmanaged, ObjectNew: updated}), "unrelated spec update")
	assert.False(t, p.Update(event.UpdateEvent{ObjectNew: managed}))
	assert.False(t, p.Update(event.UpdateEvent{ObjectOld: managed}))
	for _, obj := range []client.Object{managed, unmanaged} {
		assert.Equal(t, obj == managed, p.Create(event.CreateEvent{Object: obj}))
		assert.Equal(t, obj == managed, p.Delete(event.DeleteEvent{Object: obj}))
		assert.Equal(t, obj == managed, p.Generic(event.GenericEvent{Object: obj}))
	}
}

// registrationManager captures registration without starting informers or a
// Kubernetes API server. Unused manager methods deliberately remain unavailable.
type registrationManager struct {
	ctrl.Manager
	scheme     *runtime.Scheme
	client     client.Client
	registered []manager.Runnable
	addErr     error
}

func (m *registrationManager) GetScheme() *runtime.Scheme { return m.scheme }
func (m *registrationManager) GetClient() client.Client   { return m.client }
func (m *registrationManager) GetCache() cache.Cache      { return nil }
func (m *registrationManager) GetLogger() logr.Logger     { return logr.Discard() }
func (m *registrationManager) GetControllerOptions() config.Controller {
	skip := true
	return config.Controller{SkipNameValidation: &skip}
}
func (m *registrationManager) Add(r manager.Runnable) error {
	if m.addErr != nil {
		return m.addErr
	}
	m.registered = append(m.registered, r)
	return nil
}

func TestSetupWithManager(t *testing.T) {
	t.Run("disabled skips registration", func(t *testing.T) {
		setFeatureGate(t, false)
		r := &Reconciler{}
		require.NoError(t, r.SetupWithManager(nil))
	})
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("enabled/add failure=%t", fail), func(t *testing.T) {
			setFeatureGate(t, true)
			scheme := runtime.NewScheme()
			require.NoError(t, clientscheme.AddToScheme(scheme))
			require.NoError(t, v1alpha1.AddToScheme(scheme))
			m := &registrationManager{scheme: scheme, client: nil}
			if fail {
				m.addErr = errors.New("registration failed")
			}
			r, err := New(m)
			require.NoError(t, err)
			err = r.SetupWithManager(m)
			if fail {
				require.ErrorIs(t, err, m.addErr)
				assert.Empty(t, m.registered)
			} else {
				require.NoError(t, err)
				assert.Len(t, m.registered, 1)
			}
		})
	}
}
