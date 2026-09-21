package http01proxy

import (
	"context"
	"fmt"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
)

func (r *Reconciler) reconcileHTTP01ProxyDeployment(ctx context.Context, proxy *v1alpha1.HTTP01Proxy) error {
	info, err := r.discoverPlatform(ctx)
	if err != nil {
		return common.NewRetryRequiredError(err, "failed to discover platform")
	}

	if err := validatePlatform(info); err != nil {
		r.log.V(1).Info("platform not supported for HTTP01 proxy", "platformType", info.platformType)
		if cleanupErr := r.cleanUp(ctx, proxy); cleanupErr != nil {
			return common.NewRetryRequiredError(cleanupErr, "failed to clean up resources after platform validation failure")
		}
		return common.NewIrrecoverableError(err, "platform validation failed")
	}

	if err := r.createOrApplyMachineConfig(ctx, info); err != nil {
		r.log.Error(err, "failed to reconcile DNAT MachineConfig")
		return err
	}

	if common.AddAnnotation(proxy, controllerProcessedAnnotation, "true") {
		if err := r.UpdateWithRetry(ctx, proxy); err != nil {
			return fmt.Errorf("failed to update processed annotation to %s: %w", proxy.GetName(), err)
		}
	}

	r.log.V(4).Info("finished reconciliation of http01proxy", "name", proxy.GetName())
	return nil
}
