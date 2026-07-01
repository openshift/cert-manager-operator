package http01proxy

import (
	"context"
	"fmt"
	"maps"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
)

func (r *Reconciler) reconcileHTTP01ProxyDeployment(ctx context.Context, proxy *v1alpha1.HTTP01Proxy) error {
	info, err := r.getOrDiscoverPlatform(ctx)
	if err != nil {
		return common.NewRetryRequiredError(err, "failed to discover platform")
	}

	if reason := validatePlatform(info); reason != "" {
		r.log.V(1).Info("platform not supported for HTTP01 proxy", "reason", reason, "platformType", info.platformType)
		if err := r.cleanUp(ctx, proxy); err != nil {
			r.log.Error(err, "failed to clean up resources after platform validation failure")
		}
		return common.NewIrrecoverableError(fmt.Errorf("platform validation failed"), "%s", reason)
	}

	resourceLabels := make(map[string]string)
	maps.Copy(resourceLabels, controllerDefaultResourceLabels)

	if err := r.createOrApplyNetworkPolicies(ctx, proxy, resourceLabels); err != nil {
		r.log.Error(err, "failed to reconcile network policy resources")
		return err
	}

	if err := r.createOrApplyServiceAccount(ctx, proxy, resourceLabels); err != nil {
		r.log.Error(err, "failed to reconcile serviceaccount resource")
		return err
	}

	if err := r.createOrApplyRBACResources(ctx, proxy, resourceLabels); err != nil {
		r.log.Error(err, "failed to reconcile rbac resources")
		return err
	}

	if err := r.createOrApplyDaemonSet(ctx, proxy, resourceLabels); err != nil {
		r.log.Error(err, "failed to reconcile daemonset resource")
		return err
	}

	if common.AddAnnotation(proxy, controllerProcessedAnnotation, "true") {
		if err := r.UpdateWithRetry(ctx, proxy); err != nil {
			return fmt.Errorf("failed to update processed annotation to %s/%s: %w", proxy.GetNamespace(), proxy.GetName(), err)
		}
	}

	r.log.V(4).Info("finished reconciliation of http01proxy", "namespace", proxy.GetNamespace(), "name", proxy.GetName())
	return nil
}
