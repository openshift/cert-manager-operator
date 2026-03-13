package trustmanager

import (
	"fmt"
	"maps"
	"reflect"

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
	if exists && !serviceModified(desired, existing) {
		r.log.V(4).Info("service already matches desired state, skipping apply", "name", serviceName)
		return nil
	}

	if err := r.Patch(r.ctx, desired, client.Apply, client.FieldOwner(fieldOwner), client.ForceOwnership); err != nil {
		return common.FromClientError(err, "failed to apply service %q", serviceName)
	}

	r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "service resource %s applied", serviceName)
	r.log.V(2).Info("applied service", "name", serviceName)
	return nil
}

// serviceModified compares only the fields we manage via SSA.
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
