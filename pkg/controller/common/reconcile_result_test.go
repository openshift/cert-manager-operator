package common

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

const testRequeueDuration = 30 * time.Second

func TestHandleReconcileResult(t *testing.T) {
	retryErr := NewRetryRequiredError(errors.New("apiserver unavailable"), "failed to create deployment")
	irrecoverableErr := NewIrrecoverableError(errors.New("invalid spec"), "failed to validate config")
	statusErr := errors.New("status update conflict")

	tests := []struct {
		name string
		// primed runs HandleReconcileResult once with the same reconcileErr
		// beforehand, so the conditions are already in their target state.
		primed          bool
		reconcileErr    error
		updateErr       error
		wantResult      ctrl.Result
		wantErr         error
		wantCalls       int
		wantDegraded    metav1.ConditionStatus
		wantReady       metav1.ConditionStatus
		wantReadyReason string
	}{
		{
			name:            "success with changed conditions updates status",
			wantResult:      ctrl.Result{},
			wantCalls:       1,
			wantDegraded:    metav1.ConditionFalse,
			wantReady:       metav1.ConditionTrue,
			wantReadyReason: v1alpha1.ReasonReady,
		},
		{
			name:            "success with unchanged conditions skips status update",
			primed:          true,
			wantResult:      ctrl.Result{},
			wantCalls:       0,
			wantDegraded:    metav1.ConditionFalse,
			wantReady:       metav1.ConditionTrue,
			wantReadyReason: v1alpha1.ReasonReady,
		},
		{
			name:            "success with failed status update returns the update error",
			updateErr:       statusErr,
			wantResult:      ctrl.Result{},
			wantErr:         statusErr,
			wantCalls:       1,
			wantDegraded:    metav1.ConditionFalse,
			wantReady:       metav1.ConditionTrue,
			wantReadyReason: v1alpha1.ReasonReady,
		},
		{
			name:            "retryable error with successful status update requeues",
			reconcileErr:    retryErr,
			wantResult:      ctrl.Result{RequeueAfter: testRequeueDuration},
			wantCalls:       1,
			wantDegraded:    metav1.ConditionFalse,
			wantReady:       metav1.ConditionFalse,
			wantReadyReason: v1alpha1.ReasonInProgress,
		},
		{
			name:            "retryable error with failed status update returns the update error",
			reconcileErr:    retryErr,
			updateErr:       statusErr,
			wantResult:      ctrl.Result{},
			wantErr:         statusErr,
			wantCalls:       1,
			wantDegraded:    metav1.ConditionFalse,
			wantReady:       metav1.ConditionFalse,
			wantReadyReason: v1alpha1.ReasonInProgress,
		},
		{
			name:            "retryable error with unchanged conditions requeues without status update",
			primed:          true,
			reconcileErr:    retryErr,
			wantResult:      ctrl.Result{RequeueAfter: testRequeueDuration},
			wantCalls:       0,
			wantDegraded:    metav1.ConditionFalse,
			wantReady:       metav1.ConditionFalse,
			wantReadyReason: v1alpha1.ReasonInProgress,
		},
		{
			name:            "unclassified error is treated as retryable",
			reconcileErr:    errors.New("plain error"),
			wantResult:      ctrl.Result{RequeueAfter: testRequeueDuration},
			wantCalls:       1,
			wantDegraded:    metav1.ConditionFalse,
			wantReady:       metav1.ConditionFalse,
			wantReadyReason: v1alpha1.ReasonInProgress,
		},
		{
			name:            "wrapped irrecoverable error is still irrecoverable",
			reconcileErr:    fmt.Errorf("reconcile: %w", irrecoverableErr),
			wantResult:      ctrl.Result{},
			wantCalls:       1,
			wantDegraded:    metav1.ConditionTrue,
			wantReady:       metav1.ConditionFalse,
			wantReadyReason: v1alpha1.ReasonFailed,
		},
		{
			name:            "irrecoverable error with unchanged conditions skips status update",
			primed:          true,
			reconcileErr:    irrecoverableErr,
			wantResult:      ctrl.Result{},
			wantCalls:       0,
			wantDegraded:    metav1.ConditionTrue,
			wantReady:       metav1.ConditionFalse,
			wantReadyReason: v1alpha1.ReasonFailed,
		},
		{
			name:            "irrecoverable error marks degraded and does not requeue",
			reconcileErr:    irrecoverableErr,
			wantResult:      ctrl.Result{},
			wantCalls:       1,
			wantDegraded:    metav1.ConditionTrue,
			wantReady:       metav1.ConditionFalse,
			wantReadyReason: v1alpha1.ReasonFailed,
		},
		{
			name:            "irrecoverable error with failed status update returns the update error",
			reconcileErr:    irrecoverableErr,
			updateErr:       statusErr,
			wantResult:      ctrl.Result{},
			wantErr:         statusErr,
			wantCalls:       1,
			wantDegraded:    metav1.ConditionTrue,
			wantReady:       metav1.ConditionFalse,
			wantReadyReason: v1alpha1.ReasonFailed,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var status v1alpha1.ConditionalStatus
			if tt.primed {
				_, _ = HandleReconcileResult(&status, tt.reconcileErr, logr.Discard(), func(error) error { return nil }, testRequeueDuration)
			}

			calls := 0
			// Mirrors the istiocsr and trustmanager updateCondition callbacks,
			// which return prependErr when the status write succeeds.
			updateFn := func(prependErr error) error {
				calls++
				if tt.updateErr != nil {
					return tt.updateErr
				}
				return prependErr
			}

			got, err := HandleReconcileResult(&status, tt.reconcileErr, logr.Discard(), updateFn, testRequeueDuration)

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("error = %v, want %v", err, tt.wantErr)
			}
			if got != tt.wantResult {
				t.Errorf("result = %+v, want %+v", got, tt.wantResult)
			}
			if calls != tt.wantCalls {
				t.Errorf("updateConditionFn calls = %d, want %d", calls, tt.wantCalls)
			}
			if c := status.GetCondition(v1alpha1.Degraded); c == nil || c.Status != tt.wantDegraded {
				t.Errorf("Degraded condition = %+v, want status %s", c, tt.wantDegraded)
			}
			if c := status.GetCondition(v1alpha1.Ready); c == nil || c.Status != tt.wantReady || c.Reason != tt.wantReadyReason {
				t.Errorf("Ready condition = %+v, want status %s reason %s", c, tt.wantReady, tt.wantReadyReason)
			}
		})
	}
}
