package istiocsr

import (
	"fmt"
	"maps"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

func (r *Reconciler) reconcileIstioCSRDeployment(istiocsr *v1alpha1.IstioCSR, istioCSRCreateRecon bool) error {
	if err := validateIstioCSRConfig(istiocsr); err != nil {
		return NewIrrecoverableError(err, "%s/%s configuration validation failed", istiocsr.GetNamespace(), istiocsr.GetName())
	}

	resourceLabels := r.buildResourceLabels(istiocsr)

	if err := r.reconcileAllResources(istiocsr, resourceLabels, istioCSRCreateRecon); err != nil {
		return fmt.Errorf("failed to reconcile all resources: %w", err)
	}

	if err := r.updateProcessedAnnotation(istiocsr); err != nil {
		return fmt.Errorf("failed to update processed annotation: %w", err)
	}

	r.log.V(logVerbosityLevelDebug).Info("finished reconciliation of istiocsr", "namespace", istiocsr.GetNamespace(), "name", istiocsr.GetName())
	return nil
}

func (r *Reconciler) buildResourceLabels(istiocsr *v1alpha1.IstioCSR) map[string]string {
	resourceLabels := make(map[string]string)
	if istiocsr.Spec.ControllerConfig != nil && len(istiocsr.Spec.ControllerConfig.Labels) != 0 {
		maps.Copy(resourceLabels, istiocsr.Spec.ControllerConfig.Labels)
	}
	maps.Copy(resourceLabels, controllerDefaultResourceLabels)
	return resourceLabels
}

func (r *Reconciler) reconcileAllResources(istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string, istioCSRCreateRecon bool) error {
	reconcileFuncs := []struct {
		name string
		fn   func() error
	}{
		{"network policy", func() error { return r.createOrApplyNetworkPolicies(istiocsr, resourceLabels, istioCSRCreateRecon) }},
		{"service", func() error { return r.createOrApplyServices(istiocsr, resourceLabels, istioCSRCreateRecon) }},
		{"serviceaccount", func() error { return r.createOrApplyServiceAccounts(istiocsr, resourceLabels, istioCSRCreateRecon) }},
		{"rbac", func() error { return r.createOrApplyRBACResource(istiocsr, resourceLabels, istioCSRCreateRecon) }},
		{"certificate", func() error { return r.createOrApplyCertificates(istiocsr, resourceLabels, istioCSRCreateRecon) }},
		{"deployment", func() error { return r.createOrApplyDeployments(istiocsr, resourceLabels, istioCSRCreateRecon) }},
	}

	for _, reconcileFunc := range reconcileFuncs {
		if err := reconcileFunc.fn(); err != nil {
			r.log.Error(err, fmt.Sprintf("failed to reconcile %s resource", reconcileFunc.name))
			return fmt.Errorf("failed to reconcile %s resource: %w", reconcileFunc.name, err)
		}
	}
	return nil
}

func (r *Reconciler) updateProcessedAnnotation(istiocsr *v1alpha1.IstioCSR) error {
	if addProcessedAnnotation(istiocsr) {
		if err := r.UpdateWithRetry(r.ctx, istiocsr); err != nil {
			return fmt.Errorf("failed to update processed annotation to %s/%s: %w", istiocsr.GetNamespace(), istiocsr.GetName(), err)
		}
	}
	return nil
}
