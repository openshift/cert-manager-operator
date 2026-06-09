package trustmanager

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func (r *Reconciler) createOrApplyServices(tm *v1alpha1.TrustManager, trustNamespace string, resourceLabels map[string]string, trustManagerCreateRecon bool) error {
	metricsService := r.getMetricsServiceObject(trustNamespace, resourceLabels)
	if err := r.createOrApplyService(tm, metricsService, trustManagerCreateRecon); err != nil {
		return err
	}

	// If defaultCAPackage is enabled, ensure the ConfigMap for CA bundle injection exists
	if tm.Spec.TrustManagerConfig.DefaultCAPackage.Policy == v1alpha1.DefaultCAPackagePolicyEnabled {
		if err := r.createOrApplyDefaultCAPackageConfigMap(tm, trustNamespace, resourceLabels); err != nil {
			r.log.Error(err, "failed to reconcile default CA package configmap")
			return err
		}
	}

	return nil
}

func (r *Reconciler) createOrApplyService(tm *v1alpha1.TrustManager, svc *corev1.Service, trustManagerCreateRecon bool) error {
	serviceName := fmt.Sprintf("%s/%s", svc.GetNamespace(), svc.GetName())
	r.log.V(4).Info("reconciling service resource", "name", serviceName)
	fetched := &corev1.Service{}
	exist, err := r.Exists(r.ctx, client.ObjectKeyFromObject(svc), fetched)
	if err != nil {
		return FromClientError(err, "failed to check %s service resource already exists", serviceName)
	}

	if exist && trustManagerCreateRecon {
		r.eventRecorder.Eventf(tm, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s service resource already exists, maybe from previous installation", serviceName)
	}
	if exist && hasObjectChanged(svc, fetched) {
		r.log.V(1).Info("service has been modified, updating to desired state", "name", serviceName)
		if err := r.UpdateWithRetry(r.ctx, svc); err != nil {
			return FromClientError(err, "failed to update %s service resource", serviceName)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "service resource %s reconciled back to desired state", serviceName)
	} else {
		r.log.V(4).Info("service resource already exists and is in expected state", "name", serviceName)
	}
	if !exist {
		if err := r.Create(r.ctx, svc); err != nil {
			return FromClientError(err, "failed to create %s service resource", serviceName)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "service resource %s created", serviceName)
	}
	return nil
}

func (r *Reconciler) getMetricsServiceObject(trustNamespace string, resourceLabels map[string]string) *corev1.Service {
	service := decodeServiceObjBytes(assets.MustAsset(metricsServiceAssetName))
	updateNamespace(service, trustNamespace)
	updateResourceLabels(service, resourceLabels)
	return service
}
