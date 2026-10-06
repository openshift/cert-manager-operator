package trustmanager

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

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
	serviceName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(4).Info("reconciling service resource", "name", serviceName)

	existing := &corev1.Service{}
	exists, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), existing)
	if err != nil {
		return common.FromClientError(err, "failed to check if service %q exists", serviceName)
	}
	if exists && !common.ManagedMetadataModified(desired, existing) && !common.ServiceSpecModified(desired, existing) {
		r.log.V(4).Info("service resource exists and is in desired state", "name", serviceName)
		return nil
	}

	r.log.V(2).Info("service resource has been modified, updating to desired state", "name", serviceName)
	if err := r.Patch(r.ctx, desired, client.Apply, client.FieldOwner(fieldOwner), client.ForceOwnership); err != nil {
		return common.FromClientError(err, "failed to apply service %q", serviceName)
	}

	r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "service resource %s applied", serviceName)
	return nil
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
