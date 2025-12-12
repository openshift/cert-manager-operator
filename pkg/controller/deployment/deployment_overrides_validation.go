package deployment

import (
	"fmt"
	"unsafe"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1validation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/kubernetes/pkg/apis/core"
	corevalidation "k8s.io/kubernetes/pkg/apis/core/validation"
	"k8s.io/utils/strings/slices"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	certmanagerinformer "github.com/openshift/cert-manager-operator/pkg/operator/informers/externalversions/operator/v1alpha1"
)

// withContainerArgsValidateHook validates the container args with those that
// are supported by the operator.
func withContainerArgsValidateHook(certmanagerinformer certmanagerinformer.CertManagerInformer, deploymentName string) func(operatorSpec *operatorv1.OperatorSpec, deployment *appsv1.Deployment) error {
	supportedCertManagerArgs := []string{
		// A list of comma separated dns server endpoints used for ACME HTTP01 check requests.
		// This should be a list containing host and port, for example 8.8.8.8:53,8.8.4.4:53
		"--acme-http01-solver-nameservers",
		// Defines the resource limits CPU size when spawning new ACME HTTP01 challenge solver pods. (default "100m")
		"--acme-http01-solver-resource-limits-cpu",
		// Defines the resource limits Memory size when spawning new ACME HTTP01 challenge solver pods. (default "64Mi")
		"--acme-http01-solver-resource-limits-memory",
		// Defines the resource request CPU size when spawning new ACME HTTP01 challenge solver pods. (default "10m")
		"--acme-http01-solver-resource-request-cpu",
		// Defines the resource request Memory size when spawning new ACME HTTP01 challenge solver pods. (default "64Mi")
		"--acme-http01-solver-resource-request-memory",
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
		// Whether an issuer may make use of ambient credentials.
		// 'Ambient Credentials' are credentials drawn from the environment, metadata services,
		// or local files which are not explicitly configured in the Issuer API object.
		// When this flag is enabled, the following sources for credentials are also used:
		// AWS - All sources the Go SDK defaults to,
		// notably including any EC2 IAM roles available via instance metadata.
		// GCP - All sources for google.auth default authentication
		// i.e. following the same precedence and sources as that of
		// Application Default Credentials (ADC) per
		// https://cloud.google.com/docs/authentication/application-default-credentials#search_order
		"--issuer-ambient-credentials",
		// Whether to set the certificate resource as an owner of secret where the tls certificate
		// is stored. When this flag is enabled, the secret will be automatically removed when the
		// certificate resource is deleted.
		"--enable-certificate-owner-ref",
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

	return func(operatorSpec *operatorv1.OperatorSpec, deployment *appsv1.Deployment) error {
		certmanager, err := certmanagerinformer.Lister().Get("cluster")
		if err != nil {
			return fmt.Errorf("failed to get certmanager %q due to %w", "cluster", err)
		}

		argMap := make(map[string]string, 0)
		switch deploymentName {
		case certmanagerControllerDeployment:
			if certmanager.Spec.ControllerConfig != nil {
				parseArgMap(argMap, certmanager.Spec.ControllerConfig.OverrideArgs)
				return validateArgs(argMap, supportedCertManagerArgs)
			}
		case certmanagerWebhookDeployment:
			if certmanager.Spec.WebhookConfig != nil {
				parseArgMap(argMap, certmanager.Spec.WebhookConfig.OverrideArgs)
				return validateArgs(argMap, supportedCertManagerWebhookArgs)
			}
		case certmanagerCAinjectorDeployment:
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
func withContainerEnvValidateHook(certmanagerinformer certmanagerinformer.CertManagerInformer, deploymentName string) func(operatorSpec *operatorv1.OperatorSpec, deployment *appsv1.Deployment) error {
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

	return func(operatorSpec *operatorv1.OperatorSpec, deployment *appsv1.Deployment) error {
		certmanager, err := certmanagerinformer.Lister().Get("cluster")
		if err != nil {
			return fmt.Errorf("failed to get certmanager %q due to %w", "cluster", err)
		}

		envMap := make(map[string]corev1.EnvVar, 0)
		switch deploymentName {
		case certmanagerControllerDeployment:
			if certmanager.Spec.ControllerConfig != nil {
				parseEnvMap(envMap, certmanager.Spec.ControllerConfig.OverrideEnv)
				return validateEnv(envMap, supportedCertManagerEnv)
			}
		case certmanagerWebhookDeployment:
			if certmanager.Spec.WebhookConfig != nil {
				parseEnvMap(envMap, certmanager.Spec.WebhookConfig.OverrideEnv)
				return validateEnv(envMap, supportedCertManagerWebhookEnv)
			}
		case certmanagerCAinjectorDeployment:
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

// withPodLabelsValidateHook validates the pod labels from specific deployment config
// with those that are supported by the operator.
func withPodLabelsValidateHook(certmanagerinformer certmanagerinformer.CertManagerInformer, deploymentName string) func(operatorSpec *operatorv1.OperatorSpec, deployment *appsv1.Deployment) error {
	supportedCertManagerLabelKeys := []string{
		"azure.workload.identity/use",
	}
	supportedCertManagerWebhookLabelKeys := []string{}
	supportedCertManagerCainjectorLabelKeys := []string{}

	validateLabels := func(labels map[string]string, supportedLabelKeys []string) error {
		for k, v := range labels {
			if !slices.Contains(supportedLabelKeys, k) {
				return fmt.Errorf("validation failed due to unsupported label %q=%q", k, v)
			}
		}
		return nil
	}

	return func(operatorSpec *operatorv1.OperatorSpec, deployment *appsv1.Deployment) error {
		certmanager, err := certmanagerinformer.Lister().Get("cluster")
		if err != nil {
			return fmt.Errorf("failed to get certmanager %q due to %w", "cluster", err)
		}

		switch deploymentName {
		case certmanagerControllerDeployment:
			if certmanager.Spec.ControllerConfig != nil {
				return validateLabels(certmanager.Spec.ControllerConfig.OverrideLabels, supportedCertManagerLabelKeys)
			}
		case certmanagerWebhookDeployment:
			if certmanager.Spec.WebhookConfig != nil {
				return validateLabels(certmanager.Spec.WebhookConfig.OverrideLabels, supportedCertManagerWebhookLabelKeys)
			}
		case certmanagerCAinjectorDeployment:
			if certmanager.Spec.CAInjectorConfig != nil {
				return validateLabels(certmanager.Spec.CAInjectorConfig.OverrideLabels, supportedCertManagerCainjectorLabelKeys)
			}
		default:
			return fmt.Errorf("unsupported deployment name %q provided", deploymentName)
		}

		return nil
	}
}

// withContainerResourcesValidateHook validates the container resources with those that
// are supported by the operator.
func withContainerResourcesValidateHook(certmanagerinformer certmanagerinformer.CertManagerInformer, deploymentName string) func(operatorSpec *operatorv1.OperatorSpec, deployment *appsv1.Deployment) error {
	supportedCertManagerResourceNames := []string{
		string(corev1.ResourceCPU), string(corev1.ResourceMemory),
	}
	supportedCertManagerWebhookResourceNames := []string{
		string(corev1.ResourceCPU), string(corev1.ResourceMemory),
	}
	supportedCertManagerCainjectorResourceNames := []string{
		string(corev1.ResourceCPU), string(corev1.ResourceMemory),
	}

	return func(operatorSpec *operatorv1.OperatorSpec, deployment *appsv1.Deployment) error {
		certmanager, err := certmanagerinformer.Lister().Get("cluster")
		if err != nil {
			return fmt.Errorf("failed to get certmanager %q due to %w", "cluster", err)
		}

		switch deploymentName {
		case certmanagerControllerDeployment:
			if certmanager.Spec.ControllerConfig != nil {
				return validateResources(certmanager.Spec.ControllerConfig.OverrideResources, supportedCertManagerResourceNames)
			}
		case certmanagerWebhookDeployment:
			if certmanager.Spec.WebhookConfig != nil {
				return validateResources(certmanager.Spec.WebhookConfig.OverrideResources, supportedCertManagerWebhookResourceNames)
			}
		case certmanagerCAinjectorDeployment:
			if certmanager.Spec.CAInjectorConfig != nil {
				return validateResources(certmanager.Spec.CAInjectorConfig.OverrideResources, supportedCertManagerCainjectorResourceNames)
			}
		default:
			return fmt.Errorf("unsupported deployment name %q provided", deploymentName)
		}

		return nil
	}
}

// validateResources validates the resources with those that are in supportedResourceNames.
func validateResources(resources v1alpha1.CertManagerResourceRequirements, supportedResourceNames []string) error {
	errs := []error{}
	for k, v := range resources.Limits {
		if !slices.Contains(supportedResourceNames, string(k)) {
			errs = append(errs, fmt.Errorf("validation failed due to unsupported resource limits %q=%s", k, v.String()))
		}
	}
	for k, v := range resources.Requests {
		if !slices.Contains(supportedResourceNames, string(k)) {
			errs = append(errs, fmt.Errorf("validation failed due to unsupported resource requests %q=%s", k, v.String()))
		}
	}
	return utilerrors.NewAggregate(errs)
}

// withPodSchedulingValidateHook validates the overrides scheduling field for each operand.
func withPodSchedulingValidateHook(certmanagerinformer certmanagerinformer.CertManagerInformer, deploymentName string) func(operatorSpec *operatorv1.OperatorSpec, deployment *appsv1.Deployment) error {
	return func(operatorSpec *operatorv1.OperatorSpec, deployment *appsv1.Deployment) error {
		certmanager, err := certmanagerinformer.Lister().Get("cluster")
		if err != nil {
			return fmt.Errorf("failed to get certmanager %q due to %w", "cluster", err)
		}

		switch deploymentName {
		case certmanagerControllerDeployment:
			if certmanager.Spec.ControllerConfig != nil {
				return validateScheduling(certmanager.Spec.ControllerConfig.OverrideScheduling,
					field.NewPath("spec", "controllerConfig", "overrideScheduling"))
			}
		case certmanagerWebhookDeployment:
			if certmanager.Spec.WebhookConfig != nil {
				return validateScheduling(certmanager.Spec.WebhookConfig.OverrideScheduling,
					field.NewPath("spec", "webhookConfig", "overrideScheduling"))
			}
		case certmanagerCAinjectorDeployment:
			if certmanager.Spec.CAInjectorConfig != nil {
				return validateScheduling(certmanager.Spec.CAInjectorConfig.OverrideScheduling,
					field.NewPath("spec", "cainjectorConfig", "overrideScheduling"))
			}
		default:
			return fmt.Errorf("unsupported deployment name %q provided", deploymentName)
		}

		return nil
	}
}

// validateScheduling validates the cert manager scheduling field.
func validateScheduling(scheduling v1alpha1.CertManagerScheduling, fldPath *field.Path) error {
	errs := metav1validation.ValidateLabels(scheduling.NodeSelector, fldPath.Child("nodeSelector"))

	// Convert corev1.Tolerations to core.Tolerations.
	tolerations := *(*[]core.Toleration)(unsafe.Pointer(&scheduling.Tolerations))

	errs = append(errs, corevalidation.ValidateTolerations(tolerations, fldPath.Child("tolerations"))...)

	return errs.ToAggregate()
}
