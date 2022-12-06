package deployment

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	v1 "github.com/openshift/api/operator/v1"
	configv1 "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/deploymentcontroller"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/status"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
	"github.com/openshift/cert-manager-operator/pkg/operator/operatorclient"
)

func newGenericDeploymentController(
	controllerName, targetVersion, deploymentFile string,
	operatorClient v1helpers.OperatorClientWithFinalizers,
	kubeClient kubernetes.Interface,
	kubeInformersForTargetNamespace informers.SharedInformerFactory,
	openshiftClusterConfigClient configv1.ClusterOperatorInterface,
	eventsRecorder events.Recorder,
	versionRecorder status.VersionGetter,
) factory.Controller {
	return deploymentcontroller.NewDeploymentController(
		controllerName,
		assets.MustAsset(deploymentFile),
		eventsRecorder,
		operatorClient,
		kubeClient,
		kubeInformersForTargetNamespace.Apps().V1().Deployments(),
		[]factory.Informer{},
		[]deploymentcontroller.ManifestHookFunc{},
		func(operatorSpec *v1.OperatorSpec, deployment *appsv1.Deployment) error {
			for index := range deployment.Spec.Template.Spec.Containers {
				deployment.Spec.Template.Spec.Containers[index].Image = certManagerImage(deployment.Spec.Template.Spec.Containers[index].Image)
			}

			unsupportedExtensions, err := operatorclient.GetUnsupportedConfigOverrides(operatorSpec)
			if err != nil {
				return fmt.Errorf("failed to get unsupportedConfigOverrides due to: %w", err)
			}
			deployment = UnsupportedConfigOverrides(deployment, unsupportedExtensions)
			return nil
		},
	)
}
