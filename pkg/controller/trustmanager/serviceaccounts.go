package trustmanager

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func (r *Reconciler) createOrApplyServiceAccounts(trustManager *v1alpha1.TrustManager, resourceLabels map[string]string) error {
	desired := r.getServiceAccountObject(resourceLabels)
	serviceAccountName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(4).Info("reconciling serviceaccount resource", "name", serviceAccountName)

	// Server-Side Apply: patches only our fields
	if err := r.Patch(r.ctx, desired, client.Apply, client.FieldOwner(fieldOwner), client.ForceOwnership); err != nil {
		return common.FromClientError(err, "failed to apply serviceaccount %q", serviceAccountName)
	}

	r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "serviceaccount resource %s applied", serviceAccountName)
	r.log.V(2).Info("applied serviceaccount", "name", serviceAccountName)
	return nil
}

func (r *Reconciler) getServiceAccountObject(resourceLabels map[string]string) *corev1.ServiceAccount {
	serviceAccount := decodeServiceAccountObjBytes(assets.MustAsset(serviceAccountAssetName))
	common.UpdateNamespace(serviceAccount, operandNamespace)
	common.UpdateResourceLabels(serviceAccount, resourceLabels)

	return serviceAccount
}
