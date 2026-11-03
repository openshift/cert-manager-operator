package approverpolicy

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

// Verify generation changes on the real CRD, particularly when deletion starts
// while our finalizer still holds the object. A fake client cannot model this.
func TestPrimaryResourceGenerationEvents(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("set KUBEBUILDER_ASSETS to run generation event integration")
	}
	env := &envtest.Environment{
		CRDDirectoryPaths:     []string{"../../../config/crd/bases/operator.openshift.io_approverpolicymanagers.yaml"},
		ErrorIfCRDPathMissing: true,
	}
	config, err := env.Start()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, env.Stop()) })
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha1.AddToScheme(scheme))
	live, err := client.New(config, client.Options{Scheme: scheme})
	require.NoError(t, err)
	instance := &v1alpha1.ApproverPolicyManager{ObjectMeta: metav1.ObjectMeta{
		Name: approverPolicyManagerObjectName, Finalizers: []string{finalizer},
	}}
	require.NoError(t, live.Create(t.Context(), instance))
	key := client.ObjectKeyFromObject(instance)
	p := predicate.GenerationChangedPredicate{}

	// Configuration changes must trigger reconciliation.
	before := instance.DeepCopy()
	instance.Spec.ApproverPolicyConfig.LogLevel = 2
	require.NoError(t, live.Update(t.Context(), instance))
	assert.True(t, p.Update(event.UpdateEvent{ObjectOld: before, ObjectNew: instance}))

	// Writing status must not trigger another reconciliation.
	before = instance.DeepCopy()
	instance.Status.ApproverPolicyImage = "example.test/approver-policy:test"
	require.NoError(t, live.Status().Update(t.Context(), instance))
	assert.False(t, p.Update(event.UpdateEvent{ObjectOld: before, ObjectNew: instance}))

	// DELETE updates generation before finalization, allowing cleanup to start.
	before = instance.DeepCopy()
	require.NoError(t, live.Delete(t.Context(), instance))
	require.NoError(t, live.Get(t.Context(), key, instance))
	require.False(t, instance.DeletionTimestamp.IsZero())
	assert.Contains(t, instance.Finalizers, finalizer)
	assert.Greater(t, instance.Generation, before.Generation)
	assert.True(t, p.Update(event.UpdateEvent{ObjectOld: before, ObjectNew: instance}))
}
