package approverpolicy

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

func TestCleanupWaitsForAbsenceAndRetries(t *testing.T) {
	instance := testInstance()
	instance.Finalizers = []string{finalizer, "example.test/other"}
	now := metav1.Now()
	instance.DeletionTimestamp = &now
	r, c := testReconciler(t, instance)
	objects := map[string]client.Object{}
	for i, obj := range desiredResources(t, instance) {
		obj.SetUID(types.UID("resource-" + string(rune('a'+i))))
		objects[objectID(obj)] = obj
	}
	primaryGet := c.GetStub
	c.GetCalls(func(ctx context.Context, key client.ObjectKey, target client.Object) error {
		if _, primary := target.(*v1alpha1.ApproverPolicyManager); primary {
			return primaryGet(ctx, key, target)
		}
		target.SetName(key.Name)
		target.SetNamespace(key.Namespace)
		if obj, ok := objects[objectID(target)]; ok {
			copyObject(target, obj)
			return nil
		}
		return apierrors.NewNotFound(v1alpha1.Resource("managed"), key.Name)
	})
	failDelete := true
	c.DeleteCalls(func(_ context.Context, obj client.Object, opts ...client.DeleteOption) error {
		options := &client.DeleteOptions{}
		options.ApplyOptions(opts)
		require.NotNil(t, options.Preconditions)
		assert.Equal(t, obj.GetUID(), *options.Preconditions.UID)
		assert.Equal(t, metav1.DeletePropagationForeground, *options.PropagationPolicy)
		if _, deployment := obj.(*appsv1.Deployment); deployment {
			// Simulate asynchronous foreground garbage collection.
			objects[objectID(obj)].SetDeletionTimestamp(&now)
			return nil
		}
		if failDelete && obj.GetName() == tlsSecretName {
			return apierrors.NewForbidden(v1alpha1.Resource("secrets"), obj.GetName(), errors.New("temporarily denied"))
		}
		delete(objects, objectID(obj))
		return nil
	})
	for i := 0; i < 2; i++ {
		result, err := r.Reconcile(t.Context(), singletonRequest())
		require.NoError(t, err)
		assert.Equal(t, defaultRequeueTime, result.RequeueAfter)
		assert.Contains(t, instance.Finalizers, finalizer)
		assert.Len(t, objects, 10)
	}
	assert.Equal(t, 1, c.DeleteCallCount(), "do not repeatedly delete a terminating workload")
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: operandName, Namespace: operandNamespace}}
	delete(objects, objectID(deployment)) // The garbage collector has finished.
	result, err := r.Reconcile(t.Context(), singletonRequest())
	require.NoError(t, err)
	assert.Equal(t, defaultRequeueTime, result.RequeueAfter)
	assert.Contains(t, instance.Finalizers, finalizer)
	assert.Len(t, objects, 1)
	failDelete = false
	_, err = r.Reconcile(t.Context(), singletonRequest())
	require.NoError(t, err)
	assert.Contains(t, instance.Finalizers, finalizer, "DELETE acceptance still requires a later absence check")
	_, err = r.Reconcile(t.Context(), singletonRequest())
	require.NoError(t, err)
	assert.Equal(t, []string{"example.test/other"}, instance.Finalizers)
	assert.Empty(t, objects)
}

func TestCleanupBypassesFilteredCache(t *testing.T) {
	instance := testInstance()
	r, c := testReconciler(t, instance)
	existing, err := getDeploymentObject(instance, "")
	require.NoError(t, err)
	existing.UID = "deployment-uid"
	existing.Labels = nil
	r.apiReader = &testAPIReader{objects: []client.Object{existing}}
	c.GetReturns(apierrors.NewNotFound(v1alpha1.Resource("deployments"), operandName))
	pending, err := r.cleanUp(t.Context(), instance)
	require.NoError(t, err)
	assert.True(t, pending, "removing the managed label cannot hide an existing Deployment")
	assert.Zero(t, c.GetCallCount())
	require.Equal(t, 1, c.DeleteCallCount())
	_, obj, _ := c.DeleteArgsForCall(0)
	assert.IsType(t, &appsv1.Deployment{}, obj)
}

func TestCleanupRetriesWhenAPIReadFails(t *testing.T) {
	instance := testInstance()
	r, c := testReconciler(t, instance)
	readErr := apierrors.NewTimeoutError("API read timed out", 1)
	r.apiReader = &testAPIReader{getErr: readErr}
	c.GetReturns(apierrors.NewNotFound(v1alpha1.Resource("deployments"), operandName))
	pending, err := r.cleanUp(t.Context(), instance)
	require.ErrorIs(t, err, readErr)
	assert.True(t, pending)
	assert.Zero(t, c.GetCallCount(), "cleanup must not use cache absence to hide an API read failure")
	assert.Zero(t, c.DeleteCallCount())
}

func TestCleanupLeavesForeignReplacement(t *testing.T) {
	instance := testInstance()
	r, c := testReconciler(t, instance)
	foreign := getSecretObject(instance)
	foreign.OwnerReferences[0].UID = "another-instance"
	r.apiReader = &testAPIReader{objects: []client.Object{foreign}}
	pending, err := r.cleanUp(t.Context(), instance)
	require.NoError(t, err)
	assert.False(t, pending)
	assert.Zero(t, c.DeleteCallCount())
}

type testAPIReader struct {
	client.Reader
	objects  []client.Object
	getErr   error
	getCalls int
}

func (r *testAPIReader) Get(_ context.Context, key client.ObjectKey, target client.Object, _ ...client.GetOption) error {
	r.getCalls++
	if r.getErr != nil {
		return r.getErr
	}
	target.SetName(key.Name)
	target.SetNamespace(key.Namespace)
	for _, obj := range r.objects {
		if objectID(obj) == objectID(target) {
			copyObject(target, obj)
			return nil
		}
	}
	return apierrors.NewNotFound(v1alpha1.Resource("managed"), key.Name)
}
