package trustmanager

import (
	"fmt"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

func (r *Reconciler) reconcileTrustManagerDeployment(trustManager *v1alpha1.TrustManager, trustManagerCreateRecon bool) error {
	if err := validateTrustManagerConfig(trustManager); err != nil {
		return NewIrrecoverableError(err, "%s configuration validation failed", trustManager.GetName())
	}

	// if user has set custom labels to be added to all resources created by the controller
	// merge it with the controller's own default labels.
	resourceLabels := make(map[string]string)
	if len(trustManager.Spec.ControllerConfig.Labels) != 0 {
		for k, v := range trustManager.Spec.ControllerConfig.Labels {
			resourceLabels[k] = v
		}
	}
	for k, v := range controllerDefaultResourceLabels {
		resourceLabels[k] = v
	}

	if err := r.createOrApplyServiceAccounts(trustManager, resourceLabels, trustManagerCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile serviceaccount resource")
		return err
	}

	if err := r.createOrApplyRBACResources(trustManager, resourceLabels, trustManagerCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile rbac resources")
		return err
	}

	if err := r.createOrApplyServices(trustManager, resourceLabels, trustManagerCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile service resources")
		return err
	}

	if err := r.createOrApplyCertificates(trustManager, resourceLabels, trustManagerCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile certificate resource")
		return err
	}

	if err := r.createOrApplyDeployments(trustManager, resourceLabels, trustManagerCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile deployment resource")
		return err
	}

	if err := r.createOrApplyValidatingWebhookConfigurations(trustManager, resourceLabels, trustManagerCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile validatingwebhookconfiguration resource")
		return err
	}

	if trustManager.Spec.TrustManagerConfig.DefaultCAPackage.Policy == v1alpha1.DefaultCAPackagePolicyEnabled {
		if err := r.createOrApplyDefaultCAPackage(trustManager, resourceLabels, trustManagerCreateRecon); err != nil {
			r.log.Error(err, "failed to reconcile default CA package")
			return err
		}
	}

	// Update status with current config policies
	updateStatusPolicies(trustManager)

	if addProcessedAnnotation(trustManager) {
		if err := r.UpdateWithRetry(r.ctx, trustManager); err != nil {
			return fmt.Errorf("failed to update processed annotation to %s: %w", trustManager.GetName(), err)
		}
	}

	r.log.V(4).Info("finished reconciliation of trustmanager", "name", trustManager.GetName())
	return nil
}
