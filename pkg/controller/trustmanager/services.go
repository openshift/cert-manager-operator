package trustmanager

import (
	"maps"
	"reflect"

	corev1 "k8s.io/api/core/v1"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func (r *Reconciler) createOrApplyServices(trustManager *v1alpha1.TrustManager, resourceLabels, resourceAnnotations map[string]string) error {
	if err := r.createOrApplyService(trustManager, getWebhookServiceObject(resourceLabels, resourceAnnotations)); err != nil {
		return err
	}
	if err := r.createOrApplyService(trustManager, getMetricsServiceObject(resourceLabels, resourceAnnotations)); err != nil {
		return err
	}
	return nil
}

func (r *Reconciler) createOrApplyService(trustManager *v1alpha1.TrustManager, desired *corev1.Service) error {
	return common.ApplyResource(r.ctx, r.CtrlClient, r.log, r.eventRecorder, trustManager, desired, &corev1.Service{}, fieldOwner, serviceModified)
}

func serviceModified(desired, existing *corev1.Service) bool {
	if managedMetadataModified(desired, existing) {
		return true
	}
	if desired.Spec.Type != existing.Spec.Type ||
		!maps.Equal(desired.Spec.Selector, existing.Spec.Selector) ||
		!reflect.DeepEqual(desired.Spec.Ports, existing.Spec.Ports) {
		return true
	}
	return false
}

func getWebhookServiceObject(resourceLabels, resourceAnnotations map[string]string) *corev1.Service {
	service := common.DecodeObjBytes[*corev1.Service](codecs, corev1.SchemeGroupVersion, assets.MustAsset(serviceAssetName))
	common.UpdateName(service, trustManagerServiceName)
	common.UpdateNamespace(service, operandNamespace)
	common.UpdateResourceLabels(service, resourceLabels)
	updateResourceAnnotations(service, resourceAnnotations)
	return service
}

func getMetricsServiceObject(resourceLabels, resourceAnnotations map[string]string) *corev1.Service {
	service := common.DecodeObjBytes[*corev1.Service](codecs, corev1.SchemeGroupVersion, assets.MustAsset(metricsServiceAssetName))
	common.UpdateName(service, trustManagerMetricsServiceName)
	common.UpdateNamespace(service, operandNamespace)
	common.UpdateResourceLabels(service, resourceLabels)
	updateResourceAnnotations(service, resourceAnnotations)
	return service
}
