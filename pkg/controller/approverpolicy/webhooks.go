package approverpolicy

import (
	"context"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func (r *Reconciler) createOrApplyValidatingWebhookConfiguration(ctx context.Context, instance *v1alpha1.ApproverPolicyManager) error {
	return r.applyResource(ctx, instance, getWebhookObject(instance))
}

func getWebhookObject(instance *v1alpha1.ApproverPolicyManager) *admissionregistrationv1.ValidatingWebhookConfiguration {
	obj := common.DecodeObjBytes[*admissionregistrationv1.ValidatingWebhookConfiguration](codecs, admissionregistrationv1.SchemeGroupVersion, assets.MustAsset(assetName("validatingwebhookconfiguration", operandName)))
	prepareResource(instance, obj)
	return obj
}
