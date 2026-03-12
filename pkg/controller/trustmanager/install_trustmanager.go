package trustmanager

import (
	"errors"
	"fmt"
	"os"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
)

var errTrustNamespaceNotFound = errors.New("trust namespace does not exist, create the namespace before creating TrustManager CR")

func (r *Reconciler) reconcileTrustManagerDeployment(trustManager *v1alpha1.TrustManager, trustManagerCreateRecon bool) error {
	if err := validateTrustManagerConfig(trustManager); err != nil {
		return common.NewIrrecoverableError(err, "%s configuration validation failed", trustManager.GetName())
	}

	resourceLabels := getResourceLabels(trustManager)

	// Validate trust namespace exists
	trustNamespace := getTrustNamespace(trustManager)
	if err := r.validateTrustNamespace(trustNamespace); err != nil {
		return common.NewIrrecoverableError(err, "trust namespace %q validation failed", trustNamespace)
	}

	// TODO: Reconcile all trust-manager resources
	// For now, just reconcile ServiceAccount to verify controller is working
	if err := r.createOrApplyServiceAccounts(trustManager, resourceLabels); err != nil {
		r.log.Error(err, "failed to reconcile serviceaccount resource")
		return err
	}

	// TODO: As implementation extends, move status field updates inline within each resource reconciler
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
		return fmt.Errorf("trust namespace %q: %w", namespace, errTrustNamespaceNotFound)
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
