package deployment

import (
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	configinformers "github.com/openshift/client-go/config/informers/externalversions"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/staticresourcecontroller"
	"github.com/openshift/library-go/pkg/operator/status"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
	certmanoperatorinformers "github.com/openshift/cert-manager-operator/pkg/operator/informers/externalversions"
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
	kubeInformersForNamespaces v1helpers.KubeInformersForNamespaces,
	eventsRecorder events.Recorder) factory.Controller {
	return staticresourcecontroller.NewStaticResourceController(
		certManagerWebhookStaticResourcesControllerName,
		assets.Asset,
		certManagerWebhookAssetFiles,
		kubeClientContainer,
		operatorClient,
		eventsRecorder,
	).AddKubeInformers(kubeInformersForNamespaces)
}

func NewCertManagerWebhookDeploymentController(operatorClient v1helpers.OperatorClientWithFinalizers,
	certManagerOperatorInformers certmanoperatorinformers.SharedInformerFactory,
	infraInformers configinformers.SharedInformerFactory,
	kubeclient kubernetes.Interface,
	kubeInformersForTargetNamespace informers.SharedInformerFactory,
	eventsRecorder events.Recorder, targetVersion string, versionRecorder status.VersionGetter, trustedCAConfigmapName, cloudCredentialsSecretName string) factory.Controller {
	return newGenericDeploymentController(
		certManagerWebhookDeploymentControllerName,
		targetVersion,
		certManagerWebhookDeploymentFile,
		operatorClient,
		certManagerOperatorInformers,
		infraInformers,
		kubeclient,
		kubeInformersForTargetNamespace,
		eventsRecorder,
		versionRecorder,
		trustedCAConfigmapName,
		cloudCredentialsSecretName,
	)
}
