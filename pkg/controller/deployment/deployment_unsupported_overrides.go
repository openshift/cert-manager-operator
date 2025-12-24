package deployment

import (
	"encoding/json"

	appsv1 "k8s.io/api/apps/v1"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

// unsupportedConfigOverrides overrides the values of container args in the deployment with
// those provided in operatorSpec.
func unsupportedConfigOverrides(deployment *appsv1.Deployment, unsupportedConfigOverrides *v1alpha1.UnsupportedConfigOverrides) *appsv1.Deployment {
	if unsupportedConfigOverrides == nil {
		return deployment
	}
	if len(unsupportedConfigOverrides.Webhook.Args) > 0 && deployment.Name == certmanagerWebhookDeployment {
		deployment.Spec.Template.Spec.Containers[0].Args = mergeContainerArgs(
			deployment.Spec.Template.Spec.Containers[0].Args,
			unsupportedConfigOverrides.Webhook.Args)
	}
	if len(unsupportedConfigOverrides.CAInjector.Args) > 0 && deployment.Name == certmanagerCAinjectorDeployment {
		deployment.Spec.Template.Spec.Containers[0].Args = mergeContainerArgs(
			deployment.Spec.Template.Spec.Containers[0].Args,
			unsupportedConfigOverrides.CAInjector.Args)
	}
	if len(unsupportedConfigOverrides.Controller.Args) > 0 && deployment.Name == certmanagerControllerDeployment {
		deployment.Spec.Template.Spec.Containers[0].Args = mergeContainerArgs(
			deployment.Spec.Template.Spec.Containers[0].Args,
			unsupportedConfigOverrides.Controller.Args)
	}
	return deployment
}

// withUnsupportedArgsOverrideHook overrides the container args with those provided by
// UnsupportedConfigOverrides in the operatorSpec.
func withUnsupportedArgsOverrideHook(operatorSpec *operatorv1.OperatorSpec, deployment *appsv1.Deployment) error {
	cfg := &v1alpha1.UnsupportedConfigOverrides{}
	if len(operatorSpec.UnsupportedConfigOverrides.Raw) != 0 {
		err := json.Unmarshal(operatorSpec.UnsupportedConfigOverrides.Raw, cfg)
		if err != nil {
			return err //nolint:wrapcheck // json.Unmarshal error is already clear
		}
	}
	deployment = unsupportedConfigOverrides(deployment, cfg) //nolint:staticcheck,wastedassign // SA4006: deployment is modified in place, assignment kept for clarity
	return nil
}
