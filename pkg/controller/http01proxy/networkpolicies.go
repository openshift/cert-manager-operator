package http01proxy

import (
	"context"
	"fmt"

	networkingv1 "k8s.io/api/networking/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func (r *Reconciler) createOrApplyNetworkPolicies(ctx context.Context, proxy *v1alpha1.HTTP01Proxy, resourceLabels map[string]string) error {
	for _, assetName := range http01ProxyNetworkPolicyAssets {
		np := common.DecodeObjBytes[*networkingv1.NetworkPolicy](codecs, networkingv1.SchemeGroupVersion, assets.MustAsset(assetName))
		np.SetNamespace(proxy.GetNamespace())
		common.UpdateResourceLabels(np, resourceLabels)

		if err := r.createOrUpdateResource(ctx, np); err != nil {
			return fmt.Errorf("failed to reconcile network policy %s: %w", assetName, err)
		}
	}
	return nil
}

func (r *Reconciler) deleteNetworkPolicies(ctx context.Context, proxy *v1alpha1.HTTP01Proxy) error {
	for _, assetName := range http01ProxyNetworkPolicyAssets {
		np := common.DecodeObjBytes[*networkingv1.NetworkPolicy](codecs, networkingv1.SchemeGroupVersion, assets.MustAsset(assetName))
		key := client.ObjectKey{Namespace: proxy.GetNamespace(), Name: np.GetName()}
		if err := r.deleteIfExists(ctx, &networkingv1.NetworkPolicy{}, key); err != nil {
			return fmt.Errorf("failed to delete network policy %q: %w", key.Name, err)
		}
	}
	return nil
}
