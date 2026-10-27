package approverpolicy

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

func TestFinalizerConflictPreservesConcurrentChanges(t *testing.T) {
	for _, add := range []bool{true, false} {
		t.Run(map[bool]string{true: "add", false: "remove"}[add], func(t *testing.T) {
			stored := testInstance()
			if !add {
				stored.Finalizers = []string{finalizer}
			}
			r, c := testReconciler(t, stored)
			c.UpdateCalls(func(_ context.Context, obj client.Object, _ ...client.UpdateOption) error {
				if c.UpdateCallCount() == 1 {
					stored.Finalizers = append(stored.Finalizers, "other.example/finalizer")
					stored.Labels = map[string]string{"other-writer": "keep"}
					stored.Spec.ApproverPolicyConfig.LogLevel = 5
					stored.Generation++
					return apierrors.NewConflict(v1alpha1.Resource("approverpolicymanagers"), "cluster", errors.New("concurrent edit"))
				}
				obj.(*v1alpha1.ApproverPolicyManager).DeepCopyInto(stored)
				return nil
			})
			require.NoError(t, r.updateFinalizer(t.Context(), stored.DeepCopy(), add))
			assert.Equal(t, 2, c.UpdateCallCount())
			assert.Contains(t, stored.Finalizers, "other.example/finalizer")
			assert.Equal(t, map[string]string{"other-writer": "keep"}, stored.Labels)
			assert.Equal(t, int32(5), stored.Spec.ApproverPolicyConfig.LogLevel)
			if add {
				assert.Contains(t, stored.Finalizers, finalizer)
			} else {
				assert.NotContains(t, stored.Finalizers, finalizer)
			}
		})
	}
}

func TestFinalizerSkipsDeletionAndReplacement(t *testing.T) {
	t.Run("deletion starts before finalizer write", func(t *testing.T) {
		stored := testInstance()
		initial := stored.DeepCopy()
		now := metav1.Now()
		stored.DeletionTimestamp = &now
		r, c := testReconciler(t, stored)
		require.NoError(t, r.updateFinalizer(t.Context(), initial, true))
		assert.Zero(t, c.UpdateCallCount())
		assert.NotNil(t, initial.DeletionTimestamp)
	})
	t.Run("replacement is not modified", func(t *testing.T) {
		stored := testInstance()
		initial := stored.DeepCopy()
		stored.UID = "replacement"
		r, c := testReconciler(t, stored)
		require.True(t, apierrors.IsConflict(r.updateFinalizer(t.Context(), initial, true)))
		assert.Zero(t, c.UpdateCallCount())
	})
}

func TestStatusConflictPreservesOtherFields(t *testing.T) {
	stored := testInstance()
	r, c := testReconciler(t, stored)
	c.StatusUpdateCalls(func(_ context.Context, obj client.Object, _ ...client.SubResourceUpdateOption) error {
		if c.StatusUpdateCallCount() == 1 {
			stored.Status.ApproverPolicyImage = "concurrent-image"
			stored.Status.Conditions = append(stored.Status.Conditions, metav1.Condition{Type: "Other", Status: metav1.ConditionTrue, Reason: "OtherWriter", LastTransitionTime: metav1.Now()})
			return apierrors.NewConflict(v1alpha1.Resource("approverpolicymanagers"), "cluster", errors.New("concurrent status"))
		}
		obj.(*v1alpha1.ApproverPolicyManager).Status.DeepCopyInto(&stored.Status)
		return nil
	})
	_, err := r.processReconcileRequest(t.Context(), stored.DeepCopy())
	require.NoError(t, err)
	assert.Equal(t, 2, c.StatusUpdateCallCount())
	assert.Equal(t, "example.test/approver-policy:test", stored.Status.ApproverPolicyImage)
	assert.NotNil(t, stored.Status.GetCondition("Other"))
}

func TestStatusRejectsStaleGeneration(t *testing.T) {
	stored := testInstance()
	initial := stored.DeepCopy()
	stored.Generation++
	r, c := testReconciler(t, stored)
	_, err := r.processReconcileRequest(t.Context(), initial)
	require.True(t, apierrors.IsConflict(err))
	assert.Zero(t, c.StatusUpdateCallCount())
}

func TestManagedMetadataRemoval(t *testing.T) {
	instance := testInstance()
	desired := getSecretObject(instance)
	existing := desired.DeepCopy()
	existing.Labels["removed"] = "old"
	existing.Labels["foreign"] = "keep"
	existing.ManagedFields = []metav1.ManagedFieldsEntry{{
		Manager:  fieldOwner,
		FieldsV1: &metav1.FieldsV1{Raw: []byte(`{"f:metadata":{"f:labels":{"f:removed":{}}}}`)},
	}}
	assert.True(t, managedMetadataModified(desired, existing))
	delete(existing.Labels, "removed")
	assert.False(t, managedMetadataModified(desired, existing), "foreign metadata is not removed")
}
