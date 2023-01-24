package deployment

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	v1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/deploymentcontroller"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/status"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
	alpha1 "github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned/typed/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/operatorclient"
)

func newGenericDeploymentController(
	controllerName, targetVersion, deploymentFile string,
	operatorClient v1helpers.OperatorClientWithFinalizers,
	certManagerClient alpha1.OperatorV1alpha1Interface,
	kubeClient kubernetes.Interface,
	kubeInformersForTargetNamespace informers.SharedInformerFactory,
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

			// replace images when envCertManagerControllerRelatedImage environment variable is specified.
			for index := range deployment.Spec.Template.Spec.Containers {
				deployment.Spec.Template.Spec.Containers[index].Image = certManagerImage(deployment.Spec.Template.Spec.Containers[index].Image)
			}

			certmanager, err := certManagerClient.CertManagers().Get(context.Background(), "cluster", metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("failed to get certmanager %q due to %v", "cluster", err)

			}
			deploymentTemplate := deployment.Spec.Template

			// override container args and env variables if specified by the user.
			if certmanager.Spec.ControllerConfig != nil && len(deploymentTemplate.Spec.Containers) == 1 && deployment.Name == "cert-manager" {

				if len(certmanager.Spec.ControllerConfig.OverrideArgs) > 0 {
					deployment.Spec.Template.Spec.Containers[0].Args = mergeContainerArgs(
						deploymentTemplate.Spec.Containers[0].Args, certmanager.Spec.ControllerConfig.OverrideArgs)
				}

				if len(certmanager.Spec.ControllerConfig.OverrideEnv) > 0 {
					deployment.Spec.Template.Spec.Containers[0].Env = mergeContainerEnvs(
						deploymentTemplate.Spec.Containers[0].Env, certmanager.Spec.ControllerConfig.OverrideEnv)
				}
			}
			if certmanager.Spec.WebhookConfig != nil && len(deploymentTemplate.Spec.Containers) == 1 && deployment.Name == "cert-manager-webhook" {

				if len(certmanager.Spec.WebhookConfig.OverrideArgs) > 0 {
					deployment.Spec.Template.Spec.Containers[0].Args = mergeContainerArgs(
						deploymentTemplate.Spec.Containers[0].Args, certmanager.Spec.WebhookConfig.OverrideArgs)
				}

				if len(certmanager.Spec.WebhookConfig.OverrideEnv) > 0 {
					deployment.Spec.Template.Spec.Containers[0].Env = mergeContainerEnvs(
						deploymentTemplate.Spec.Containers[0].Env, certmanager.Spec.WebhookConfig.OverrideEnv)
				}
			}
			if certmanager.Spec.CAInjectorConfig != nil && len(deploymentTemplate.Spec.Containers) == 1 && deployment.Name == "cert-manager-cainjector" {

				if len(certmanager.Spec.CAInjectorConfig.OverrideArgs) > 0 {
					deployment.Spec.Template.Spec.Containers[0].Args = mergeContainerArgs(
						deploymentTemplate.Spec.Containers[0].Args, certmanager.Spec.CAInjectorConfig.OverrideArgs)
				}

				if len(certmanager.Spec.CAInjectorConfig.OverrideEnv) > 0 {
					deployment.Spec.Template.Spec.Containers[0].Env = mergeContainerEnvs(
						deploymentTemplate.Spec.Containers[0].Env, certmanager.Spec.CAInjectorConfig.OverrideEnv)
				}
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
