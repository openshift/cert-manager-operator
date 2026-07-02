package istiocsr

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func (r *Reconciler) createOrApplyServiceAccounts(istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string, istioCSRCreateRecon bool) error {
	desired := r.getServiceAccountObject(istiocsr, resourceLabels)
	if err := r.reconcileNamespacedObject(istiocsr, desired, &corev1.ServiceAccount{}, "reconciling serviceaccount resource", "serviceaccount resource", istioCSRCreateRecon); err != nil {
		return err
	}

	if err := r.updateServiceAccountNameInStatus(istiocsr, desired); err != nil {
		serviceAccountName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
		return common.FromClientError(err, "failed to update %s/%s istiocsr status with %s serviceaccount resource name", istiocsr.GetNamespace(), istiocsr.GetName(), serviceAccountName)
	}
	return nil
}

func (r *Reconciler) getServiceAccountObject(istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string) *corev1.ServiceAccount {
	serviceAccount := decodeServiceAccountObjBytes(assets.MustAsset(serviceAccountAssetName))
	common.UpdateNamespace(serviceAccount, istiocsr.GetNamespace())
	common.UpdateResourceLabels(serviceAccount, resourceLabels)
	return serviceAccount
}

func (r *Reconciler) updateServiceAccountNameInStatus(istiocsr *v1alpha1.IstioCSR, serviceAccount *corev1.ServiceAccount) error {
	if istiocsr.Status.ServiceAccount == serviceAccount.GetName() {
		return nil
	}
	istiocsr.Status.ServiceAccount = serviceAccount.GetName()
	return r.updateStatus(r.ctx, istiocsr)
}
