package istiocsr

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func (r *Reconciler) createOrApplyServiceAccounts(istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string, istioCSRCreateRecon bool) error {
	desired := r.getServiceAccountObject(istiocsr, resourceLabels)

	serviceAccountName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(1).Info("reconciling serviceaccount resource", "name", serviceAccountName)
	fetched := &corev1.ServiceAccount{}
	key := types.NamespacedName{
		Name:      desired.GetName(),
		Namespace: desired.GetNamespace(),
	}
	exist, err := r.Exists(r.ctx, key, fetched)
	if err != nil {
		return FromClientError(err, "failed to check %s serviceaccount resource already exists", serviceAccountName)
	}

	if exist {
		if istioCSRCreateRecon {
			r.eventRecorder.Eventf(istiocsr, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s serviceaccount resource already exists, maybe from previous installation", serviceAccountName)
		}
		r.log.V(1).Info("serviceaccount resource already exists and is in expected state", "name", serviceAccountName)
	}
	if !exist {
		if err := r.Create(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to create %s serviceaccount resource", serviceAccountName)
		}
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeNormal, "Reconciled", "serviceaccount resource %s created", serviceAccountName)
	}

	if err := r.updateServiceAccountNameInStatus(istiocsr, desired); err != nil {
		return FromClientError(err, "failed to update %s/%s istiocsr status with %s serviceaccount resource name", istiocsr.GetNamespace(), istiocsr.GetName(), serviceAccountName)
	}
	return nil
}

func (r *Reconciler) getServiceAccountObject(istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string) *corev1.ServiceAccount {
	serviceAccount := decodeServiceAccountObjBytes(assets.MustAsset(serviceAccountAssetName))
	updateNamespace(serviceAccount, istiocsr.GetNamespace())
	updateResourceLabels(serviceAccount, resourceLabels)
	return serviceAccount
}

func (r *Reconciler) updateServiceAccountNameInStatus(istiocsr *v1alpha1.IstioCSR, serviceAccount *corev1.ServiceAccount) error {
	if istiocsr.Status.ServiceAccount == serviceAccount.GetName() {
		return nil
	}
	istiocsr.Status.ServiceAccount = serviceAccount.GetName()
	return r.updateStatus(r.ctx, istiocsr)
}
