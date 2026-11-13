package common

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type stubCtrlClient struct {
	existsResult bool
	existsErr    error
	patchErr     error
	applyErr     error
	patchCount   int
	patchTypes   []types.PatchType
	fieldManager string
	forceApply   bool
}

func (s *stubCtrlClient) Get(context.Context, client.ObjectKey, client.Object) error {
	return nil
}
func (s *stubCtrlClient) List(context.Context, client.ObjectList, ...client.ListOption) error {
	return nil
}
func (s *stubCtrlClient) StatusUpdate(context.Context, client.Object, ...client.SubResourceUpdateOption) error {
	return nil
}
func (s *stubCtrlClient) Update(context.Context, client.Object, ...client.UpdateOption) error {
	return nil
}
func (s *stubCtrlClient) UpdateWithRetry(context.Context, client.Object, ...client.UpdateOption) error {
	return nil
}
func (s *stubCtrlClient) Create(context.Context, client.Object, ...client.CreateOption) error {
	return nil
}
func (s *stubCtrlClient) Delete(context.Context, client.Object, ...client.DeleteOption) error {
	return nil
}
func (s *stubCtrlClient) Patch(_ context.Context, _ client.Object, p client.Patch, opts ...client.PatchOption) error {
	s.patchCount++
	s.patchTypes = append(s.patchTypes, p.Type())
	po := &client.PatchOptions{}
	for _, o := range opts {
		o.ApplyToPatch(po)
	}
	s.fieldManager = po.FieldManager
	s.forceApply = po.Force != nil && *po.Force
	return s.patchErr
}

// Apply records server-side applies as ApplyPatchType so the assertions can
// check the order of applies and merge-patch fallbacks in one slice.
func (s *stubCtrlClient) Apply(_ context.Context, _ client.Object, opts ...client.ApplyOption) error {
	s.patchCount++
	s.patchTypes = append(s.patchTypes, types.ApplyPatchType)
	ao := (&client.ApplyOptions{}).ApplyOptions(opts)
	s.fieldManager = ao.FieldManager
	s.forceApply = ao.Force != nil && *ao.Force
	if s.applyErr != nil {
		return s.applyErr
	}
	return s.patchErr
}
func (s *stubCtrlClient) Exists(_ context.Context, _ client.ObjectKey, _ client.Object) (bool, error) {
	return s.existsResult, s.existsErr
}

func TestApplyResource_CreatesWhenNotExists(t *testing.T) {
	c := &stubCtrlClient{existsResult: false}
	recorder := record.NewFakeRecorder(10)

	desired := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: "test-sa", Namespace: "test-ns"},
	}
	owner := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "owner"}}

	err := ApplyResource(
		context.Background(), c, logr.Discard(), recorder,
		owner, desired, &corev1.ServiceAccount{}, "test-manager",
		func(d, e *corev1.ServiceAccount) bool { return true },
	)
	require.NoError(t, err)
	assert.Equal(t, 1, c.patchCount)
	// Creation goes through server-side apply with force ownership.
	assert.Equal(t, []types.PatchType{types.ApplyPatchType}, c.patchTypes)
	assert.Equal(t, "test-manager", c.fieldManager)
	assert.True(t, c.forceApply, "creation should force ownership")

	select {
	case event := <-recorder.Events:
		assert.Contains(t, event, "Reconciled")
	default:
		t.Error("expected reconcile event to be recorded")
	}
}

func TestApplyResource_NoOpWhenExistsAndUnchanged(t *testing.T) {
	c := &stubCtrlClient{existsResult: true}
	recorder := record.NewFakeRecorder(10)

	desired := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: "test-sa", Namespace: "test-ns"},
	}

	err := ApplyResource(
		context.Background(), c, logr.Discard(), recorder,
		&corev1.ConfigMap{}, desired, &corev1.ServiceAccount{}, "test-manager",
		func(d, e *corev1.ServiceAccount) bool { return false },
	)
	require.NoError(t, err)
	assert.Equal(t, 0, c.patchCount, "should not patch when unchanged")
}

func TestApplyResource_DriftUsesSSAWhenApplySucceeds(t *testing.T) {
	c := &stubCtrlClient{existsResult: true}
	recorder := record.NewFakeRecorder(10)

	desired := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: "test-sa", Namespace: "test-ns"},
	}

	err := ApplyResource(
		context.Background(), c, logr.Discard(), recorder,
		&corev1.ConfigMap{}, desired, &corev1.ServiceAccount{}, "test-manager",
		func(d, e *corev1.ServiceAccount) bool { return true },
	)
	require.NoError(t, err)
	assert.Equal(t, 1, c.patchCount)
	// Drift correction prefers SSA: it preserves fields owned by other
	// controllers and prunes fields this manager no longer desires.
	assert.Equal(t, []types.PatchType{types.ApplyPatchType}, c.patchTypes)
	assert.Equal(t, "test-manager", c.fieldManager)
	assert.True(t, c.forceApply, "drift correction should force ownership")
}

func TestApplyResource_DriftFallsBackToMergeWhenApplyFails(t *testing.T) {
	c := &stubCtrlClient{
		existsResult: true,
		applyErr: apierrors.NewInvalid(
			schema.GroupKind{Group: "core", Kind: "Service"}, "test-sa",
			field.ErrorList{field.Invalid(field.NewPath("spec.ports[1].name"), "web", "Duplicate value: \"web\"")}),
	}
	recorder := record.NewFakeRecorder(10)

	desired := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: "test-sa", Namespace: "test-ns"},
	}

	err := ApplyResource(
		context.Background(), c, logr.Discard(), recorder,
		&corev1.ConfigMap{}, desired, &corev1.ServiceAccount{}, "test-manager",
		func(d, e *corev1.ServiceAccount) bool { return true },
	)
	require.NoError(t, err)
	// SSA cannot correct drift in granular list-map key fields (the applied
	// entry is added alongside the drifted one and fails validation), so the
	// drift correction falls back to a JSON merge patch, which replaces lists
	// wholesale.
	assert.Equal(t, 2, c.patchCount)
	assert.Equal(t, []types.PatchType{types.ApplyPatchType, types.MergePatchType}, c.patchTypes)
	assert.Equal(t, "test-manager", c.fieldManager)
	assert.False(t, c.forceApply, "merge-patch fallback should not force ownership")

	select {
	case event := <-recorder.Events:
		assert.Contains(t, event, "Reconciled")
	default:
		t.Error("expected reconcile event to be recorded")
	}
}

func TestApplyResource_DriftReturnsApplyErrorWhenBothPatchesFail(t *testing.T) {
	c := &stubCtrlClient{
		existsResult: true,
		applyErr:     fmt.Errorf("apply failed: duplicate list keys"),
		patchErr:     fmt.Errorf("merge failed: forbidden"),
	}
	recorder := record.NewFakeRecorder(10)

	desired := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: "test-sa", Namespace: "test-ns"},
	}

	err := ApplyResource(
		context.Background(), c, logr.Discard(), recorder,
		&corev1.ConfigMap{}, desired, &corev1.ServiceAccount{}, "test-manager",
		func(d, e *corev1.ServiceAccount) bool { return true },
	)
	require.Error(t, err)
	// The apply error is surfaced (not the merge fallback error) so callers
	// classify the original failure.
	assert.Contains(t, err.Error(), "apply failed")
	assert.Contains(t, err.Error(), "failed to apply")
}

func TestApplyResource_ExistsError(t *testing.T) {
	c := &stubCtrlClient{existsErr: fmt.Errorf("connection refused")}
	recorder := record.NewFakeRecorder(10)

	desired := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: "test-sa", Namespace: "test-ns"},
	}

	err := ApplyResource(
		context.Background(), c, logr.Discard(), recorder,
		&corev1.ConfigMap{}, desired, &corev1.ServiceAccount{}, "test-manager",
		func(d, e *corev1.ServiceAccount) bool { return true },
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to check if")
	assert.Equal(t, 0, c.patchCount)
}

func TestApplyResource_PatchError(t *testing.T) {
	c := &stubCtrlClient{existsResult: false, patchErr: fmt.Errorf("forbidden")}
	recorder := record.NewFakeRecorder(10)

	desired := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: "test-sa", Namespace: "test-ns"},
	}

	err := ApplyResource(
		context.Background(), c, logr.Discard(), recorder,
		&corev1.ConfigMap{}, desired, &corev1.ServiceAccount{}, "test-manager",
		func(d, e *corev1.ServiceAccount) bool { return true },
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to apply")
}

func TestApplyResource_UnauthorizedPatchIsIrrecoverable(t *testing.T) {
	c := &stubCtrlClient{existsResult: false, patchErr: apierrors.NewUnauthorized("not allowed")}
	recorder := record.NewFakeRecorder(10)

	desired := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: "test-sa", Namespace: "test-ns"},
	}

	err := ApplyResource(
		context.Background(), c, logr.Discard(), recorder,
		&corev1.ConfigMap{}, desired, &corev1.ServiceAccount{}, "test-manager",
		func(d, e *corev1.ServiceAccount) bool { return true },
	)
	require.Error(t, err)
	assert.True(t, IsIrrecoverableError(err), "unauthorized error should be irrecoverable")
}

func TestApplyResource_ConflictPatchIsRetryable(t *testing.T) {
	c := &stubCtrlClient{
		existsResult: false,
		patchErr: apierrors.NewConflict(
			schema.GroupResource{Group: "", Resource: "serviceaccounts"}, "test-sa", fmt.Errorf("conflict")),
	}
	recorder := record.NewFakeRecorder(10)

	desired := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: "test-sa", Namespace: "test-ns"},
	}

	err := ApplyResource(
		context.Background(), c, logr.Discard(), recorder,
		&corev1.ConfigMap{}, desired, &corev1.ServiceAccount{}, "test-manager",
		func(d, e *corev1.ServiceAccount) bool { return true },
	)
	require.Error(t, err)
	assert.True(t, IsRetryRequiredError(err), "conflict error should be retryable")
}
