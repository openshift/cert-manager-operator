package deployment

import (
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	v1 "github.com/openshift/api/operator/v1"

	certmanagerinformer "github.com/openshift/cert-manager-operator/pkg/operator/informers/externalversions/operator/v1alpha1"
)

// overrideArgsFunc defines a function signature that is accepted by
// withContainerArgsOverrideHook(). This function returns the
// override args provided to the cert-manager-operator operator spec.
type overrideArgsFunc func(certmanagerinformer.CertManagerInformer, string) ([]string, error)

// overrideArgsFunc defines a function signature that is accepted by
// withContainerEnvOverrideHook(). This function returns the
// override env provided to the cert-manager-operator operator spec.
type overrideEnvFunc func(certmanagerinformer.CertManagerInformer, string) ([]corev1.EnvVar, error)

// withOperandImageOverrideHook overrides the deployment image with
// the operand images provided to the operator.
func withOperandImageOverrideHook(operatorSpec *v1.OperatorSpec, deployment *appsv1.Deployment) error {
	for index := range deployment.Spec.Template.Spec.Containers {
		deployment.Spec.Template.Spec.Containers[index].Image = certManagerImage(deployment.Spec.Template.Spec.Containers[index].Image)
	}
	return nil
}

// withContainerArgsOverrideHook overrides the container args with those provided by
// the overrideArgsFunc function.
func withContainerArgsOverrideHook(certmanagerinformer certmanagerinformer.CertManagerInformer, deploymentName string, fn overrideArgsFunc) func(operatorSpec *v1.OperatorSpec, deployment *appsv1.Deployment) error {
	return func(operatorSpec *v1.OperatorSpec, deployment *appsv1.Deployment) error {
		overrideArgs, err := fn(certmanagerinformer, deploymentName)
		if err != nil {
			return err
		}

		if overrideArgs != nil && len(overrideArgs) > 0 && len(deployment.Spec.Template.Spec.Containers) == 1 && deployment.Name == deploymentName {
			deployment.Spec.Template.Spec.Containers[0].Args = mergeContainerArgs(
				deployment.Spec.Template.Spec.Containers[0].Args, overrideArgs)
			sort.Strings(deployment.Spec.Template.Spec.Containers[0].Args)
		}
		return nil
	}
}

// withContainerEnvOverrideHook verrides the container env with those provided by
// the overrideEnvFunc function.
func withContainerEnvOverrideHook(certmanagerinformer certmanagerinformer.CertManagerInformer, deploymentName string, fn overrideEnvFunc) func(operatorSpec *v1.OperatorSpec, deployment *appsv1.Deployment) error {
	return func(operatorSpec *v1.OperatorSpec, deployment *appsv1.Deployment) error {
		overrideEnv, err := fn(certmanagerinformer, deploymentName)
		if err != nil {
			return err
		}

		if overrideEnv != nil && len(overrideEnv) > 0 && len(deployment.Spec.Template.Spec.Containers) == 1 && deployment.Name == deploymentName {
			deployment.Spec.Template.Spec.Containers[0].Env = mergeContainerEnvs(
				deployment.Spec.Template.Spec.Containers[0].Env, overrideEnv)

		}
		return nil
	}
}
