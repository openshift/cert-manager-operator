package approverpolicy

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
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

func TestFinalizerReadFailure(t *testing.T) {
	instance := testInstance()
	r, c := testReconciler(t, instance)
	readErr := apierrors.NewTimeoutError("read timed out", 1)
	c.GetReturns(readErr)
	require.ErrorIs(t, r.updateFinalizer(t.Context(), instance.DeepCopy(), true), readErr)
	assert.Zero(t, c.UpdateCallCount())
}

func TestStatusReadFailure(t *testing.T) {
	instance := testInstance()
	r, c := testReconciler(t, instance)
	readErr := apierrors.NewTimeoutError("read timed out", 1)
	c.GetReturns(readErr)
	err := r.updateStatus(t.Context(), instance.DeepCopy(), v1alpha1.ConditionalStatus{})
	require.Error(t, err)
	assert.ErrorIs(t, err, readErr)
	assert.Zero(t, c.StatusUpdateCallCount())
}

func TestPrepareResource(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version string
	}{
		{name: "no RELATED_IMAGE version pin"},
		{name: "version pin adds the version label", version: "v1.2.3"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.version != "" {
				t.Setenv(versionEnvVar, tc.version)
			}
			instance := testInstance()
			instance.Spec.ControllerConfig.Labels = map[string]string{"team": "security"}
			instance.Spec.ControllerConfig.Annotations = map[string]string{"example.test/note": "value"}
			obj := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
				Labels:      map[string]string{"component": "asset-default"},
				Annotations: map[string]string{"asset.example/injected": "keep"},
			}}

			prepareResource(instance, obj)

			// Asset-provided labels/annotations win over operator-configured ones.
			assert.Equal(t, "asset-default", obj.Labels["component"])
			assert.Equal(t, "security", obj.Labels["team"])
			// The managed-resource marker label always wins so cache/watch filtering works.
			assert.Equal(t, RequestEnqueueLabelValue, obj.Labels[common.ManagedResourceLabelKey])
			assert.Equal(t, "keep", obj.Annotations["asset.example/injected"])
			assert.Equal(t, "value", obj.Annotations["example.test/note"])
			if tc.version != "" {
				assert.Equal(t, tc.version, obj.Labels["app.kubernetes.io/version"])
			} else {
				assert.NotContains(t, obj.Labels, "app.kubernetes.io/version")
			}
			require.Len(t, obj.OwnerReferences, 1)
			assert.Equal(t, instance.UID, obj.OwnerReferences[0].UID)
			assert.True(t, *obj.OwnerReferences[0].Controller)
		})
	}
}

func TestManagedMapModified(t *testing.T) {
	ownedFields := func(raw string) []metav1.ManagedFieldsEntry {
		return []metav1.ManagedFieldsEntry{{Manager: fieldOwner, FieldsV1: &metav1.FieldsV1{Raw: []byte(raw)}}}
	}
	for _, tc := range []struct {
		name          string
		desired       map[string]string
		existing      map[string]string
		managedFields []metav1.ManagedFieldsEntry
		want          bool
	}{
		{
			name:     "desired key missing from existing",
			desired:  map[string]string{"team": "security"},
			existing: map[string]string{},
			want:     true,
		},
		{
			name:     "desired key present with a different value",
			desired:  map[string]string{"team": "security"},
			existing: map[string]string{"team": "other"},
			want:     true,
		},
		{
			name:     "a foreign key with no managed fields is not drift",
			desired:  map[string]string{"team": "security"},
			existing: map[string]string{"team": "security", "foreign": "keep"},
			want:     false,
		},
		{
			name:          "a key we previously owned and dropped is removed",
			desired:       map[string]string{},
			existing:      map[string]string{"removed": "old"},
			managedFields: ownedFields(`{"f:metadata":{"f:labels":{"f:removed":{}}}}`),
			want:          true,
		},
		{
			name:          "a managed fields entry from another manager is ignored",
			desired:       map[string]string{},
			existing:      map[string]string{"removed": "old"},
			managedFields: []metav1.ManagedFieldsEntry{{Manager: "other-controller", FieldsV1: &metav1.FieldsV1{Raw: []byte(`{"f:metadata":{"f:labels":{"f:removed":{}}}}`)}}},
			want:          false,
		},
		{
			name:          "a managed fields entry for a subresource is ignored",
			desired:       map[string]string{},
			existing:      map[string]string{"removed": "old"},
			managedFields: []metav1.ManagedFieldsEntry{{Manager: fieldOwner, Subresource: "status", FieldsV1: &metav1.FieldsV1{Raw: []byte(`{"f:metadata":{"f:labels":{"f:removed":{}}}}`)}}},
			want:          false,
		},
		{
			name:          "a managed fields entry without FieldsV1 is ignored",
			desired:       map[string]string{},
			existing:      map[string]string{"removed": "old"},
			managedFields: []metav1.ManagedFieldsEntry{{Manager: fieldOwner}},
			want:          false,
		},
		{
			name:          "unparsable FieldsV1 is treated as drift",
			desired:       map[string]string{},
			existing:      map[string]string{"removed": "old"},
			managedFields: ownedFields(`not-json`),
			want:          true,
		},
		{
			// Real FieldsV1 payloads include a "." marker key alongside "f:" keys
			// (e.g. to record ownership of the map itself); it must not be
			// mistaken for an owned field. Only the "." key is present here so
			// the assertion does not depend on Go's random map iteration order.
			name:          "a non f: prefixed field path segment (e.g. '.') is skipped",
			desired:       map[string]string{},
			existing:      map[string]string{},
			managedFields: ownedFields(`{"f:metadata":{"f:labels":{".":{}}}}`),
			want:          false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			existing := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{ManagedFields: tc.managedFields}}
			assert.Equal(t, tc.want, managedMapModified(tc.desired, tc.existing, existing, "f:metadata", "f:labels"))
		})
	}
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
