package deployment

import (
	"errors"
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

var (
	errUnsupportedArg           = errors.New("validation failed due to unsupported arg")
	errUnsupportedLabel         = errors.New("validation failed due to unsupported label")
	errUnsupportedResourceLimit = errors.New("validation failed due to unsupported resource limits")
	errUnsupportedResourceReq   = errors.New("validation failed due to unsupported resource requests")
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
				return fmt.Errorf("%w: %q=%q", errUnsupportedArg, k, v)
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
		supportedArgs, err := getSupportedArgsForDeployment(deploymentName, certmanager, supportedCertManagerArgs, supportedCertManagerWebhookArgs, supportedCertManageCainjectorArgs)
		if err != nil {
			return err
		}
		if supportedArgs == nil {
			return nil
		}

		parseArgMap(argMap, getOverrideArgsForDeployment(deploymentName, certmanager))
		return validateArgs(argMap, supportedArgs)
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
				return fmt.Errorf("%w: %q=%q", errUnsupportedArg, k, v)
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
		supportedEnv, err := getSupportedEnvForDeployment(deploymentName, certmanager, supportedCertManagerEnv, supportedCertManagerWebhookEnv, supportedCertManageCainjectorEnv)
		if err != nil {
			return err
		}
		if supportedEnv == nil {
			return nil
		}

		parseEnvMap(envMap, getOverrideEnvForDeployment(deploymentName, certmanager))
		return validateEnv(envMap, supportedEnv)
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
				return fmt.Errorf("%w: %q=%q", errUnsupportedLabel, k, v)
			}
		}
		return nil
	}

	return func(operatorSpec *operatorv1.OperatorSpec, deployment *appsv1.Deployment) error {
		certmanager, err := certmanagerinformer.Lister().Get("cluster")
		if err != nil {
			return fmt.Errorf("failed to get certmanager %q due to %w", "cluster", err)
		}

		supportedLabelKeys, overrideLabels, err := getSupportedLabelsForDeployment(deploymentName, certmanager, supportedCertManagerLabelKeys, supportedCertManagerWebhookLabelKeys, supportedCertManagerCainjectorLabelKeys)
		if err != nil {
			return err
		}
		if supportedLabelKeys == nil {
			return nil
		}

		return validateLabels(overrideLabels, supportedLabelKeys)
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
			errs = append(errs, fmt.Errorf("%w: %q=%s", errUnsupportedResourceLimit, k, v.String()))
		}
	}
	for k, v := range resources.Requests {
		if !slices.Contains(supportedResourceNames, string(k)) {
			errs = append(errs, fmt.Errorf("%w: %q=%s", errUnsupportedResourceReq, k, v.String()))
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

func getSupportedArgsForDeployment(deploymentName string, certmanager *v1alpha1.CertManager, supportedCertManagerArgs, supportedCertManagerWebhookArgs, supportedCertManageCainjectorArgs []string) ([]string, error) {
	switch deploymentName {
	case certmanagerControllerDeployment:
		if certmanager.Spec.ControllerConfig != nil {
			return supportedCertManagerArgs, nil
		}
		return nil, nil
	case certmanagerWebhookDeployment:
		if certmanager.Spec.WebhookConfig != nil {
			return supportedCertManagerWebhookArgs, nil
		}
		return nil, nil
	case certmanagerCAinjectorDeployment:
		if certmanager.Spec.CAInjectorConfig != nil {
			return supportedCertManageCainjectorArgs, nil
		}
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported deployment name %q provided", deploymentName)
	}
}

func getOverrideArgsForDeployment(deploymentName string, certmanager *v1alpha1.CertManager) []string {
	switch deploymentName {
	case certmanagerControllerDeployment:
		if certmanager.Spec.ControllerConfig != nil {
			return certmanager.Spec.ControllerConfig.OverrideArgs
		}
	case certmanagerWebhookDeployment:
		if certmanager.Spec.WebhookConfig != nil {
			return certmanager.Spec.WebhookConfig.OverrideArgs
		}
	case certmanagerCAinjectorDeployment:
		if certmanager.Spec.CAInjectorConfig != nil {
			return certmanager.Spec.CAInjectorConfig.OverrideArgs
		}
	}
	return nil
}

func getSupportedEnvForDeployment(deploymentName string, certmanager *v1alpha1.CertManager, supportedCertManagerEnv, supportedCertManagerWebhookEnv, supportedCertManageCainjectorEnv []string) ([]string, error) {
	switch deploymentName {
	case certmanagerControllerDeployment:
		if certmanager.Spec.ControllerConfig != nil {
			return supportedCertManagerEnv, nil
		}
		return nil, nil
	case certmanagerWebhookDeployment:
		if certmanager.Spec.WebhookConfig != nil {
			return supportedCertManagerWebhookEnv, nil
		}
		return nil, nil
	case certmanagerCAinjectorDeployment:
		if certmanager.Spec.CAInjectorConfig != nil {
			return supportedCertManageCainjectorEnv, nil
		}
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported deployment name %q provided", deploymentName)
	}
}

func getOverrideEnvForDeployment(deploymentName string, certmanager *v1alpha1.CertManager) []corev1.EnvVar {
	switch deploymentName {
	case certmanagerControllerDeployment:
		if certmanager.Spec.ControllerConfig != nil {
			return certmanager.Spec.ControllerConfig.OverrideEnv
		}
	case certmanagerWebhookDeployment:
		if certmanager.Spec.WebhookConfig != nil {
			return certmanager.Spec.WebhookConfig.OverrideEnv
		}
	case certmanagerCAinjectorDeployment:
		if certmanager.Spec.CAInjectorConfig != nil {
			return certmanager.Spec.CAInjectorConfig.OverrideEnv
		}
	}
	return nil
}

func getSupportedLabelsForDeployment(deploymentName string, certmanager *v1alpha1.CertManager, supportedCertManagerLabelKeys, supportedCertManagerWebhookLabelKeys, supportedCertManagerCainjectorLabelKeys []string) ([]string, map[string]string, error) {
	switch deploymentName {
	case certmanagerControllerDeployment:
		if certmanager.Spec.ControllerConfig != nil {
			return supportedCertManagerLabelKeys, certmanager.Spec.ControllerConfig.OverrideLabels, nil
		}
		return nil, nil, nil
	case certmanagerWebhookDeployment:
		if certmanager.Spec.WebhookConfig != nil {
			return supportedCertManagerWebhookLabelKeys, certmanager.Spec.WebhookConfig.OverrideLabels, nil
		}
		return nil, nil, nil
	case certmanagerCAinjectorDeployment:
		if certmanager.Spec.CAInjectorConfig != nil {
			return supportedCertManagerCainjectorLabelKeys, certmanager.Spec.CAInjectorConfig.OverrideLabels, nil
		}
		return nil, nil, nil
	default:
		return nil, nil, fmt.Errorf("unsupported deployment name %q provided", deploymentName)
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
