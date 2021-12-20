package deployment

import (
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	configv1 "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"

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

func NewCertManagerCAInjectorStaticResourcesController(operatorClient v1helpers.OperatorClient, kubeClientContainer *resourceapply.ClientHolder, kubeInformersForTargetNamespace v1helpers.KubeInformersForNamespaces, eventsRecorder events.Recorder, deploymentChecker *deploymentChecker) factory.Controller {
	return staticresourcecontroller.NewStaticResourceController(
		certManagerCAInjectorStaticResourcesControllerName,
		assets.Asset,
		certManagerCAInjectorAssetFiles,
		kubeClientContainer,
		operatorClient,
		eventsRecorder,
	).WithPrecondition(deploymentChecker.shouldSync).AddKubeInformers(kubeInformersForTargetNamespace)
}

func NewCertManagerCAInjectorDeploymentController(operatorClient v1helpers.OperatorClient, kubeClient kubernetes.Interface, kubeInformersForTargetNamespace informers.SharedInformerFactory, openshiftClusterConfigClient configv1.ClusterOperatorInterface, eventsRecorder events.Recorder, versionRecorder status.VersionGetter, deploymentChecker *deploymentChecker) factory.Controller {
	return newGenericDeploymentController(
		certManagerCAInjectorDeploymentControllerName,
		certManagerCAInjectorDeploymentFile,
		operatorClient,
		kubeClient,
		kubeInformersForTargetNamespace,
		openshiftClusterConfigClient,
		eventsRecorder,
		versionRecorder,
		deploymentChecker)
}
