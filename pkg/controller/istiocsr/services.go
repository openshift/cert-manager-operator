package istiocsr

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

const (
	// grpcServicePortName is the name found for the GRPC service in the static manifest.
	grpcServicePortName = "web"
)

func (r *Reconciler) createOrApplyServices(istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string, istioCSRCreateRecon bool) error {
	service := r.getServiceObject(istiocsr, resourceLabels)
	if err := r.createOrApplyService(istiocsr, service, istioCSRCreateRecon); err != nil {
		return err
	}
	if err := r.updateGRPCEndpointInStatus(istiocsr, service); err != nil {
		return FromClientError(err, "failed to update %s/%s istiocsr status with %s service endpoint info", istiocsr.GetNamespace(), istiocsr.GetName(), service.GetName())
	}

	metricsService := r.getMetricsServiceObject(istiocsr, resourceLabels)
	if err := r.createOrApplyService(istiocsr, metricsService, istioCSRCreateRecon); err != nil {
		return err
	}
	return nil
}

func (r *Reconciler) createOrApplyService(istiocsr *v1alpha1.IstioCSR, svc *corev1.Service, istioCSRCreateRecon bool) error {
	serviceName := fmt.Sprintf("%s/%s", svc.GetNamespace(), svc.GetName())
	r.log.V(logVerbosityLevelDebug).Info("reconciling service resource", "name", serviceName)
	fetched := &corev1.Service{}
	exist, err := r.Exists(r.ctx, client.ObjectKeyFromObject(svc), fetched)
	if err != nil {
		return FromClientError(err, "failed to check %s service resource already exists", serviceName)
	}

	if exist && istioCSRCreateRecon {
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s service resource already exists, maybe from previous installation", serviceName)
	}
	if exist && hasObjectChanged(svc, fetched) {
		r.log.V(1).Info("service has been modified, updating to desired state", "name", serviceName)
		if err := r.UpdateWithRetry(r.ctx, svc); err != nil {
			return FromClientError(err, "failed to update %s service resource", serviceName)
		}
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeNormal, "Reconciled", "service resource %s reconciled back to desired state", serviceName)
	} else {
		r.log.V(logVerbosityLevelDebug).Info("service resource already exists and is in expected state", "name", serviceName)
	}
	if !exist {
		if err := r.Create(r.ctx, svc); err != nil {
			return FromClientError(err, "failed to create %s service resource", serviceName)
		}
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeNormal, "Reconciled", "service resource %s created", serviceName)
	}
	return nil
}

func (r *Reconciler) getServiceObject(istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string) *corev1.Service {
	service := decodeServiceObjBytes(assets.MustAsset(serviceAssetName))
	updateNamespace(service, istiocsr.GetNamespace())
	updateResourceLabels(service, resourceLabels)
	if istiocsr.Spec.IstioCSRConfig.Server != nil {
		updateServicePort(service, istiocsr.Spec.IstioCSRConfig.Server.Port)
	}
	return service
}

func (r *Reconciler) getMetricsServiceObject(istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string) *corev1.Service {
	service := decodeServiceObjBytes(assets.MustAsset(metricsServiceAssetName))
	updateNamespace(service, istiocsr.GetNamespace())
	updateResourceLabels(service, resourceLabels)
	return service
}

func updateServicePort(service *corev1.Service, port int32) {
	for i, servicePort := range service.Spec.Ports {
		if servicePort.Name == grpcServicePortName && port != 0 {
			service.Spec.Ports[i].Port = port
		}
	}
}

func (r *Reconciler) updateGRPCEndpointInStatus(istiocsr *v1alpha1.IstioCSR, service *corev1.Service) error {
	for _, servicePort := range service.Spec.Ports {
		if servicePort.Name == grpcServicePortName {
			endpoint := fmt.Sprintf(istiocsrGRPCEndpointFmt, service.Name, service.Namespace, servicePort.Port)
			if istiocsr.Status.IstioCSRGRPCEndpoint == endpoint {
				return nil
			}
			istiocsr.Status.IstioCSRGRPCEndpoint = endpoint
		}
	}
	return r.updateStatus(r.ctx, istiocsr)
}
