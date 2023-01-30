package deployment

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/strings/slices"

	v1 "github.com/openshift/api/operator/v1"

	alpha1 "github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned/typed/operator/v1alpha1"
)

// withContainerArgsValidateHook validates the container args with those that
// are supported by the operator.
func withContainerArgsValidateHook(certManagerClient alpha1.OperatorV1alpha1Interface, deploymentName string) func(operatorSpec *v1.OperatorSpec, deployment *appsv1.Deployment) error {

	supportedCertManagerArgs := []string{
		// A list of comma separated dns server endpoints used for ACME HTTP01 check requests.
		// This should be a list containing host and port, for example 8.8.8.8:53,8.8.4.4:53
		"--acme-http01-solver-nameservers",
		// A list of comma separated dns server endpoints used for DNS01 check requests.
		// This should be a list containing host and port, for example 8.8.8.8:53,8.8.4.4:53
		"--dns01-recursive-nameservers",
		// When true, cert-manager will only ever query the configured DNS resolvers to perform the
		// ACME DNS01 self check. This is useful in DNS constrained environments, where access
		// to authoritative nameservers is restricted. Enabling this option could cause the DNS01
		// self check to take longer due to caching performed by the recursive nameservers.
		"--dns01-recursive-nameservers-only",
		// Log Level
		"--v", "-V",
		// The host and port that the metrics endpoint should listen on. (default "0.0.0.0:9402")
		"--metrics-listen-address",
	}
	supportedCertManagerWebhookArgs := []string{
		// Log Level
		"--v", "-V",
	}
	supportedCertManageCainjectorArgs := []string{
		// Log Level
		"--v", "-V",
	}

	validateArgs := func(argMap map[string]string, supportedArgs []string) error {
		for k, v := range argMap {
			if !slices.Contains(supportedArgs, k) {
				return fmt.Errorf("validation failed due to unsupported arg %q=%q", k, v)
			}
		}
		return nil
	}

	return func(operatorSpec *v1.OperatorSpec, deployment *appsv1.Deployment) error {
		certmanager, err := certManagerClient.CertManagers().Get(context.Background(), "cluster", metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get certmanager %q due to %v", "cluster", err)
		}

		argMap := make(map[string]string, 0)
		switch deploymentName {
		case "cert-manager":
			if certmanager.Spec.ControllerConfig != nil {
				parseArgMap(argMap, certmanager.Spec.ControllerConfig.OverrideArgs)
				return validateArgs(argMap, supportedCertManagerArgs)
			}
		case "cert-manager-webhook":
			if certmanager.Spec.WebhookConfig != nil {
				parseArgMap(argMap, certmanager.Spec.WebhookConfig.OverrideArgs)
				return validateArgs(argMap, supportedCertManagerWebhookArgs)
			}
		case "cert-manager-cainjector":
			if certmanager.Spec.CAInjectorConfig != nil {
				parseArgMap(argMap, certmanager.Spec.CAInjectorConfig.OverrideArgs)
				return validateArgs(argMap, supportedCertManageCainjectorArgs)
			}
		default:
			return fmt.Errorf("unsupported deployment name %q provided", deploymentName)
		}

		return nil
	}
}

// withContainerEnvValidateHook validates the container env with those that
// are supported by the operator.
func withContainerEnvValidateHook(certManagerClient alpha1.OperatorV1alpha1Interface, deploymentName string) func(operatorSpec *v1.OperatorSpec, deployment *appsv1.Deployment) error {

	supportedCertManagerEnv := []string{
		"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY",
	}
	supportedCertManagerWebhookEnv := []string{}
	supportedCertManageCainjectorEnv := []string{}

	validateEnv := func(argMap map[string]corev1.EnvVar, supportedEnv []string) error {
		for k, v := range argMap {
			if !slices.Contains(supportedEnv, k) {
				return fmt.Errorf("validation failed due to unsupported arg %q=%q", k, v)
			}
		}
		return nil
	}

	return func(operatorSpec *v1.OperatorSpec, deployment *appsv1.Deployment) error {
		certmanager, err := certManagerClient.CertManagers().Get(context.Background(), "cluster", metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get certmanager %q due to %v", "cluster", err)
		}

		envMap := make(map[string]corev1.EnvVar, 0)
		switch deploymentName {
		case "cert-manager":
			if certmanager.Spec.ControllerConfig != nil {
				parseEnvMap(envMap, certmanager.Spec.ControllerConfig.OverrideEnv)
				return validateEnv(envMap, supportedCertManagerEnv)
			}
		case "cert-manager-webhook":
			if certmanager.Spec.WebhookConfig != nil {
				parseEnvMap(envMap, certmanager.Spec.WebhookConfig.OverrideEnv)
				return validateEnv(envMap, supportedCertManagerWebhookEnv)
			}
		case "cert-manager-cainjector":
			if certmanager.Spec.CAInjectorConfig != nil {
				parseEnvMap(envMap, certmanager.Spec.CAInjectorConfig.OverrideEnv)
				return validateEnv(envMap, supportedCertManageCainjectorEnv)
			}
		default:
			return fmt.Errorf("unsupported deployment name %q provided", deploymentName)
		}

		return nil
	}
}
