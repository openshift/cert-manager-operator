package trustmanager

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func (r *Reconciler) createOrApplyServiceAccounts(trustManager *v1alpha1.TrustManager, resourceLabels, resourceAnnotations map[string]string) error {
	desired := r.getServiceAccountObject(resourceLabels, resourceAnnotations)
	return common.ApplyResource(r.ctx, r.CtrlClient, r.log, r.eventRecorder, trustManager, desired, &corev1.ServiceAccount{}, fieldOwner, serviceAccountModified)
}

func serviceAccountModified(desired, existing *corev1.ServiceAccount) bool {
	return managedMetadataModified(desired, existing) ||
		!ptr.Equal(desired.AutomountServiceAccountToken, existing.AutomountServiceAccountToken)
}

func (r *Reconciler) getServiceAccountObject(resourceLabels, resourceAnnotations map[string]string) *corev1.ServiceAccount {
	serviceAccount := common.DecodeObjBytes[*corev1.ServiceAccount](codecs, corev1.SchemeGroupVersion, assets.MustAsset(serviceAccountAssetName))
	common.UpdateName(serviceAccount, trustManagerServiceAccountName)
	common.UpdateNamespace(serviceAccount, operandNamespace)
	common.UpdateResourceLabels(serviceAccount, resourceLabels)
	updateResourceAnnotations(serviceAccount, resourceAnnotations)

	return serviceAccount
}
