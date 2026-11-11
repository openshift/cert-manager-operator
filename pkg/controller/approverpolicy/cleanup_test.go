package approverpolicy

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

// TestCleanupWaitsForAbsenceAndRetries drives a single deleting instance
// through the full cleanup state machine. The subtests below are not
// independent: each one advances the same fake client/objects map left
// behind by the previous one, so they always run in this order.
func TestCleanupWaitsForAbsenceAndRetries(t *testing.T) {
	instance := testInstance()
	instance.Finalizers = []string{finalizer, "example.test/other"}
	now := metav1.Now()
	instance.DeletionTimestamp = &now
	r, c := testReconciler(t, instance)

	// objects models every managed resource that still exists. Reconcile's
	// view of "what's live" is this map, not the Deployment/webhook/etc.
	// builders directly, so deletions and GC can be simulated by mutating it.
	objects := map[string]client.Object{}
	for i, obj := range desiredResources(t, instance) {
		obj.SetUID(types.UID("resource-" + string(rune('a'+i))))
		objects[objectID(obj)] = obj
	}
	r.apiReader = &testAPIReader{getStub: func(_ context.Context, key client.ObjectKey, target client.Object) error {
		target.SetName(key.Name)
		target.SetNamespace(key.Namespace)
		if obj, ok := objects[objectID(target)]; ok {
			copyObject(target, obj)
			return nil
		}
		return apierrors.NewNotFound(v1alpha1.Resource("managed"), key.Name)
	}}

	// failDelete toggles whether DELETE of the TLS Secret is rejected, so the
	// "DELETE accepted but not yet absent" and "DELETE itself fails" cases can
	// both be exercised against the same resource.
	failDelete := true
	var deletedKinds []string
	c.DeleteCalls(func(_ context.Context, obj client.Object, opts ...client.DeleteOption) error {
		options := &client.DeleteOptions{}
		options.ApplyOptions(opts)
		require.NotNil(t, options.Preconditions)
		assert.Equal(t, obj.GetUID(), *options.Preconditions.UID)
		assert.Equal(t, metav1.DeletePropagationForeground, *options.PropagationPolicy)
		deletedKinds = append(deletedKinds, reflect.TypeOf(obj).Elem().Name())
		switch obj.(type) {
		case *admissionregistrationv1.ValidatingWebhookConfiguration, *appsv1.Deployment:
			// Simulate asynchronous foreground garbage collection: the DELETE
			// is accepted but the object stays present with a timestamp.
			objects[objectID(obj)].SetDeletionTimestamp(&now)
			return nil
		}
		if failDelete && obj.GetName() == tlsSecretName {
			return apierrors.NewForbidden(v1alpha1.Resource("secrets"), obj.GetName(), errors.New("temporarily denied"))
		}
		delete(objects, objectID(obj))
		return nil
	})

	t.Run("webhook removal is confirmed before deleting its backend", func(t *testing.T) {
		for range 2 {
			result, err := r.Reconcile(t.Context(), singletonRequest())
			require.NoError(t, err)
			assert.Equal(t, defaultRequeueTime, result.RequeueAfter)
			assert.Contains(t, instance.Finalizers, finalizer)
			assert.Len(t, objects, 10, "the backend must remain while the webhook is terminating")
		}
		assert.Equal(t, []string{"ValidatingWebhookConfiguration"}, deletedKinds, "do not repeatedly delete a terminating webhook")
	})

	t.Run("Services are deleted before the Deployment and its garbage collection is awaited", func(t *testing.T) {
		delete(objects, objectID(getWebhookObject(instance)))
		for range 2 {
			result, err := r.Reconcile(t.Context(), singletonRequest())
			require.NoError(t, err)
			assert.Equal(t, defaultRequeueTime, result.RequeueAfter)
			assert.Contains(t, instance.Finalizers, finalizer)
			assert.Len(t, objects, 7, "RBAC and credentials must remain while the Deployment is terminating")
		}
		assert.Equal(t, []string{"ValidatingWebhookConfiguration", "Service", "Service", "Deployment"}, deletedKinds,
			"do not repeatedly delete a terminating workload")
	})

	t.Run("the remaining resources are deleted once Deployment GC finishes", func(t *testing.T) {
		deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: operandName, Namespace: operandNamespace}}
		delete(objects, objectID(deployment)) // The garbage collector has finished.
		result, err := r.Reconcile(t.Context(), singletonRequest())
		require.NoError(t, err)
		assert.Equal(t, defaultRequeueTime, result.RequeueAfter)
		assert.Contains(t, instance.Finalizers, finalizer)
		assert.Len(t, objects, 1, "only the Secret, whose DELETE keeps failing, remains")
		assert.Equal(t, []string{
			"ValidatingWebhookConfiguration", "Service", "Service", "Deployment",
			"RoleBinding", "Role", "ClusterRoleBinding", "ClusterRole", "Secret", "ServiceAccount",
		}, deletedKinds, "DELETE requests must follow Helm's uninstall order")
	})

	t.Run("an accepted DELETE still requires a later absence check", func(t *testing.T) {
		failDelete = false
		_, err := r.Reconcile(t.Context(), singletonRequest())
		require.NoError(t, err)
		assert.Contains(t, instance.Finalizers, finalizer, "the Secret DELETE succeeding this round is not enough on its own")
	})

	t.Run("the finalizer is removed once every managed resource is confirmed absent", func(t *testing.T) {
		_, err := r.Reconcile(t.Context(), singletonRequest())
		require.NoError(t, err)
		assert.Equal(t, []string{"example.test/other"}, instance.Finalizers)
		assert.Empty(t, objects)
	})
}

func TestCleanupPreservesDependenciesWhenDeleteFails(t *testing.T) {
	t.Setenv(imageEnvVar, "example.test/approver-policy:test")
	instance := testInstance()
	deployment, err := getDeploymentObject(instance, "")
	require.NoError(t, err)

	for _, blocked := range []client.Object{getWebhookObject(instance), deployment} {
		t.Run(reflect.TypeOf(blocked).Elem().Name(), func(t *testing.T) {
			r, c := testReconciler(t, instance)
			// Include dependencies from later phases to prove they are not deleted.
			objects := append(getRBACObjects(instance), getSecretObject(instance), getServiceAccountObject(instance), deployment)
			if _, webhook := blocked.(*admissionregistrationv1.ValidatingWebhookConfiguration); webhook {
				objects = append(objects, blocked)
				objects = append(objects, getServiceObjects(instance)...)
			}
			r.apiReader = &testAPIReader{objects: objects}
			deleteErr := apierrors.NewForbidden(v1alpha1.Resource("managed"), blocked.GetName(), errors.New("temporarily denied"))
			c.DeleteReturns(deleteErr)

			pending, err := r.cleanUp(t.Context(), instance)

			require.ErrorIs(t, err, deleteErr)
			assert.True(t, pending)
			require.Equal(t, 1, c.DeleteCallCount(), "dependencies must remain when deletion fails")
			_, deleted, _ := c.DeleteArgsForCall(0)
			assert.Equal(t, objectID(blocked), objectID(deleted))
		})
	}
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

func TestDeleteResourceTreatsNotFoundOnDeleteAsSuccess(t *testing.T) {
	instance := testInstance()
	r, c := testReconciler(t, instance)
	existing := getSecretObject(instance)
	existing.UID = "secret-uid"
	r.apiReader = &testAPIReader{objects: []client.Object{existing}}
	// A concurrent deletion (e.g. by another writer, or a retried request whose
	// first DELETE already succeeded) must not be reported as a cleanup error.
	c.DeleteReturns(apierrors.NewNotFound(v1alpha1.Resource("secrets"), existing.Name))

	pending, err := r.deleteResource(t.Context(), instance, getSecretObject(instance))

	require.NoError(t, err)
	assert.False(t, pending, "the resource is already gone, so cleanup is not waiting on it")
	assert.Equal(t, 1, c.DeleteCallCount())
	assert.Zero(t, c.GetCallCount(), "cleanup reads must use the API reader")
}

// testAPIReader is a minimal client.Reader double for the uncached,
// unfiltered reads used by cleanup and cache-miss confirmation. It models
// API server state independently of the fake CtrlClient's cached reads.
type testAPIReader struct {
	client.Reader
	objects  []client.Object
	getErr   error
	getCalls int
	getStub  func(context.Context, client.ObjectKey, client.Object) error
}

func (r *testAPIReader) Get(ctx context.Context, key client.ObjectKey, target client.Object, _ ...client.GetOption) error {
	r.getCalls++
	if r.getErr != nil {
		return r.getErr
	}
	if r.getStub != nil {
		return r.getStub(ctx, key, target)
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
