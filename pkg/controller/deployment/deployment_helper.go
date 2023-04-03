package deployment

import (
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"

	certmanagerinformer "github.com/openshift/cert-manager-operator/pkg/operator/informers/externalversions/operator/v1alpha1"
)

const argKeyValSeparator = "="

// mergeContainerArgs merges the source args with override values
// using a map that tracks unique keys for each arg containing a
// key value pair of form `key[=value]`
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
// list of args
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

// getOverrideArgsFor is a helper function that returns the overrideArgs provided
// in the operator spec based on the deployment name.
func getOverrideArgsFor(certmanagerinformer certmanagerinformer.CertManagerInformer, deploymentName string) ([]string, error) {
	certmanager, err := certmanagerinformer.Lister().Get("cluster")
	if err != nil {
		return nil, fmt.Errorf("failed to get certmanager %q due to %v", "cluster", err)
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
		return nil, fmt.Errorf("unsupported deployment name %q provided", deploymentName)
	}
	return nil, nil
}

// getOverrideEnvFor() is a helper function that returns the OverrideEnv provided
// in the operator spec based on the deployment name.
func getOverrideEnvFor(certmanagerinformer certmanagerinformer.CertManagerInformer, deploymentName string) ([]corev1.EnvVar, error) {
	certmanager, err := certmanagerinformer.Lister().Get("cluster")
	if err != nil {
		return nil, fmt.Errorf("failed to get certmanager %q due to %v", "cluster", err)
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
		return nil, fmt.Errorf("unsupported deployment name %q provided", deploymentName)
	}
	return nil, nil
}

// getOverridePodLabelsFor() is a helper function that returns the OverridePodLabels provided
// in the operator spec based on the deployment name.
func getOverridePodLabelsFor(certmanagerinformer certmanagerinformer.CertManagerInformer, deploymentName string) (map[string]string, error) {
	certmanager, err := certmanagerinformer.Lister().Get("cluster")
	if err != nil {
		return nil, fmt.Errorf("failed to get certmanager %q due to %v", "cluster", err)
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
		return nil, fmt.Errorf("unsupported deployment name %q provided", deploymentName)
	}
	return nil, nil

}
