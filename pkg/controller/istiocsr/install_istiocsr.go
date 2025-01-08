package istiocsr

import (
	"fmt"
	"reflect"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

func (r *Reconciler) reconcileIstioCSRDeployment(istiocsr *v1alpha1.IstioCSR) error {
	if err := r.validateIstioCSRConfig(istiocsr); err != nil {
		return NewIrrecoverableError(err, "%s/%s configuration validation failed", istiocsr.GetNamespace(), istiocsr.GetName())
	}

	// if user has set custom labels to be added to all resources created by the controller
	// merge it with the controller's own default labels.
	resourceLabels := make(map[string]string)
	if istiocsr.Spec.ControllerConfig != nil && len(istiocsr.Spec.ControllerConfig.Labels) != 0 {
		for k, v := range istiocsr.Spec.ControllerConfig.Labels {
			resourceLabels[k] = v
		}
	}
	for k, v := range controllerDefaultResourceLabels {
		resourceLabels[k] = v
	}

	istioCSRCreateRecon := false
	if !containsProcessedAnnotation(istiocsr) || reflect.DeepEqual(istiocsr.Status, v1alpha1.IstioCSRStatus{}) {
		r.log.V(1).Info("starting reconciliation of newly created istiocsr", "namespace", istiocsr.GetNamespace(), "name", istiocsr.GetName())
		istioCSRCreateRecon = true
	}

	if err := r.createOrApplyServices(istiocsr, resourceLabels, istioCSRCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile service resource")
		return err
	}

	if err := r.createOrApplyServiceAccounts(istiocsr, resourceLabels, istioCSRCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile serviceaccount resource")
		return err
	}

	if err := r.createOrApplyRBACResource(istiocsr, resourceLabels, istioCSRCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile rbac resources")
		return err
	}

	if err := r.createOrApplyCertificates(istiocsr, resourceLabels, istioCSRCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile certificate resource")
		return err
	}

	if err := r.createOrApplyDeployments(istiocsr, resourceLabels, istioCSRCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile deployment resource")
		return err
	}

	addProcessedAnnotation(istiocsr)
	if err := r.UpdateWithRetry(r.ctx, istiocsr); err != nil {
		return fmt.Errorf("failed to update processed annotation to %s/%s: %w", istiocsr.GetNamespace(), istiocsr.GetName(), err)
	}

	r.log.V(1).Info("finished reconciliation of istiocsr", "namespace", istiocsr.GetNamespace(), "name", istiocsr.GetName())
	return nil
}

func (r *Reconciler) validateIstioCSRConfig(istiocsr *v1alpha1.IstioCSR) error {
	return nil
}
