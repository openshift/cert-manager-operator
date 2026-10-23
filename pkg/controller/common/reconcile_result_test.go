package common

import (
	"fmt"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

func conditionByType(status *v1alpha1.ConditionalStatus, ct string) *metav1.Condition {
	return status.GetCondition(ct)
}

func TestHandleReconcileResult(t *testing.T) {
	tests := []struct {
		name string

		reconcileErr error

		wantDegradedStatus    metav1.ConditionStatus
		wantDegradedReason    string
		wantReadyStatus       metav1.ConditionStatus
		wantReadyReason       string
		wantProgressingStatus metav1.ConditionStatus
		wantProgressingReason string
		wantRequeue           bool
		wantUpdateCalled      bool
	}{
		{
			name:                  "success sets Ready=True, Degraded=False, Progressing=False",
			reconcileErr:          nil,
			wantDegradedStatus:    metav1.ConditionFalse,
			wantDegradedReason:    v1alpha1.ReasonReady,
			wantReadyStatus:       metav1.ConditionTrue,
			wantReadyReason:       v1alpha1.ReasonReady,
			wantProgressingStatus: metav1.ConditionFalse,
			wantProgressingReason: v1alpha1.ReasonReady,
			wantUpdateCalled:      true,
		},
		{
			name:                  "irrecoverable error sets Degraded=True, Ready=False, Progressing=False",
			reconcileErr:          NewIrrecoverableError(fmt.Errorf("fatal"), "fatal failure"),
			wantDegradedStatus:    metav1.ConditionTrue,
			wantDegradedReason:    v1alpha1.ReasonFailed,
			wantReadyStatus:       metav1.ConditionFalse,
			wantReadyReason:       v1alpha1.ReasonFailed,
			wantProgressingStatus: metav1.ConditionFalse,
			wantProgressingReason: v1alpha1.ReasonFailed,
			wantUpdateCalled:      true,
		},
		{
			name:                  "irrecoverable error with custom ConditionReason uses it",
			reconcileErr:          NewIrrecoverableError(fmt.Errorf("validation"), "invalid config").WithConditionReason(v1alpha1.ReasonValidationFailed),
			wantDegradedStatus:    metav1.ConditionTrue,
			wantDegradedReason:    v1alpha1.ReasonValidationFailed,
			wantReadyStatus:       metav1.ConditionFalse,
			wantReadyReason:       v1alpha1.ReasonValidationFailed,
			wantProgressingStatus: metav1.ConditionFalse,
			wantProgressingReason: v1alpha1.ReasonValidationFailed,
			wantUpdateCalled:      true,
		},
		{
			name:                  "recoverable error sets Progressing=True, Ready=False, Degraded=False",
			reconcileErr:          NewRetryRequiredError(fmt.Errorf("transient"), "retrying"),
			wantDegradedStatus:    metav1.ConditionFalse,
			wantDegradedReason:    v1alpha1.ReasonReady,
			wantReadyStatus:       metav1.ConditionFalse,
			wantReadyReason:       v1alpha1.ReasonInProgress,
			wantProgressingStatus: metav1.ConditionTrue,
			wantProgressingReason: v1alpha1.ReasonReconciling,
			wantRequeue:           true,
			wantUpdateCalled:      true,
		},
		{
			name:                  "recoverable error with custom ConditionReason uses it for Ready and Progressing",
			reconcileErr:          NewRetryRequiredError(fmt.Errorf("waiting"), "waiting for dep").WithConditionReason(v1alpha1.ReasonWaitingForDependencies),
			wantDegradedStatus:    metav1.ConditionFalse,
			wantDegradedReason:    v1alpha1.ReasonReady,
			wantReadyStatus:       metav1.ConditionFalse,
			wantReadyReason:       v1alpha1.ReasonWaitingForDependencies,
			wantProgressingStatus: metav1.ConditionTrue,
			wantProgressingReason: v1alpha1.ReasonWaitingForDependencies,
			wantRequeue:           true,
			wantUpdateCalled:      true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status := &v1alpha1.ConditionalStatus{}
			updateCalled := false

			updateFn := func(_ error) error {
				updateCalled = true
				return nil
			}

			result, err := HandleReconcileResult(
				status,
				tc.reconcileErr,
				logr.Discard(),
				updateFn,
				10*time.Second,
			)
			require.NoError(t, err)

			assert.Equal(t, tc.wantUpdateCalled, updateCalled, "updateConditionFn called")

			if tc.wantRequeue {
				assert.NotZero(t, result.RequeueAfter, "expected requeue")
			} else {
				assert.Zero(t, result.RequeueAfter, "expected no requeue")
			}

			degraded := conditionByType(status, v1alpha1.Degraded)
			require.NotNil(t, degraded, "Degraded condition should be set")
			assert.Equal(t, tc.wantDegradedStatus, degraded.Status)
			assert.Equal(t, tc.wantDegradedReason, degraded.Reason)

			ready := conditionByType(status, v1alpha1.Ready)
			require.NotNil(t, ready, "Ready condition should be set")
			assert.Equal(t, tc.wantReadyStatus, ready.Status)
			assert.Equal(t, tc.wantReadyReason, ready.Reason)

			progressing := conditionByType(status, v1alpha1.Progressing)
			require.NotNil(t, progressing, "Progressing condition should be set")
			assert.Equal(t, tc.wantProgressingStatus, progressing.Status)
			assert.Equal(t, tc.wantProgressingReason, progressing.Reason)
		})
	}
}

func TestHandleReconcileResult_NoChangeSkipsUpdate(t *testing.T) {
	status := &v1alpha1.ConditionalStatus{}
	status.SetCondition(v1alpha1.Degraded, metav1.ConditionFalse, v1alpha1.ReasonReady, "")
	status.SetCondition(v1alpha1.Ready, metav1.ConditionTrue, v1alpha1.ReasonReady, "reconciliation successful")
	status.SetCondition(v1alpha1.Progressing, metav1.ConditionFalse, v1alpha1.ReasonReady, "")

	updateCalled := false
	updateFn := func(_ error) error {
		updateCalled = true
		return nil
	}

	_, err := HandleReconcileResult(status, nil, logr.Discard(), updateFn, 10*time.Second)
	require.NoError(t, err)
	assert.False(t, updateCalled, "updateConditionFn should not be called when conditions are unchanged")
}

func TestHandleReconcileResult_UpdateFnErrorPropagated(t *testing.T) {
	status := &v1alpha1.ConditionalStatus{}
	updateErr := fmt.Errorf("status update failed")

	updateFn := func(_ error) error {
		return updateErr
	}

	_, err := HandleReconcileResult(status, nil, logr.Discard(), updateFn, 10*time.Second)
	require.ErrorIs(t, err, updateErr)
}

func TestHandleReconcileResult_IrrecoverableUpdateFnErrorPropagated(t *testing.T) {
	status := &v1alpha1.ConditionalStatus{}
	updateErr := fmt.Errorf("status update failed")

	updateFn := func(_ error) error {
		return updateErr
	}

	_, err := HandleReconcileResult(
		status,
		NewIrrecoverableError(fmt.Errorf("fatal"), "fatal"),
		logr.Discard(),
		updateFn,
		10*time.Second,
	)
	require.ErrorIs(t, err, updateErr)
}

func TestHandleReconcileResult_RecoverableUpdateFnErrorPropagated(t *testing.T) {
	status := &v1alpha1.ConditionalStatus{}
	updateErr := fmt.Errorf("status update failed")

	updateFn := func(_ error) error {
		return updateErr
	}

	result, err := HandleReconcileResult(
		status,
		NewRetryRequiredError(fmt.Errorf("transient"), "retrying"),
		logr.Discard(),
		updateFn,
		10*time.Second,
	)
	require.ErrorIs(t, err, updateErr)
	assert.Zero(t, result.RequeueAfter, "should not requeue when updateFn fails")
}

func TestHandleReconcileResult_RecoverableNoChangeSkipsUpdate(t *testing.T) {
	status := &v1alpha1.ConditionalStatus{}
	reconcileErr := NewRetryRequiredError(fmt.Errorf("transient"), "retrying")

	// First call sets conditions.
	_, err := HandleReconcileResult(status, reconcileErr, logr.Discard(), func(_ error) error { return nil }, 10*time.Second)
	require.NoError(t, err)

	// Second call with same error — conditions already match, so updateFn should be skipped.
	updateCalled := false
	result, err := HandleReconcileResult(status, reconcileErr, logr.Discard(), func(_ error) error {
		updateCalled = true
		return nil
	}, 10*time.Second)
	require.NoError(t, err)
	assert.False(t, updateCalled, "updateConditionFn should not be called when conditions are unchanged")
	assert.NotZero(t, result.RequeueAfter, "should still requeue even when conditions are unchanged")
}

func TestHandleReconcileResult_IrrecoverableNoChangeSkipsUpdate(t *testing.T) {
	status := &v1alpha1.ConditionalStatus{}
	reconcileErr := NewIrrecoverableError(fmt.Errorf("fatal"), "fatal failure")

	// First call sets conditions.
	_, err := HandleReconcileResult(status, reconcileErr, logr.Discard(), func(_ error) error { return nil }, 10*time.Second)
	require.NoError(t, err)

	// Second call with same error — conditions already match, so updateFn should be skipped.
	updateCalled := false
	result, err := HandleReconcileResult(status, reconcileErr, logr.Discard(), func(_ error) error {
		updateCalled = true
		return nil
	}, 10*time.Second)
	require.NoError(t, err)
	assert.False(t, updateCalled, "updateConditionFn should not be called when conditions are unchanged")
	assert.Zero(t, result.RequeueAfter, "irrecoverable error should not requeue")
	assert.Equal(t, metav1.ConditionTrue, conditionByType(status, v1alpha1.Degraded).Status)
}
