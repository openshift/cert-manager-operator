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

	// Determine the target namespace for trust-manager deployment.
	// trust-manager is always deployed in the cert-manager namespace.
	trustNamespace := tm.Spec.TrustManagerConfig.TrustNamespace
	if trustNamespace == "" {
		trustNamespace = trustManagerDefaultNamespace
	}

	if err := r.createOrApplyServiceAccounts(tm, trustNamespace, resourceLabels, trustManagerCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile serviceaccount resource")
		return err
	}

	if err := r.createOrApplyRBACResource(tm, trustNamespace, resourceLabels, trustManagerCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile rbac resources")
		return err
	}

	if err := r.createOrApplyServices(tm, trustNamespace, resourceLabels, trustManagerCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile service resource")
		return err
	}

	if err := r.createOrApplyDeployments(tm, trustNamespace, resourceLabels, trustManagerCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile deployment resource")
		return err
	}

	if addProcessedAnnotation(tm) {
		if err := r.UpdateWithRetry(r.ctx, tm); err != nil {
			return fmt.Errorf("failed to update processed annotation to %s: %w", tm.GetName(), err)
		}
	}

	// Update status with observed configuration
	r.updateStatusFields(tm)

	r.log.V(4).Info("finished reconciliation of trustmanager", "name", tm.GetName())
	return nil
}

// updateStatusFields updates the status fields with the observed configuration.
func (r *Reconciler) updateStatusFields(tm *v1alpha1.TrustManager) {
	trustNamespace := tm.Spec.TrustManagerConfig.TrustNamespace
	if trustNamespace == "" {
		trustNamespace = trustManagerDefaultNamespace
	}

	tm.Status.TrustNamespace = trustNamespace
	tm.Status.SecretTargetsPolicy = tm.Spec.TrustManagerConfig.SecretTargets.Policy
	tm.Status.DefaultCAPackagePolicy = tm.Spec.TrustManagerConfig.DefaultCAPackage.Policy
	tm.Status.FilterExpiredCertificatesPolicy = tm.Spec.TrustManagerConfig.FilterExpiredCertificates
}
