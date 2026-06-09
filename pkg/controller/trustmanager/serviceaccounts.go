package trustmanager

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func (r *Reconciler) createOrApplyServiceAccounts(tm *v1alpha1.TrustManager, trustNamespace string, resourceLabels map[string]string, trustManagerCreateRecon bool) error {
	desired := r.getServiceAccountObject(trustNamespace, resourceLabels)

	serviceAccountName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(4).Info("reconciling serviceaccount resource", "name", serviceAccountName)
	fetched := &corev1.ServiceAccount{}
	exist, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), fetched)
	if err != nil {
		return FromClientError(err, "failed to check %s serviceaccount resource already exists", serviceAccountName)
	}

	if exist {
		if trustManagerCreateRecon {
			r.eventRecorder.Eventf(tm, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s serviceaccount resource already exists, maybe from previous installation", serviceAccountName)
		}
		r.log.V(4).Info("serviceaccount resource already exists and is in expected state", "name", serviceAccountName)
	}
	if !exist {
		if err := r.Create(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to create %s serviceaccount resource", serviceAccountName)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "serviceaccount resource %s created", serviceAccountName)
	}

	return nil
}

func (r *Reconciler) getServiceAccountObject(trustNamespace string, resourceLabels map[string]string) *corev1.ServiceAccount {
	serviceAccount := decodeServiceAccountObjBytes(assets.MustAsset(serviceAccountAssetName))
	updateNamespace(serviceAccount, trustNamespace)
	updateResourceLabels(serviceAccount, resourceLabels)
	return serviceAccount
}
