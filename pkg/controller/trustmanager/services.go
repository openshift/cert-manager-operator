package trustmanager

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func (r *Reconciler) createOrApplyServices(trustManager *v1alpha1.TrustManager, resourceLabels map[string]string, trustManagerCreateRecon bool) error {
	serviceAssets := []string{
		webhookServiceAssetName,
		metricsServiceAssetName,
	}

	for _, assetName := range serviceAssets {
		desired := r.getServiceObject(assetName, resourceLabels)

		serviceName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
		r.log.V(4).Info("reconciling service resource", "name", serviceName)
		fetched := &corev1.Service{}
		exist, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), fetched)
		if err != nil {
			return FromClientError(err, "failed to check %s service resource already exists", serviceName)
		}

		if exist && trustManagerCreateRecon {
			r.eventRecorder.Eventf(trustManager, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s service resource already exists, maybe from previous installation", serviceName)
		}
		if exist && hasObjectChanged(desired, fetched) {
			r.log.V(1).Info("service has been modified, updating to desired state", "name", serviceName)
			if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
				return FromClientError(err, "failed to update %s service resource", serviceName)
			}
			r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "service resource %s reconciled back to desired state", serviceName)
		} else {
			r.log.V(4).Info("service resource already exists and is in expected state", "name", serviceName)
		}
		if !exist {
			if err := r.Create(r.ctx, desired); err != nil {
				return FromClientError(err, "failed to create %s service resource", serviceName)
			}
			r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "service resource %s created", serviceName)
		}
	}

	return nil
}

func (r *Reconciler) getServiceObject(assetName string, resourceLabels map[string]string) *corev1.Service {
	service := decodeServiceObjBytes(assets.MustAsset(assetName))
	updateNamespace(service, trustManagerOperandNamespace)
	updateResourceLabels(service, resourceLabels)
	return service
}
