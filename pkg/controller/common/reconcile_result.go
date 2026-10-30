package common

import (
	"fmt"
	"time"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

// HandleReconcileResult processes the result of a reconciliation attempt and
// updates status conditions accordingly.
func HandleReconcileResult(
	status *v1alpha1.ConditionalStatus,
	reconcileErr error,
	log logr.Logger,
	updateConditionFn func(prependErr error) error,
	requeueDuration time.Duration,
) (ctrl.Result, error) {
	var errUpdate error

	if reconcileErr != nil {
		if IsIrrecoverableError(reconcileErr) {
			// Permanent failure - don't retry
			// Set Degraded=True, Ready=False
			// Set both conditions atomically before updating status
			degradedChanged := status.SetCondition(v1alpha1.Degraded, metav1.ConditionTrue, v1alpha1.ReasonFailed, fmt.Sprintf("reconciliation failed with irrecoverable error not retrying: %v", reconcileErr))
			readyChanged := status.SetCondition(v1alpha1.Ready, metav1.ConditionFalse, v1alpha1.ReasonFailed, "")

			if degradedChanged || readyChanged {
				log.V(2).Info("updating conditions on irrecoverable error",
					"degradedChanged", degradedChanged,
					"readyChanged", readyChanged,
					"error", reconcileErr)
				errUpdate = updateConditionFn(nil)
			}
			return ctrl.Result{}, errUpdate
		}

		// Temporary failure - retry after delay
		// Set Degraded=False, Ready=False with "in progress" message
		// Set both conditions atomically before updating status
		degradedChanged := status.SetCondition(v1alpha1.Degraded, metav1.ConditionFalse, v1alpha1.ReasonReady, "")
		readyChanged := status.SetCondition(v1alpha1.Ready, metav1.ConditionFalse, v1alpha1.ReasonInProgress, fmt.Sprintf("reconciliation failed, retrying: %v", reconcileErr))

		if degradedChanged || readyChanged {
			log.V(2).Info("updating conditions on recoverable error",
				"degradedChanged", degradedChanged,
				"readyChanged", readyChanged,
				"error", reconcileErr)
			errUpdate = updateConditionFn(reconcileErr)
		}
		// For recoverable errors, either requeue manually or return error, not both.
		// If status update failed, return the update error; otherwise requeue.
		if errUpdate != nil {
			return ctrl.Result{}, errUpdate
		}
		return ctrl.Result{RequeueAfter: requeueDuration}, nil
	}

	// Success - update status
	// Set both conditions atomically before updating status on success
	degradedChanged := status.SetCondition(v1alpha1.Degraded, metav1.ConditionFalse, v1alpha1.ReasonReady, "")
	readyChanged := status.SetCondition(v1alpha1.Ready, metav1.ConditionTrue, v1alpha1.ReasonReady, "reconciliation successful")

	if degradedChanged || readyChanged {
		log.V(2).Info("updating conditions on successful reconciliation",
			"degradedChanged", degradedChanged,
			"readyChanged", readyChanged)
		errUpdate = updateConditionFn(nil)
	}
	return ctrl.Result{}, errUpdate
}
