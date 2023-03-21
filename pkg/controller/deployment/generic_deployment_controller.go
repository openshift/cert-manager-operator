package deployment

import (
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/deploymentcontroller"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	"github.com/openshift/library-go/pkg/operator/status"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
	certmanoperatorinformers "github.com/openshift/cert-manager-operator/pkg/operator/informers/externalversions"
)

func newGenericDeploymentController(
	controllerName, targetVersion, deploymentFile string,
	operatorClient v1helpers.OperatorClientWithFinalizers,
	certManagerOperatorInformers certmanoperatorinformers.SharedInformerFactory,
	kubeClient kubernetes.Interface,
	kubeInformersForTargetNamespace informers.SharedInformerFactory,
	eventsRecorder events.Recorder,
	versionRecorder status.VersionGetter,
	trustedCAConfigmapName string,
) factory.Controller {
	deployment := resourceread.ReadDeploymentV1OrDie(assets.MustAsset(deploymentFile))
	return deploymentcontroller.NewDeploymentController(
		controllerName,
		assets.MustAsset(deploymentFile),
		eventsRecorder,
		operatorClient,
		kubeClient,
		kubeInformersForTargetNamespace.Apps().V1().Deployments(),
		[]factory.Informer{
			kubeInformersForTargetNamespace.Core().V1().ConfigMaps().Informer(),
		},
		[]deploymentcontroller.ManifestHookFunc{},
		withOperandImageOverrideHook,
		withContainerArgsOverrideHook(certManagerOperatorInformers.Operator().V1alpha1().CertManagers(), deployment.Name, getOverrideArgsFor),
		withContainerArgsValidateHook(certManagerOperatorInformers.Operator().V1alpha1().CertManagers(), deployment.Name),
		withContainerEnvOverrideHook(certManagerOperatorInformers.Operator().V1alpha1().CertManagers(), deployment.Name, getOverrideEnvFor),
		withContainerEnvValidateHook(certManagerOperatorInformers.Operator().V1alpha1().CertManagers(), deployment.Name),
		withLogLevel,
		withUnsupportedArgsOverrideHook,
		withProxyEnv,
		withCAConfigMap(kubeInformersForTargetNamespace.Core().V1().ConfigMaps(), deployment, trustedCAConfigmapName),
	)
}
