package approverpolicy

import (
	corev1 "k8s.io/api/core/v1"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func getSecretObject(instance *v1alpha1.ApproverPolicyManager) *corev1.Secret {
	obj := common.DecodeObjBytes[*corev1.Secret](codecs, corev1.SchemeGroupVersion, assets.MustAsset(assetName("secret", tlsSecretName)))
	prepareResource(instance, obj)
	// Only bootstrap metadata is owned by the operator. DynamicSource generates
	// and rotates the certificate data and may set the Secret type.
	obj.Data, obj.StringData, obj.Type = nil, nil, ""
	return obj
}
