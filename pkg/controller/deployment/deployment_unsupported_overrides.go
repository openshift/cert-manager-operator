package deployment

import (
	"encoding/json"

	v1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	alpha1 "github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned/typed/operator/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
)

// unsupportedConfigOverrides overrides the values of container args in the deployment with
// those provided in operatorSpec.
func unsupportedConfigOverrides(deployment *appsv1.Deployment, unsupportedConfigOverrides *v1alpha1.UnsupportedConfigOverrides) *appsv1.Deployment {
	if unsupportedConfigOverrides == nil {
		return deployment
	}
	if len(unsupportedConfigOverrides.Webhook.Args) > 0 && deployment.Name == "cert-manager-webhook" {
		deployment.Spec.Template.Spec.Containers[0].Args = mergeContainerArgs(
			deployment.Spec.Template.Spec.Containers[0].Args,
			unsupportedConfigOverrides.Webhook.Args)
	}
	if len(unsupportedConfigOverrides.CAInjector.Args) > 0 && deployment.Name == "cert-manager-cainjector" {
		deployment.Spec.Template.Spec.Containers[0].Args = mergeContainerArgs(
			deployment.Spec.Template.Spec.Containers[0].Args,
			unsupportedConfigOverrides.CAInjector.Args)
	}
	if len(unsupportedConfigOverrides.Controller.Args) > 0 && deployment.Name == "cert-manager" {
		deployment.Spec.Template.Spec.Containers[0].Args = mergeContainerArgs(
			deployment.Spec.Template.Spec.Containers[0].Args,
			unsupportedConfigOverrides.Controller.Args)
	}
	return deployment
}

// withUnsupportedArgsOverrideHook overrides the container args with those provided by
// UnsupportedConfigOverrides in the operatorSpec.
func withUnsupportedArgsOverrideHook(certManagerClient alpha1.OperatorV1alpha1Interface, deploymentName string) func(operatorSpec *v1.OperatorSpec, deployment *appsv1.Deployment) error {
	return func(operatorSpec *v1.OperatorSpec, deployment *appsv1.Deployment) error {

		cfg := &v1alpha1.UnsupportedConfigOverrides{}
		if len(operatorSpec.UnsupportedConfigOverrides.Raw) != 0 {
			err := json.Unmarshal(operatorSpec.UnsupportedConfigOverrides.Raw, cfg)
			if err != nil {
				return err
			}
		}
		deployment = unsupportedConfigOverrides(deployment, cfg)
		return nil
	}
}
