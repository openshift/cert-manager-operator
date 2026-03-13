package trustmanager

import (
	"fmt"
	"os"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
)

func (r *Reconciler) reconcileTrustManagerDeployment(trustManager *v1alpha1.TrustManager) error {
	if err := validateTrustManagerConfig(trustManager); err != nil {
		return common.NewIrrecoverableError(err, "%s configuration validation failed", trustManager.GetName())
	}

	resourceLabels := getResourceLabels(trustManager)
	resourceAnnotations := getResourceAnnotations(trustManager)

	trustNamespace := getTrustNamespace(trustManager)
	if err := r.validateTrustNamespace(trustNamespace); err != nil {
		return common.NewIrrecoverableError(err, "trust namespace %q validation failed", trustNamespace)
	}

	if err := r.createOrApplyServiceAccounts(trustManager, resourceLabels, resourceAnnotations); err != nil {
		r.log.Error(err, "failed to reconcile serviceaccount resource")
		return err
	}

	if err := r.createOrApplyRBACResources(trustManager, resourceLabels, resourceAnnotations, trustNamespace); err != nil {
		r.log.Error(err, "failed to reconcile RBAC resources")
		return err
	}

	if err := r.createOrApplyServices(trustManager, resourceLabels, resourceAnnotations); err != nil {
		r.log.Error(err, "failed to reconcile service resources")
		return err
	}

	if err := r.createOrApplyIssuer(trustManager, resourceLabels, resourceAnnotations); err != nil {
		r.log.Error(err, "failed to reconcile issuer resource")
		return err
	}

	if err := r.createOrApplyCertificate(trustManager, resourceLabels, resourceAnnotations); err != nil {
		r.log.Error(err, "failed to reconcile certificate resource")
		return err
	}

	if err := r.createOrApplyDeployment(trustManager, resourceLabels, resourceAnnotations); err != nil {
		r.log.Error(err, "failed to reconcile deployment resource")
		return err
	}

	if err := r.createOrApplyValidatingWebhookConfiguration(trustManager, resourceLabels, resourceAnnotations); err != nil {
		r.log.Error(err, "failed to reconcile validatingwebhookconfiguration resource")
		return err
	}

	if err := r.updateStatusObservedState(trustManager); err != nil {
		return fmt.Errorf("failed to update status observed state: %w", err)
	}

	if addProcessedAnnotation(trustManager) {
		if err := r.UpdateWithRetry(r.ctx, trustManager); err != nil {
			return fmt.Errorf("failed to update processed annotation to %s: %w", trustManager.GetName(), err)
		}
	}

	r.log.V(4).Info("finished reconciliation of trustmanager", "name", trustManager.GetName())
	return nil
}

// validateTrustNamespace validates that the trust namespace exists.
func (r *Reconciler) validateTrustNamespace(namespace string) error {
	exists, err := r.namespaceExists(namespace)
	if err != nil {
		return fmt.Errorf("failed to check if namespace %q exists: %w", namespace, err)
	}
	if !exists {
		return fmt.Errorf("trust namespace %q does not exist, create the namespace before creating TrustManager CR", namespace)
	}
	return nil
}

// updateStatusObservedState populates and persists the TrustManager status with the observed state.
// Returns nil if no changes were needed, otherwise returns an error if the update fails.
func (r *Reconciler) updateStatusObservedState(trustManager *v1alpha1.TrustManager) error {
	changed := false

	if image := os.Getenv(trustManagerImageNameEnvVarName); trustManager.Status.TrustManagerImage != image {
		trustManager.Status.TrustManagerImage = image
		changed = true
	}

	if ns := getTrustNamespace(trustManager); trustManager.Status.TrustNamespace != ns {
		trustManager.Status.TrustNamespace = ns
		changed = true
	}

	if policy := trustManager.Spec.TrustManagerConfig.SecretTargets.Policy; trustManager.Status.SecretTargetsPolicy != policy {
		trustManager.Status.SecretTargetsPolicy = policy
		changed = true
	}

	if policy := trustManager.Spec.TrustManagerConfig.DefaultCAPackage.Policy; trustManager.Status.DefaultCAPackagePolicy != policy {
		trustManager.Status.DefaultCAPackagePolicy = policy
		changed = true
	}

	if policy := trustManager.Spec.TrustManagerConfig.FilterExpiredCertificates; trustManager.Status.FilterExpiredCertificatesPolicy != policy {
		trustManager.Status.FilterExpiredCertificatesPolicy = policy
		changed = true
	}

	if !changed {
		return nil
	}

	return r.updateStatus(r.ctx, trustManager)
}
