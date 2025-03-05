package certmanager

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

const (
	tpConditionType = "TechPreviewFeaturesEnabled"

	tpConditionReasonUsed       = "UnsupportedFeaturesUsed"
	tpConditionReasonAsExpected = "AsExpected"

	tpWarningMessage = "UnsupportedFeatures were set during the lifecycle of this operator, " +
		"Technology Preview features are not supported with Red Hat production service level agreements (SLAs) and might not be functionally complete. " +
		"Red Hat does not recommend using them in production."
)

func alreadyInDesiredTPConditionState(object *v1alpha1.CertManager, needsTechPreview bool) bool {
	var desiredState = apiv1.ConditionFalse
	if needsTechPreview {
		desiredState = apiv1.ConditionTrue
	}

	for _, cond := range object.Status.Conditions {
		if cond.Type == tpConditionType && cond.Status == desiredState {
			return true
		}
	}

	return false
}

func (r *CertManagerReconciler) setStatusWithTPCondition(ctx context.Context, currentObj *v1alpha1.CertManager, needsTechPreview bool) (updated bool, err error) {
	var desiredState = apiv1.ConditionFalse
	if needsTechPreview {
		desiredState = apiv1.ConditionTrue
	}

	var cond apiv1.OperatorCondition
	var idx int
	var exists bool

	for idx, cond = range currentObj.Status.Conditions {
		if cond.Type == tpConditionType {

			// no-op: condition is already in desired state
			if cond.Status == desiredState {
				return false, nil
			}

			exists = true
			break
		}
	}

	cond = apiv1.OperatorCondition{
		Type:               tpConditionType,
		Status:             desiredState,
		LastTransitionTime: metav1.NewTime(r.clock.Now()),

		Reason: tpConditionReasonAsExpected,
	}

	if needsTechPreview {
		cond.Reason = tpConditionReasonUsed
		cond.Message = tpWarningMessage
	}

	desiredObj := currentObj.DeepCopy()
	if exists {
		desiredObj.Status.Conditions[idx] = cond
	} else {
		desiredObj.Status.Conditions = append(desiredObj.Status.Conditions, cond)
	}

	return true, r.Status().Update(ctx, desiredObj)
}
