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
	certManagerCAInjectorStaticResourcesControllerName = operatorName + "-cainjector-static-resources-"
	certManagerCAInjectorDeploymentControllerName      = operatorName + "-cainjector-deployment-"
	certManagerCAInjectorDeploymentFile                = "cert-manager-deployment/cert-manager-cainjector/cert-manager-cainjector-deployment.yaml"
)

var (
	certManagerCAInjectorAssetFiles = []string{
		"cert-manager-deployment/cert-manager-cainjector/cert-manager-cainjector-cr.yaml",
		"cert-manager-deployment/cert-manager-cainjector/cert-manager-cainjector-crb.yaml",
		"cert-manager-deployment/cert-manager-cainjector/cert-manager-cainjector-leaderelection-rb.yaml",
		"cert-manager-deployment/cert-manager-cainjector/cert-manager-cainjector-leaderelection-role.yaml",
		"cert-manager-deployment/cert-manager-cainjector/cert-manager-cainjector-sa.yaml",
	}
)

func NewCertManagerCAInjectorStaticResourcesController(operatorClient v1helpers.OperatorClient,
	kubeClientContainer kubeclient.KubeClientContainer,
	kubeInformersForTargetNamespace v1helpers.KubeInformersForNamespaces,
	eventsRecorder events.Recorder) factory.Controller {
	return staticresourcecontroller.NewStaticResourceController(
		certManagerCAInjectorStaticResourcesControllerName,
		assets.Asset,
		certManagerCAInjectorAssetFiles,
		kubeClientContainer.ToKubeClientHolder(),
		operatorClient,
		eventsRecorder,
	).AddKubeInformers(kubeInformersForTargetNamespace)
}

func NewCertManagerCAInjectorDeploymentController(operatorClient v1helpers.OperatorClient,
	kubeClientContainer kubeclient.KubeClientContainer,
	kubeInformersForTargetNamespace informers.SharedInformerFactory,
	openshiftClusterConfigClient configv1.ClusterOperatorInterface,
	eventsRecorder events.Recorder, versionRecorder status.VersionGetter) factory.Controller {
	return newGenericDeploymentController(
		certManagerCAInjectorDeploymentControllerName,
		certManagerCAInjectorDeploymentFile,
		operatorClient,
		kubeClientContainer,
		kubeInformersForTargetNamespace,
		openshiftClusterConfigClient,
		eventsRecorder,
		versionRecorder)
}
