package trustmanager

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func (r *Reconciler) createOrApplyServiceAccounts(trustManager *v1alpha1.TrustManager, resourceLabels, resourceAnnotations map[string]string) error {
	desired := r.getServiceAccountObject(resourceLabels, resourceAnnotations)
	serviceAccountName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(4).Info("reconciling serviceaccount resource", "name", serviceAccountName)

	existing := &corev1.ServiceAccount{}
	exists, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), existing)
	if err != nil {
		return common.FromClientError(err, "failed to check if serviceaccount %q exists", serviceAccountName)
	}
	if exists && !serviceAccountModified(desired, existing) {
		r.log.V(4).Info("serviceaccount already matches desired state, skipping apply", "name", serviceAccountName)
		return nil
	}

	if err := r.Patch(r.ctx, desired, client.Apply, client.FieldOwner(fieldOwner), client.ForceOwnership); err != nil {
		return common.FromClientError(err, "failed to apply serviceaccount %q", serviceAccountName)
	}

	r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "serviceaccount resource %s applied", serviceAccountName)
	r.log.V(2).Info("applied serviceaccount", "name", serviceAccountName)
	return nil
}

// serviceAccountModified compares only the fields we manage via SSA.
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
