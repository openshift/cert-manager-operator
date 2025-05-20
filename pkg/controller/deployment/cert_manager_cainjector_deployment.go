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
	certManagerCAInjectorStaticResourcesControllerName = operatorName + "-cainjector-static-resources-"
	certManagerCAInjectorDeploymentControllerName      = operatorName + "-cainjector-deployment"
	certManagerCAInjectorDeploymentFile                = "cert-manager-deployment/cainjector/cert-manager-cainjector-deployment.yaml"
)

var (
	certManagerCAInjectorAssetFiles = []string{
		"cert-manager-deployment/cainjector/cert-manager-cainjector-cr.yaml",
		"cert-manager-deployment/cainjector/cert-manager-cainjector-crb.yaml",
		"cert-manager-deployment/cainjector/cert-manager-cainjector-leaderelection-rb.yaml",
		"cert-manager-deployment/cainjector/cert-manager-cainjector-leaderelection-role.yaml",
		"cert-manager-deployment/cainjector/cert-manager-cainjector-sa.yaml",
		"cert-manager-deployment/cainjector/cert-manager-cainjector-svc.yaml",
	}
)

func NewCertManagerCAInjectorStaticResourcesController(operatorClient v1helpers.OperatorClient,
	kubeClientContainer *resourceapply.ClientHolder,
	kubeInformersForNamespaces v1helpers.KubeInformersForNamespaces,
	eventsRecorder events.Recorder,
) factory.Controller {
	return staticresourcecontroller.NewStaticResourceController(
		certManagerCAInjectorStaticResourcesControllerName,
		assets.Asset,
		certManagerCAInjectorAssetFiles,
		kubeClientContainer,
		operatorClient,
		eventsRecorder,
	).AddKubeInformers(kubeInformersForNamespaces)
}

func NewCertManagerCAInjectorDeploymentController(operatorClient v1helpers.OperatorClientWithFinalizers,
	certManagerOperatorInformers certmanoperatorinformers.SharedInformerFactory,
	infraInformers configinformers.SharedInformerFactory,
	kubeClient kubernetes.Interface,
	kubeInformersForTargetNamespace informers.SharedInformerFactory,
	eventsRecorder events.Recorder, targetVersion string, versionRecorder status.VersionGetter,
	trustedCAConfigmapName, cloudCredentialsSecretName string,
) factory.Controller {
	return newGenericDeploymentController(
		certManagerCAInjectorDeploymentControllerName,
		targetVersion,
		certManagerCAInjectorDeploymentFile,
		operatorClient,
		certManagerOperatorInformers,
		infraInformers,
		kubeClient,
		kubeInformersForTargetNamespace,
		eventsRecorder,
		versionRecorder,
		trustedCAConfigmapName,
		cloudCredentialsSecretName,
	)
}
