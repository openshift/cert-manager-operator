package approverpolicy

import (
	"context"

	corev1 "k8s.io/api/core/v1"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func (r *Reconciler) createOrApplySecret(ctx context.Context, instance *v1alpha1.ApproverPolicyManager) error {
	return r.applyResource(ctx, instance, getSecretObject(instance))
}

func getSecretObject(instance *v1alpha1.ApproverPolicyManager) *corev1.Secret {
	// The bootstrap manifest contains only metadata. DynamicSource generates
	// and rotates the certificate data and may set the Secret type.
	obj := common.DecodeObjBytes[*corev1.Secret](codecs, corev1.SchemeGroupVersion, assets.MustAsset(assetName("secret", tlsSecretName)))
	prepareResource(instance, obj)
	return obj
}
