package deployment

import (
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	configinformers "github.com/openshift/client-go/config/informers/externalversions"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/deploymentcontroller"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	"github.com/openshift/library-go/pkg/operator/status"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
	certmanoperatorinformers "github.com/openshift/cert-manager-operator/pkg/operator/informers/externalversions"
	"github.com/openshift/cert-manager-operator/pkg/operator/optionalinformer"
)

func newGenericDeploymentController(
	controllerName, targetVersion, deploymentFile string,
	operatorClient v1helpers.OperatorClientWithFinalizers,
	certManagerOperatorInformers certmanoperatorinformers.SharedInformerFactory,
	infraInformers optionalinformer.OptionalInformer[configinformers.SharedInformerFactory],
	kubeClient kubernetes.Interface,
	kubeInformersForTargetNamespace informers.SharedInformerFactory,
	eventsRecorder events.Recorder,
	versionRecorder status.VersionGetter,
	trustedCAConfigmapName string,
	cloudCredentialsSecretName string,
) factory.Controller {
	deployment := resourceread.ReadDeploymentV1OrDie(assets.MustAsset(deploymentFile))

	hooks := []deploymentcontroller.DeploymentHookFunc{
		withOperandImageOverrideHook,
		withLogLevel,
		withPodLabelsOverrideHook(certManagerOperatorInformers.Operator().V1alpha1().CertManagers(), deployment.Name, getOverridePodLabelsFor),
		withPodLabelsValidateHook(certManagerOperatorInformers.Operator().V1alpha1().CertManagers(), deployment.Name),
		withContainerArgsOverrideHook(certManagerOperatorInformers.Operator().V1alpha1().CertManagers(), deployment.Name, getOverrideArgsFor),
		withContainerArgsValidateHook(certManagerOperatorInformers.Operator().V1alpha1().CertManagers(), deployment.Name),
		withProxyEnv,
		withContainerEnvOverrideHook(certManagerOperatorInformers.Operator().V1alpha1().CertManagers(), deployment.Name, getOverrideEnvFor),
		withContainerEnvValidateHook(certManagerOperatorInformers.Operator().V1alpha1().CertManagers(), deployment.Name),
		withDeploymentReplicasOverrideHook(certManagerOperatorInformers.Operator().V1alpha1().CertManagers(), deployment.Name, getOverrideReplicasFor),
		withContainerResourcesOverrideHook(certManagerOperatorInformers.Operator().V1alpha1().CertManagers(), deployment.Name, getOverrideResourcesFor),
		withContainerResourcesValidateHook(certManagerOperatorInformers.Operator().V1alpha1().CertManagers(), deployment.Name),
		withPodSchedulingOverrideHook(certManagerOperatorInformers.Operator().V1alpha1().CertManagers(), deployment.Name, getOverrideSchedulingFor),
		withPodSchedulingValidateHook(certManagerOperatorInformers.Operator().V1alpha1().CertManagers(), deployment.Name),
		withUnsupportedArgsOverrideHook,
		withCAConfigMap(kubeInformersForTargetNamespace.Core().V1().ConfigMaps(), deployment, trustedCAConfigmapName),
		withSABoundToken,
	}

	if infraInformers.Applicable() {
		infraInformerFactory := (*infraInformers.InformerFactory)

		hooks = append(hooks, withCloudCredentials(
			kubeInformersForTargetNamespace.Core().V1().Secrets(),
			infraInformerFactory.Config().V1().Infrastructures(),
			deployment.Name,
			cloudCredentialsSecretName,
		))
	}

	return deploymentcontroller.NewDeploymentController(
		controllerName,
		assets.MustAsset(deploymentFile),
		eventsRecorder,
		operatorClient,
		kubeClient,
		kubeInformersForTargetNamespace.Apps().V1().Deployments(),
		nil,
		[]deploymentcontroller.ManifestHookFunc{},
		hooks...,
	)
}
