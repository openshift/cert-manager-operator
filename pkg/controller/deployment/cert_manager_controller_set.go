package deployment

import (
	"github.com/openshift/cert-manager-operator/pkg/operator/kubeclient"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/status"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
	"k8s.io/client-go/informers"
)

type CertManagerControllerSet struct {
	certManagerControllerStaticResourcesController factory.Controller
	certManagerControllerDeploymentController      factory.Controller
	certManagerWebhookStaticResourcesController    factory.Controller
	certManagerWebhookDeploymentController         factory.Controller
	certManagerCAInjectorStaticResourcesController factory.Controller
	certManagerCAInjectorDeploymentController      factory.Controller
}

func NewCertManagerControllerSet(kubeInformersForTargetNamespace v1helpers.KubeInformersForNamespaces, informersFactory informers.SharedInformerFactory, operatorClient v1helpers.OperatorClient, kubeClientContainer kubeclient.KubeClientContainer, eventRecorder events.Recorder, versionRecorder status.VersionGetter) *CertManagerControllerSet {
	return &CertManagerControllerSet{
		certManagerControllerStaticResourcesController: NewCertManagerControllerStaticResourcesController(operatorClient, kubeClientContainer, kubeInformersForTargetNamespace, eventRecorder),
		certManagerControllerDeploymentController:      NewCertManagerControllerDeploymentController(operatorClient, kubeClientContainer, informersFactory, kubeClientContainer.ConfigClient.ConfigV1().ClusterOperators(), eventRecorder, versionRecorder),
		certManagerWebhookStaticResourcesController:    NewCertManagerWebhookStaticResourcesController(operatorClient, kubeClientContainer, kubeInformersForTargetNamespace, eventRecorder),
		certManagerWebhookDeploymentController:         NewCertManagerWebhookDeploymentController(operatorClient, kubeClientContainer, informersFactory, kubeClientContainer.ConfigClient.ConfigV1().ClusterOperators(), eventRecorder, versionRecorder),
		certManagerCAInjectorStaticResourcesController: NewCertManagerCAInjectorStaticResourcesController(operatorClient, kubeClientContainer, kubeInformersForTargetNamespace, eventRecorder),
		certManagerCAInjectorDeploymentController:      NewCertManagerCAInjectorDeploymentController(operatorClient, kubeClientContainer, informersFactory, kubeClientContainer.ConfigClient.ConfigV1().ClusterOperators(), eventRecorder, versionRecorder),
	}
}

func (c *CertManagerControllerSet) ToArray() []factory.Controller {
	return []factory.Controller{
		c.certManagerControllerStaticResourcesController,
		c.certManagerControllerDeploymentController,
		c.certManagerWebhookStaticResourcesController,
		c.certManagerWebhookDeploymentController,
		c.certManagerCAInjectorStaticResourcesController,
		c.certManagerCAInjectorDeploymentController,
	}
}
