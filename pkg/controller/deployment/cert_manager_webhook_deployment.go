package deployment

import (
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/staticresourcecontroller"
	"github.com/openshift/library-go/pkg/operator/status"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

const (
	certManagerWebhookStaticResourcesControllerName = operatorName + "-webhook-static-resources-"
	certManagerWebhookDeploymentControllerName      = operatorName + "-webhook-deployment"
	certManagerWebhookDeploymentFile                = "cert-manager-deployment/webhook/cert-manager-webhook-deployment.yaml"
)

var (
	certManagerWebhookAssetFiles = []string{
		"cert-manager-deployment/webhook/cert-manager-webhook-mutatingwebhookconfiguration.yaml",
		"cert-manager-deployment/webhook/cert-manager-webhook-validatingwebhookconfiguration.yaml",
		"cert-manager-deployment/webhook/cert-manager-webhook-dynamic-serving-rb.yaml",
		"cert-manager-deployment/webhook/cert-manager-webhook-dynamic-serving-role.yaml",
		"cert-manager-deployment/webhook/cert-manager-webhook-sa.yaml",
		"cert-manager-deployment/webhook/cert-manager-webhook-subjectaccessreviews-cr.yaml",
		"cert-manager-deployment/webhook/cert-manager-webhook-subjectaccessreviews-crb.yaml",
		"cert-manager-deployment/webhook/cert-manager-webhook-svc.yaml",
		"cert-manager-deployment/webhook/cert-manager-webhook-configmap.yaml",
	}
)

func NewCertManagerWebhookStaticResourcesController(operatorClient v1helpers.OperatorClient,
	kubeClientContainer *resourceapply.ClientHolder,
	kubeInformersForTargetNamespace v1helpers.KubeInformersForNamespaces,
	eventsRecorder events.Recorder) factory.Controller {
	return staticresourcecontroller.NewStaticResourceController(
		certManagerWebhookStaticResourcesControllerName,
		assets.Asset,
		certManagerWebhookAssetFiles,
		kubeClientContainer,
		operatorClient,
		eventsRecorder,
	).AddKubeInformers(kubeInformersForTargetNamespace)
}

func NewCertManagerWebhookDeploymentController(operatorClient v1helpers.OperatorClientWithFinalizers,
	kubeclient kubernetes.Interface,
	kubeInformersForTargetNamespace informers.SharedInformerFactory,
	eventsRecorder events.Recorder, targetVersion string, versionRecorder status.VersionGetter) factory.Controller {
	return newGenericDeploymentController(
		certManagerWebhookDeploymentControllerName,
		targetVersion,
		certManagerWebhookDeploymentFile,
		operatorClient,
		kubeclient,
		kubeInformersForTargetNamespace,
		eventsRecorder,
		versionRecorder)
}
