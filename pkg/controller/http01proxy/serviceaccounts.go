package http01proxy

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func (r *Reconciler) createOrApplyServiceAccount(ctx context.Context, proxy *v1alpha1.HTTP01Proxy, resourceLabels map[string]string) error {
	sa := common.DecodeObjBytes[*corev1.ServiceAccount](codecs, corev1.SchemeGroupVersion, assets.MustAsset(serviceAccountAssetName))
	sa.SetNamespace(proxy.GetNamespace())
	common.UpdateResourceLabels(sa, resourceLabels)
	return r.createOrUpdateResource(ctx, sa)
}

func (r *Reconciler) deleteServiceAccount(ctx context.Context, proxy *v1alpha1.HTTP01Proxy) error {
	return r.deleteIfExists(ctx, &corev1.ServiceAccount{}, client.ObjectKey{
		Namespace: proxy.GetNamespace(),
		Name:      http01proxyCommonName,
	})
}
