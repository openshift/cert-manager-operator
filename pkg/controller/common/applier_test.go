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
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type stubCtrlClient struct {
	existsResult bool
	existsErr    error
	patchErr     error
	patchCount   int
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
func (s *stubCtrlClient) Patch(_ context.Context, _ client.Object, _ client.Patch, _ ...client.PatchOption) error {
	s.patchCount++
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

func TestApplyResource_PatchesWhenExistsAndChanged(t *testing.T) {
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
