package deployment

import (
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	configv1client "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"

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
	configClient configv1client.ConfigV1Interface,
	informersFactory informers.SharedInformerFactory,
	operatorClient v1helpers.OperatorClient,
	kubeClientContainer *resourceapply.ClientHolder,
	eventRecorder events.Recorder,
	versionRecorder status.VersionGetter,
) *CertManagerControllerSet {
	deploymentChecker := newDeploymentChecker(kubeClient.AppsV1())
	return &CertManagerControllerSet{
		certManagerControllerStaticResourcesController: NewCertManagerControllerStaticResourcesController(operatorClient, kubeClientContainer, kubeInformersForTargetNamespace, eventRecorder, deploymentChecker),
		certManagerControllerDeploymentController:      NewCertManagerControllerDeploymentController(operatorClient, kubeClient, informersFactory, configClient.ClusterOperators(), eventRecorder, versionRecorder, deploymentChecker),
		certManagerWebhookStaticResourcesController:    NewCertManagerWebhookStaticResourcesController(operatorClient, kubeClientContainer, kubeInformersForTargetNamespace, eventRecorder, deploymentChecker),
		certManagerWebhookDeploymentController:         NewCertManagerWebhookDeploymentController(operatorClient, kubeClient, informersFactory, configClient.ClusterOperators(), eventRecorder, versionRecorder, deploymentChecker),
		certManagerCAInjectorStaticResourcesController: NewCertManagerCAInjectorStaticResourcesController(operatorClient, kubeClientContainer, kubeInformersForTargetNamespace, eventRecorder, deploymentChecker),
		certManagerCAInjectorDeploymentController:      NewCertManagerCAInjectorDeploymentController(operatorClient, kubeClient, informersFactory, configClient.ClusterOperators(), eventRecorder, versionRecorder, deploymentChecker),
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
