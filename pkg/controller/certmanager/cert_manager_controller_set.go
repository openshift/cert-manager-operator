package certmanager

import (
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	configinformers "github.com/openshift/client-go/config/informers/externalversions"
	consoleclient "github.com/openshift/client-go/console/clientset/versioned"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/status"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	operatorconfigclient "github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned/typed/operator/v1alpha1"
	certmanoperatorinformers "github.com/openshift/cert-manager-operator/pkg/operator/informers/externalversions"
	"github.com/openshift/cert-manager-operator/pkg/operator/utils"
)

type CertManagerControllerSet struct {
	certManagerControllerStaticResourcesController    factory.Controller
	certManagerControllerDeploymentController         factory.Controller
	certManagerWebhookStaticResourcesController       factory.Controller
	certManagerWebhookDeploymentController            factory.Controller
	certManagerCAInjectorStaticResourcesController    factory.Controller
	certManagerCAInjectorDeploymentController         factory.Controller
	certManagerNetworkPolicyStaticResourcesController factory.Controller
	certManagerNetworkPolicyUserDefinedController     factory.Controller
	consoleResourcesController                        factory.Controller
	certManagerAutoApproverController                 factory.Controller
}

func NewCertManagerControllerSet(
	kubeClient kubernetes.Interface,
	kubeInformersForNamespaces v1helpers.KubeInformersForNamespaces,
	kubeInformersForTargetNamespace informers.SharedInformerFactory,
	infraInformers utils.OptionalInformer[configinformers.SharedInformerFactory],
	operatorClient v1helpers.OperatorClientWithFinalizers,
	certManagerOperatorInformers certmanoperatorinformers.SharedInformerFactory,
	operatorConfigClient operatorconfigclient.OperatorV1alpha1Interface,
	kubeClientContainer *resourceapply.ClientHolder,
	consoleClient consoleclient.Interface,
	eventRecorder events.Recorder,
	targetVersion string,
	versionRecorder status.VersionGetter,
	trustedCAConfigmapName,
	cloudCredentialsSecretName string,
) *CertManagerControllerSet {
	set := &CertManagerControllerSet{
		certManagerAutoApproverController:                 NewCertManagerAutoApproverController(operatorClient, operatorConfigClient, certManagerOperatorInformers, kubeClient, kubeInformersForNamespaces, eventRecorder),
		certManagerControllerStaticResourcesController:    NewCertManagerControllerStaticResourcesController(operatorClient, kubeClientContainer, kubeInformersForNamespaces, eventRecorder),
		certManagerControllerDeploymentController:         NewCertManagerControllerDeploymentController(operatorClient, certManagerOperatorInformers, infraInformers, kubeClient, kubeInformersForTargetNamespace, eventRecorder, targetVersion, versionRecorder, trustedCAConfigmapName, cloudCredentialsSecretName),
		certManagerWebhookStaticResourcesController:       NewCertManagerWebhookStaticResourcesController(operatorClient, kubeClientContainer, kubeInformersForNamespaces, eventRecorder),
		certManagerWebhookDeploymentController:            NewCertManagerWebhookDeploymentController(operatorClient, certManagerOperatorInformers, infraInformers, kubeClient, kubeInformersForTargetNamespace, eventRecorder, targetVersion, versionRecorder, trustedCAConfigmapName, cloudCredentialsSecretName),
		certManagerCAInjectorStaticResourcesController:    NewCertManagerCAInjectorStaticResourcesController(operatorClient, kubeClientContainer, kubeInformersForNamespaces, eventRecorder),
		certManagerCAInjectorDeploymentController:         NewCertManagerCAInjectorDeploymentController(operatorClient, certManagerOperatorInformers, infraInformers, kubeClient, kubeInformersForTargetNamespace, eventRecorder, targetVersion, versionRecorder, trustedCAConfigmapName, cloudCredentialsSecretName),
		certManagerNetworkPolicyStaticResourcesController: NewCertManagerNetworkPolicyStaticResourcesController(operatorClient, kubeClientContainer, kubeInformersForNamespaces, certManagerOperatorInformers, eventRecorder),
		certManagerNetworkPolicyUserDefinedController:     NewCertManagerNetworkPolicyUserDefinedController(operatorClient, certManagerOperatorInformers, kubeClient, kubeInformersForNamespaces, eventRecorder),
	}
	if consoleClient != nil {
		set.consoleResourcesController = NewConsoleResourcesController(operatorClient, consoleClient, eventRecorder)
	}
	return set
}

func (c *CertManagerControllerSet) ToArray() []factory.Controller {
	controllers := []factory.Controller{
		c.certManagerControllerStaticResourcesController,
		c.certManagerControllerDeploymentController,
		c.certManagerWebhookStaticResourcesController,
		c.certManagerWebhookDeploymentController,
		c.certManagerCAInjectorStaticResourcesController,
		c.certManagerCAInjectorDeploymentController,
		c.certManagerNetworkPolicyStaticResourcesController,
		c.certManagerNetworkPolicyUserDefinedController,
		c.certManagerAutoApproverController,
	}
	if c.consoleResourcesController != nil {
		controllers = append(controllers, c.consoleResourcesController)
	}
	return controllers
}
