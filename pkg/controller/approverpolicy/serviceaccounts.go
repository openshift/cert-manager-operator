package approverpolicy

import (
	corev1 "k8s.io/api/core/v1"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func getServiceAccountObject(instance *v1alpha1.ApproverPolicyManager) *corev1.ServiceAccount {
	obj := common.DecodeObjBytes[*corev1.ServiceAccount](codecs, corev1.SchemeGroupVersion, assets.MustAsset(assetName("serviceaccount", operandName)))
	prepareResource(instance, obj)
	return obj
}
