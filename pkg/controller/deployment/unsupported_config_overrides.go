package deployment

import (
	appsv1 "k8s.io/api/apps/v1"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

func UnsupportedConfigOverrides(deployment *appsv1.Deployment, unsupportedConfigOverrides *v1alpha1.UnsupportedConfigOverrides) *appsv1.Deployment {
	if unsupportedConfigOverrides == nil {
		return deployment
	}
	if len(unsupportedConfigOverrides.Webhook.Args) > 0 && deployment.Name == "cert-manager-webhook" {
		deployment.Spec.Template.Spec.Containers[0].Args = unsupportedConfigOverrides.Webhook.Args
	}
	if len(unsupportedConfigOverrides.CAInjector.Args) > 0 && deployment.Name == "cert-manager-cainjector" {
		deployment.Spec.Template.Spec.Containers[0].Args = unsupportedConfigOverrides.CAInjector.Args
	}
	if len(unsupportedConfigOverrides.Controller.Args) > 0 && deployment.Name == "cert-manager" {
		deployment.Spec.Template.Spec.Containers[0].Args = unsupportedConfigOverrides.Controller.Args
	}
	return deployment
}
