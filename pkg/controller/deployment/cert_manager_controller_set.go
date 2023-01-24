package deployment

import (
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	alpha1 "github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned/typed/operator/v1alpha1"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/status"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
)

type CertManagerControllerSet struct {
	certManagerControllerStaticResourcesController factory.Controller
	certManagerControllerDeploymentController      factory.Controller
	certManagerWebhookStaticResourcesController    factory.Controller
	certManagerWebhookDeploymentController         factory.Controller
	certManagerCAInjectorStaticResourcesController factory.Controller
	certManagerCAInjectorDeploymentController      factory.Controller
}

func NewCertManagerControllerSet(
	kubeClient kubernetes.Interface,
	kubeInformersForTargetNamespace v1helpers.KubeInformersForNamespaces,
	informersFactory informers.SharedInformerFactory,
	operatorClient v1helpers.OperatorClientWithFinalizers,
	certManagerClient alpha1.OperatorV1alpha1Interface,
	kubeClientContainer *resourceapply.ClientHolder,
	eventRecorder events.Recorder,
	targetVersion string,
	versionRecorder status.VersionGetter,
) *CertManagerControllerSet {
	return &CertManagerControllerSet{
		certManagerControllerStaticResourcesController: NewCertManagerControllerStaticResourcesController(operatorClient, kubeClientContainer, kubeInformersForTargetNamespace, eventRecorder),
		certManagerControllerDeploymentController:      NewCertManagerControllerDeploymentController(operatorClient, certManagerClient, kubeClient, informersFactory, eventRecorder, targetVersion, versionRecorder),
		certManagerWebhookStaticResourcesController:    NewCertManagerWebhookStaticResourcesController(operatorClient, kubeClientContainer, kubeInformersForTargetNamespace, eventRecorder),
		certManagerWebhookDeploymentController:         NewCertManagerWebhookDeploymentController(operatorClient, certManagerClient, kubeClient, informersFactory, eventRecorder, targetVersion, versionRecorder),
		certManagerCAInjectorStaticResourcesController: NewCertManagerCAInjectorStaticResourcesController(operatorClient, kubeClientContainer, kubeInformersForTargetNamespace, eventRecorder),
		certManagerCAInjectorDeploymentController:      NewCertManagerCAInjectorDeploymentController(operatorClient, certManagerClient, kubeClient, informersFactory, eventRecorder, targetVersion, versionRecorder),
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
