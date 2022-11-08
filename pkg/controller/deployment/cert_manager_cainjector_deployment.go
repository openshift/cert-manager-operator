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
	certManagerCAInjectorStaticResourcesControllerName = operatorName + "-cainjector-static-resources-"
	certManagerCAInjectorDeploymentControllerName      = operatorName + "-cainjector-deployment-"
	certManagerCAInjectorDeploymentFile                = "cert-manager-deployment/cainjector/cert-manager-cainjector-deployment.yaml"
)

var (
	certManagerCAInjectorAssetFiles = []string{
		"cert-manager-deployment/cainjector/cert-manager-cainjector-cr.yaml",
		"cert-manager-deployment/cainjector/cert-manager-cainjector-crb.yaml",
		"cert-manager-deployment/cainjector/cert-manager-cainjector-leaderelection-rb.yaml",
		"cert-manager-deployment/cainjector/cert-manager-cainjector-leaderelection-role.yaml",
		"cert-manager-deployment/cainjector/cert-manager-cainjector-sa.yaml",
	}
)

func NewCertManagerCAInjectorStaticResourcesController(operatorClient v1helpers.OperatorClient,
	kubeClientContainer *resourceapply.ClientHolder,
	kubeInformersForTargetNamespace v1helpers.KubeInformersForNamespaces,
	eventsRecorder events.Recorder,
) factory.Controller {
	return staticresourcecontroller.NewStaticResourceController(
		certManagerCAInjectorStaticResourcesControllerName,
		assets.Asset,
		certManagerCAInjectorAssetFiles,
		kubeClientContainer,
		operatorClient,
		eventsRecorder,
	).AddKubeInformers(kubeInformersForTargetNamespace)
}

func NewCertManagerCAInjectorDeploymentController(operatorClient v1helpers.OperatorClient,
	kubeClient kubernetes.Interface,
	kubeInformersForTargetNamespace informers.SharedInformerFactory,
	eventsRecorder events.Recorder, targetVersion string, versionRecorder status.VersionGetter,
) factory.Controller {
	return newGenericDeploymentController(
		certManagerCAInjectorDeploymentControllerName,
		targetVersion,
		certManagerCAInjectorDeploymentFile,
		operatorClient,
		kubeClient,
		kubeInformersForTargetNamespace,
		eventsRecorder,
		versionRecorder)
}
