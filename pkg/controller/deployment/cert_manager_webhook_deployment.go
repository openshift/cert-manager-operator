package deployment

import (
	configv1 "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/staticresourcecontroller"
	"github.com/openshift/library-go/pkg/operator/status"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
	"k8s.io/client-go/informers"

	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
	"github.com/openshift/cert-manager-operator/pkg/operator/kubeclient"
)

const (
	certManagerWebhookStaticResourcesControllerName = operatorName + "-webhook-static-resources-"
	certManagerWebhookDeploymentControllerName      = operatorName + "-webhook-deployment-"
	certManagerWebhookDeploymentFile                = "cert-manager-deployment/cert-manager-webhook/cert-manager-webhook-deployment.yaml"
)

var (
	certManagerWebhookAssetFiles = []string{
		// FIXME: Add Mutating Webhook Configuration into github.com/openshift/library-go/pkg/operator/resource/resourceapply/unstructured.go
		// Then, this these guys back on.
		//"cert-manager-deployment/cert-manager-webhook/cert-manager-webhook-mutatingwebhookconfiguration.yaml",
		//"cert-manager-deployment/cert-manager-webhook/cert-manager-webhook-validatingwebhookconfiguration.yaml",
		"cert-manager-deployment/cert-manager-webhook/cert-manager-webhook-dynamic-serving-rb.yaml",
		"cert-manager-deployment/cert-manager-webhook/cert-manager-webhook-dynamic-serving-role.yaml",
		"cert-manager-deployment/cert-manager-webhook/cert-manager-webhook-sa.yaml",
		"cert-manager-deployment/cert-manager-webhook/cert-manager-webhook-subjectaccessreviews-cr.yaml",
		"cert-manager-deployment/cert-manager-webhook/cert-manager-webhook-subjectaccessreviews-crb.yaml",
		"cert-manager-deployment/cert-manager-webhook/cert-manager-webhook-svc.yaml",
	}
)

func NewCertManagerWebhookStaticResourcesController(operatorClient v1helpers.OperatorClient,
	kubeClientContainer kubeclient.KubeClientContainer,
	kubeInformersForTargetNamespace v1helpers.KubeInformersForNamespaces,
	eventsRecorder events.Recorder) factory.Controller {
	return staticresourcecontroller.NewStaticResourceController(
		certManagerWebhookStaticResourcesControllerName,
		assets.Asset,
		certManagerWebhookAssetFiles,
		kubeClientContainer.ToKubeClientHolder(),
		operatorClient,
		eventsRecorder,
	).AddKubeInformers(kubeInformersForTargetNamespace)
}

func NewCertManagerWebhookDeploymentController(operatorClient v1helpers.OperatorClient,
	kubeClientContainer kubeclient.KubeClientContainer,
	kubeInformersForTargetNamespace informers.SharedInformerFactory,
	openshiftClusterConfigClient configv1.ClusterOperatorInterface,
	eventsRecorder events.Recorder, versionRecorder status.VersionGetter) factory.Controller {
	return newGenericDeploymentController(
		certManagerWebhookDeploymentControllerName,
		certManagerWebhookDeploymentFile,
		operatorClient,
		kubeClientContainer,
		kubeInformersForTargetNamespace,
		openshiftClusterConfigClient,
		eventsRecorder,
		versionRecorder)
}
