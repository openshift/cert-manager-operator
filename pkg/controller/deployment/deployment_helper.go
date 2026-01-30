package deployment

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"unsafe"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/kubernetes/pkg/apis/core"
	"k8s.io/kubernetes/pkg/util/tolerations"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	certmanagerinformer "github.com/openshift/cert-manager-operator/pkg/operator/informers/externalversions/operator/v1alpha1"
)

var (
	errUnsupportedDeploymentName = errors.New("unsupported deployment name provided")
)

const argKeyValSeparator = "="

// mergeContainerArgs merges the source args with override values
// using a map that tracks unique keys for each arg containing a
// key value pair of form `key[=value]`.
func mergeContainerArgs(sourceArgs []string, overrideArgs []string) (destArgs []string) {
	destArgMap := map[string]string{}
	parseArgMap(destArgMap, sourceArgs)
	parseArgMap(destArgMap, overrideArgs)

	destArgs = make([]string, len(destArgMap))
	i := 0
	for key, val := range destArgMap {
		if len(val) > 0 {
			destArgs[i] = fmt.Sprintf("%s%s%s", key, argKeyValSeparator, val)
		} else {
			destArgs[i] = key
		}
		i++
	}
	sort.Strings(destArgs)
	return destArgs
}

// mergeContainerEnvs merges source container env variables with those
// provided as override env variables.
func mergeContainerEnvs(sourceEnvs []corev1.EnvVar, overrideEnvs []corev1.EnvVar) []corev1.EnvVar {
	destEnvsMap := map[string]corev1.EnvVar{}
	parseEnvMap(destEnvsMap, sourceEnvs)
	parseEnvMap(destEnvsMap, overrideEnvs)

	keys := make([]string, 0)
	for k := range destEnvsMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	destEnvs := make([]corev1.EnvVar, 0)
	for _, k := range keys {
		destEnvs = append(destEnvs, destEnvsMap[k])
	}
	return destEnvs
}

func parseEnvMap(envMap map[string]corev1.EnvVar, envs []corev1.EnvVar) {
	for _, env := range envs {
		envMap[env.Name] = env
	}
}

// parseArgMap adds new entries to the map using keys
// parsed from each arg (of the form `key=[value]`) from the
// list of args.
func parseArgMap(argMap map[string]string, args []string) {
	for _, arg := range args {
		splitted := strings.Split(arg, argKeyValSeparator)
		if len(splitted) > 0 && arg != "" {
			key := splitted[0]
			// ensure that for given arg eg. "--gate=FeatureA=true"Config
			// the value remains "FeatureA=true" instead of just "FeatureA"
			value := strings.Join(splitted[1:], argKeyValSeparator)
			argMap[key] = value
		}
	}
}

// mergeContainerResources merges source container resources with that
// provided as override resources.
func mergeContainerResources(sourceResources corev1.ResourceRequirements, overrideResources v1alpha1.CertManagerResourceRequirements) corev1.ResourceRequirements {
	sourceResources.Limits = mergeContainerResourceList(sourceResources.Limits, overrideResources.Limits)
	sourceResources.Requests = mergeContainerResourceList(sourceResources.Requests, overrideResources.Requests)

	return sourceResources
}

// mergeContainerResourceList merges source resource list with that
// provided as override resource list. Only cpu and memory resource
// values are overridden.
func mergeContainerResourceList(sourceResourceList corev1.ResourceList, overrideResourceList corev1.ResourceList) corev1.ResourceList {
	if overrideResourceList == nil {
		return sourceResourceList
	}

	if sourceResourceList == nil {
		sourceResourceList = corev1.ResourceList{}
	}

	for _, resource := range []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory} {
		if quantity, exists := overrideResourceList[resource]; exists && !quantity.IsZero() {
			sourceResourceList[resource] = quantity.DeepCopy()
		}
	}

	return sourceResourceList
}

// mergePodScheduling merges source scheduling with that provided as override scheduling.
func mergePodScheduling(sourceScheduling v1alpha1.CertManagerScheduling, overrideScheduling v1alpha1.CertManagerScheduling) v1alpha1.CertManagerScheduling {
	// Merge the source and override NodeSelector.
	mergedNodeSelector := labels.Merge(sourceScheduling.NodeSelector, overrideScheduling.NodeSelector)

	// Convert corev1.Tolerations to core.Tolerations.
	sourceTolerations := *(*[]core.Toleration)(unsafe.Pointer(&sourceScheduling.Tolerations))
	overridingTolerations := *(*[]core.Toleration)(unsafe.Pointer(&overrideScheduling.Tolerations))

	// Merge the source and override Tolerations.
	mergedCoreTolerations := tolerations.MergeTolerations(sourceTolerations, overridingTolerations)

	// Convert core.Tolerations to corev1.Tolerations.
	mergedCorev1Tolerations := *(*[]corev1.Toleration)(unsafe.Pointer(&mergedCoreTolerations))

	return v1alpha1.CertManagerScheduling{
		NodeSelector: mergedNodeSelector,
		Tolerations:  mergedCorev1Tolerations,
	}
}

// getOverrideArgsFor is a helper function that returns the overrideArgs provided
// in the operator spec based on the deployment name.
func getOverrideArgsFor(certmanagerinformer certmanagerinformer.CertManagerInformer, deploymentName string) ([]string, error) {
	certmanager, err := certmanagerinformer.Lister().Get("cluster")
	if err != nil {
		return nil, fmt.Errorf("failed to get certmanager %q due to %w", "cluster", err)
	}

	switch deploymentName {
	case certmanagerControllerDeployment:
		if certmanager.Spec.ControllerConfig != nil {
			return certmanager.Spec.ControllerConfig.OverrideArgs, nil
		}
	case certmanagerWebhookDeployment:
		if certmanager.Spec.WebhookConfig != nil {
			return certmanager.Spec.WebhookConfig.OverrideArgs, nil
		}
	case certmanagerCAinjectorDeployment:
		if certmanager.Spec.CAInjectorConfig != nil {
			return certmanager.Spec.CAInjectorConfig.OverrideArgs, nil
		}
	default:
		return nil, fmt.Errorf("%w: %q", errUnsupportedDeploymentName, deploymentName)
	}
	return nil, nil
}

// getOverrideEnvFor() is a helper function that returns the OverrideEnv provided
// in the operator spec based on the deployment name.
func getOverrideEnvFor(certmanagerinformer certmanagerinformer.CertManagerInformer, deploymentName string) ([]corev1.EnvVar, error) {
	certmanager, err := certmanagerinformer.Lister().Get("cluster")
	if err != nil {
		return nil, fmt.Errorf("failed to get certmanager %q due to %w", "cluster", err)
	}

	switch deploymentName {
	case certmanagerControllerDeployment:
		if certmanager.Spec.ControllerConfig != nil {
			return certmanager.Spec.ControllerConfig.OverrideEnv, nil
		}
	case certmanagerWebhookDeployment:
		if certmanager.Spec.WebhookConfig != nil {
			return certmanager.Spec.WebhookConfig.OverrideEnv, nil
		}
	case certmanagerCAinjectorDeployment:
		if certmanager.Spec.CAInjectorConfig != nil {
			return certmanager.Spec.CAInjectorConfig.OverrideEnv, nil
		}
	default:
		return nil, fmt.Errorf("%w: %q", errUnsupportedDeploymentName, deploymentName)
	}
	return nil, nil
}

// getOverridePodLabelsFor() is a helper function that returns the OverridePodLabels provided
// in the operator spec based on the deployment name.
func getOverridePodLabelsFor(certmanagerinformer certmanagerinformer.CertManagerInformer, deploymentName string) (map[string]string, error) {
	certmanager, err := certmanagerinformer.Lister().Get("cluster")
	if err != nil {
		return nil, fmt.Errorf("failed to get certmanager %q due to %w", "cluster", err)
	}

	switch deploymentName {
	case certmanagerControllerDeployment:
		if certmanager.Spec.ControllerConfig != nil {
			return certmanager.Spec.ControllerConfig.OverrideLabels, nil
		}
	case certmanagerWebhookDeployment:
		if certmanager.Spec.WebhookConfig != nil {
			return certmanager.Spec.WebhookConfig.OverrideLabels, nil
		}
	case certmanagerCAinjectorDeployment:
		if certmanager.Spec.CAInjectorConfig != nil {
			return certmanager.Spec.CAInjectorConfig.OverrideLabels, nil
		}
	default:
		return nil, fmt.Errorf("%w: %q", errUnsupportedDeploymentName, deploymentName)
	}
	return nil, nil
}

// getOverrideReplicasFor is a helper function that returns the OverrideReplicas provided
// in the operator spec based on the deployment name.
func getOverrideReplicasFor(certmanagerinformer certmanagerinformer.CertManagerInformer, deploymentName string) (*int32, error) {
	certmanager, err := certmanagerinformer.Lister().Get("cluster")
	if err != nil {
		return nil, fmt.Errorf("failed to get certmanager %q due to %w", "cluster", err)
	}

	switch deploymentName {
	case certmanagerControllerDeployment:
		if certmanager.Spec.ControllerConfig != nil {
			return certmanager.Spec.ControllerConfig.OverrideReplicas, nil
		}
	case certmanagerWebhookDeployment:
		if certmanager.Spec.WebhookConfig != nil {
			return certmanager.Spec.WebhookConfig.OverrideReplicas, nil
		}
	case certmanagerCAinjectorDeployment:
		if certmanager.Spec.CAInjectorConfig != nil {
			return certmanager.Spec.CAInjectorConfig.OverrideReplicas, nil
		}
	default:
		return nil, fmt.Errorf("unsupported deployment name %q provided", deploymentName)
	}
	return nil, nil
}

// getOverrideResourcesFor is a helper function that returns the OverrideResources provided
// in the operator spec based on the deployment name.
func getOverrideResourcesFor(certmanagerinformer certmanagerinformer.CertManagerInformer, deploymentName string) (v1alpha1.CertManagerResourceRequirements, error) {
	certmanager, err := certmanagerinformer.Lister().Get("cluster")
	if err != nil {
		return v1alpha1.CertManagerResourceRequirements{}, fmt.Errorf("failed to get certmanager %q due to %w", "cluster", err)
	}

	switch deploymentName {
	case certmanagerControllerDeployment:
		if certmanager.Spec.ControllerConfig != nil {
			return certmanager.Spec.ControllerConfig.OverrideResources, nil
		}
	case certmanagerWebhookDeployment:
		if certmanager.Spec.WebhookConfig != nil {
			return certmanager.Spec.WebhookConfig.OverrideResources, nil
		}
	case certmanagerCAinjectorDeployment:
		if certmanager.Spec.CAInjectorConfig != nil {
			return certmanager.Spec.CAInjectorConfig.OverrideResources, nil
		}
	default:
		return v1alpha1.CertManagerResourceRequirements{}, fmt.Errorf("unsupported deployment name %q provided", deploymentName)
	}
	return v1alpha1.CertManagerResourceRequirements{}, nil
}

// getOverrideSchedulingFor is a helper function that returns the OverrideScheduling provided
// in the operator spec based on the deployment name.
func getOverrideSchedulingFor(certmanagerinformer certmanagerinformer.CertManagerInformer, deploymentName string) (v1alpha1.CertManagerScheduling, error) {
	certmanager, err := certmanagerinformer.Lister().Get("cluster")
	if err != nil {
		return v1alpha1.CertManagerScheduling{}, fmt.Errorf("failed to get certmanager %q due to %w", "cluster", err)
	}

	switch deploymentName {
	case certmanagerControllerDeployment:
		if certmanager.Spec.ControllerConfig != nil {
			return certmanager.Spec.ControllerConfig.OverrideScheduling, nil
		}
	case certmanagerWebhookDeployment:
		if certmanager.Spec.WebhookConfig != nil {
			return certmanager.Spec.WebhookConfig.OverrideScheduling, nil
		}
	case certmanagerCAinjectorDeployment:
		if certmanager.Spec.CAInjectorConfig != nil {
			return certmanager.Spec.CAInjectorConfig.OverrideScheduling, nil
		}
	default:
		return v1alpha1.CertManagerScheduling{}, fmt.Errorf("unsupported deployment name %q provided", deploymentName)
	}
	return v1alpha1.CertManagerScheduling{}, nil
}
