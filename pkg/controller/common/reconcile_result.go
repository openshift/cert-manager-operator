package common

import (
	"fmt"
	"time"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

// resolveConditionReason extracts a specific condition reason from the error
// chain if one was set via WithConditionReason, otherwise returns the default.
func resolveConditionReason(err error, defaultReason string) string {
	if reason := GetConditionReason(err); reason != "" {
		return reason
	}
	return defaultReason
}

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
			reason := resolveConditionReason(reconcileErr, v1alpha1.ReasonFailed)
			degradedChanged := status.SetCondition(v1alpha1.Degraded, metav1.ConditionTrue, reason, fmt.Sprintf("reconciliation failed with irrecoverable error not retrying: %v", reconcileErr))
			readyChanged := status.SetCondition(v1alpha1.Ready, metav1.ConditionFalse, reason, "")
			progressingChanged := status.SetCondition(v1alpha1.Progressing, metav1.ConditionFalse, reason, "")

			if degradedChanged || readyChanged || progressingChanged {
				log.V(2).Info("updating conditions on irrecoverable error",
					"degradedChanged", degradedChanged,
					"readyChanged", readyChanged,
					"progressingChanged", progressingChanged,
					"reason", reason,
					"error", reconcileErr)
				errUpdate = updateConditionFn(nil)
			}
			return ctrl.Result{}, errUpdate
		}

		readyReason := resolveConditionReason(reconcileErr, v1alpha1.ReasonInProgress)
		progressingReason := resolveConditionReason(reconcileErr, v1alpha1.ReasonReconciling)
		degradedChanged := status.SetCondition(v1alpha1.Degraded, metav1.ConditionFalse, v1alpha1.ReasonReady, "")
		readyChanged := status.SetCondition(v1alpha1.Ready, metav1.ConditionFalse, readyReason, fmt.Sprintf("reconciliation failed, retrying: %v", reconcileErr))
		progressingChanged := status.SetCondition(v1alpha1.Progressing, metav1.ConditionTrue, progressingReason, fmt.Sprintf("reconciliation in progress: %v", reconcileErr))

		if degradedChanged || readyChanged || progressingChanged {
			log.V(2).Info("updating conditions on recoverable error",
				"degradedChanged", degradedChanged,
				"readyChanged", readyChanged,
				"progressingChanged", progressingChanged,
				"reason", readyReason,
				"error", reconcileErr)
			errUpdate = updateConditionFn(reconcileErr)
		}
		if errUpdate != nil {
			return ctrl.Result{}, errUpdate
		}
		return ctrl.Result{RequeueAfter: requeueDuration}, nil
	}

	degradedChanged := status.SetCondition(v1alpha1.Degraded, metav1.ConditionFalse, v1alpha1.ReasonReady, "")
	readyChanged := status.SetCondition(v1alpha1.Ready, metav1.ConditionTrue, v1alpha1.ReasonReady, "reconciliation successful")
	progressingChanged := status.SetCondition(v1alpha1.Progressing, metav1.ConditionFalse, v1alpha1.ReasonReady, "")

	if degradedChanged || readyChanged || progressingChanged {
		log.V(2).Info("updating conditions on successful reconciliation",
			"degradedChanged", degradedChanged,
			"readyChanged", readyChanged,
			"progressingChanged", progressingChanged)
		errUpdate = updateConditionFn(nil)
	}
	return ctrl.Result{}, errUpdate
}
