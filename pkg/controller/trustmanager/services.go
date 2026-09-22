package trustmanager

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func (r *Reconciler) createOrApplyServices(tm *v1alpha1.TrustManager, resourceLabels map[string]string, trustManagerCreateRecon bool) error {
	serviceAssets := []string{webhookServiceAssetName, metricsServiceAssetName}
	for _, assetName := range serviceAssets {
		if err := r.createOrApplyService(tm, assetName, resourceLabels, trustManagerCreateRecon); err != nil {
			return err
		}
	}
	return nil
}

func (r *Reconciler) createOrApplyService(tm *v1alpha1.TrustManager, assetName string, resourceLabels map[string]string, trustManagerCreateRecon bool) error {
	desired := r.getServiceObject(tm, assetName, resourceLabels)

	serviceName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(4).Info("reconciling service resource", "name", serviceName)
	fetched := &corev1.Service{}
	exist, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), fetched)
	if err != nil {
		return FromClientError(err, "failed to check %s service resource already exists", serviceName)
	}

	if exist && trustManagerCreateRecon {
		r.eventRecorder.Eventf(tm, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s service resource already exists, maybe from previous installation", serviceName)
	}
	if exist && hasObjectChanged(desired, fetched) {
		r.log.V(1).Info("service has been modified, updating to desired state", "name", serviceName)
		if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to update %s service resource", serviceName)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "service resource %s reconciled back to desired state", serviceName)
	} else {
		r.log.V(4).Info("service resource already exists and is in expected state", "name", serviceName)
	}
	if !exist {
		if err := r.Create(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to create %s service resource", serviceName)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "service resource %s created", serviceName)
	}

	return nil
}

func (r *Reconciler) getServiceObject(tm *v1alpha1.TrustManager, assetName string, resourceLabels map[string]string) *corev1.Service {
	service := decodeServiceObjBytes(assets.MustAsset(assetName))
	trustNamespace := tm.Spec.TrustManagerConfig.TrustNamespace
	if trustNamespace == "" {
		trustNamespace = defaultTrustNamespace
	}
	updateNamespace(service, trustNamespace)
	updateResourceLabels(service, resourceLabels)
	return service
}
