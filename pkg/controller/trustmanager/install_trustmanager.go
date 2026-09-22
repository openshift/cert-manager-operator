package trustmanager

import (
	"fmt"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

func (r *Reconciler) reconcileTrustManagerDeployment(tm *v1alpha1.TrustManager, trustManagerCreateRecon bool) error {
	// if user has set custom labels to be added to all resources created by the controller
	// merge it with the controller's own default labels.
	resourceLabels := make(map[string]string)
	if len(tm.Spec.ControllerConfig.Labels) != 0 {
		for k, v := range tm.Spec.ControllerConfig.Labels {
			resourceLabels[k] = v
		}
	}
	for k, v := range controllerDefaultResourceLabels {
		resourceLabels[k] = v
	}

	if err := r.createOrApplyNetworkPolicies(tm, resourceLabels, trustManagerCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile network policy resources")
		return err
	}

	if err := r.createOrApplyServiceAccounts(tm, resourceLabels, trustManagerCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile serviceaccount resource")
		return err
	}

	if err := r.createOrApplyRBACResources(tm, resourceLabels, trustManagerCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile rbac resources")
		return err
	}

	if err := r.createOrApplyServices(tm, resourceLabels, trustManagerCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile service resources")
		return err
	}

	if err := r.createOrApplyWebhookIssuers(tm, resourceLabels, trustManagerCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile webhook issuer resource")
		return err
	}

	if err := r.createOrApplyCertificates(tm, resourceLabels, trustManagerCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile certificate resource")
		return err
	}

	if err := r.createOrApplyValidatingWebhookConfiguration(tm, resourceLabels, trustManagerCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile validating webhook configuration")
		return err
	}

	if err := r.reconcileDefaultCAPackage(tm, resourceLabels); err != nil {
		r.log.Error(err, "failed to reconcile default CA package")
		return err
	}

	if err := r.createOrApplyDeployments(tm, resourceLabels, trustManagerCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile deployment resource")
		return err
	}

	if err := r.updateStatusFields(tm); err != nil {
		r.log.Error(err, "failed to update status fields")
		return err
	}

	if addProcessedAnnotation(tm) {
		if err := r.UpdateWithRetry(r.ctx, tm); err != nil {
			return fmt.Errorf("failed to update processed annotation to %s: %w", tm.GetName(), err)
		}
	}

	r.log.V(4).Info("finished reconciliation of trustmanager", "name", tm.GetName())
	return nil
}

// updateStatusFields updates the status fields with current configuration state.
func (r *Reconciler) updateStatusFields(tm *v1alpha1.TrustManager) error {
	changed := false

	trustNamespace := tm.Spec.TrustManagerConfig.TrustNamespace
	if trustNamespace == "" {
		trustNamespace = defaultTrustNamespace
	}
	if tm.Status.TrustNamespace != trustNamespace {
		tm.Status.TrustNamespace = trustNamespace
		changed = true
	}

	if tm.Status.SecretTargetsPolicy != tm.Spec.TrustManagerConfig.SecretTargets.Policy {
		tm.Status.SecretTargetsPolicy = tm.Spec.TrustManagerConfig.SecretTargets.Policy
		changed = true
	}

	if tm.Status.DefaultCAPackagePolicy != tm.Spec.TrustManagerConfig.DefaultCAPackage.Policy {
		tm.Status.DefaultCAPackagePolicy = tm.Spec.TrustManagerConfig.DefaultCAPackage.Policy
		changed = true
	}

	if tm.Status.FilterExpiredCertificatesPolicy != tm.Spec.TrustManagerConfig.FilterExpiredCertificates {
		tm.Status.FilterExpiredCertificatesPolicy = tm.Spec.TrustManagerConfig.FilterExpiredCertificates
		changed = true
	}

	if changed {
		return r.updateStatus(r.ctx, tm)
	}
	return nil
}
