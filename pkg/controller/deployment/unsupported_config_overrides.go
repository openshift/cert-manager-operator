package deployment

import (
	appsv1 "k8s.io/api/apps/v1"

	"github.com/openshift/cert-manager-operator/apis/operator/v1alpha1"
)

func UnsupportedConfigOverrides(deployment *appsv1.Deployment, unsupportedConfigOverrides *v1alpha1.UnsupportedConfigOverrides) *appsv1.Deployment {
	// If the unsupportedConfigOverrides is nil, just continue
	if unsupportedConfigOverrides == nil {
		return deployment
	}

	// Handle Args injection
	// Add the container args for the webhook container
	if len(unsupportedConfigOverrides.Webhook.Args) > 0 && deployment.Name == "cert-manager-webhook" {
		deployment.Spec.Template.Spec.Containers[0].Args = unsupportedConfigOverrides.Webhook.Args
	}
	// Add the container args for the cainjector container
	if len(unsupportedConfigOverrides.CAInjector.Args) > 0 && deployment.Name == "cert-manager-cainjector" {
		deployment.Spec.Template.Spec.Containers[0].Args = unsupportedConfigOverrides.CAInjector.Args
	}
	// Add the container args for the controller container
	if len(unsupportedConfigOverrides.Controller.Args) > 0 && deployment.Name == "cert-manager" {
		deployment.Spec.Template.Spec.Containers[0].Args = unsupportedConfigOverrides.Controller.Args
	}

	// Handle EnvVars injection
	// Append the container envVars for the webhook container
	if len(unsupportedConfigOverrides.Webhook.EnvVars) > 0 && deployment.Name == "cert-manager-webhook" {
		deployment.Spec.Template.Spec.Containers[0].Env = append(deployment.Spec.Template.Spec.Containers[0].Env, unsupportedConfigOverrides.Webhook.EnvVars...)
	}
	// Append the container envVars for the cainjector container
	if len(unsupportedConfigOverrides.CAInjector.EnvVars) > 0 && deployment.Name == "cert-manager-cainjector" {
		deployment.Spec.Template.Spec.Containers[0].Env = append(deployment.Spec.Template.Spec.Containers[0].Env, unsupportedConfigOverrides.CAInjector.EnvVars...)
	}
	// Append the container envVars for the controller container
	if len(unsupportedConfigOverrides.Controller.EnvVars) > 0 && deployment.Name == "cert-manager" {
		deployment.Spec.Template.Spec.Containers[0].Env = append(deployment.Spec.Template.Spec.Containers[0].Env, unsupportedConfigOverrides.Controller.EnvVars...)
	}

	// Return the formatted Deployment
	return deployment
}
