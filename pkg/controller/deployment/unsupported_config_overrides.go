package deployment

import (
	"fmt"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

const argKeyValSeparator = "="

// UnsupportedConfigOverrides overrides the values of container args in the deployment
func UnsupportedConfigOverrides(deployment *appsv1.Deployment, unsupportedConfigOverrides *v1alpha1.UnsupportedConfigOverrides) *appsv1.Deployment {
	if unsupportedConfigOverrides == nil {
		return deployment
	}
	if len(unsupportedConfigOverrides.Webhook.Args) > 0 && deployment.Name == "cert-manager-webhook" {
		deployment.Spec.Template.Spec.Containers[0].Args = mergeContainerArgs(
			deployment.Spec.Template.Spec.Containers[0].Args,
			unsupportedConfigOverrides.Webhook.Args)

		sort.Strings(deployment.Spec.Template.Spec.Containers[0].Args)
	}
	if len(unsupportedConfigOverrides.CAInjector.Args) > 0 && deployment.Name == "cert-manager-cainjector" {
		deployment.Spec.Template.Spec.Containers[0].Args = mergeContainerArgs(
			deployment.Spec.Template.Spec.Containers[0].Args,
			unsupportedConfigOverrides.CAInjector.Args)

		sort.Strings(deployment.Spec.Template.Spec.Containers[0].Args)

	}
	if len(unsupportedConfigOverrides.Controller.Args) > 0 && deployment.Name == "cert-manager" {
		deployment.Spec.Template.Spec.Containers[0].Args = mergeContainerArgs(
			deployment.Spec.Template.Spec.Containers[0].Args,
			unsupportedConfigOverrides.Controller.Args)

		sort.Strings(deployment.Spec.Template.Spec.Containers[0].Args)
	}
	return deployment
}

// mergeContainerArgs merges the source args with override values
// using a map that tracks unique keys for each arg containing a
// key value pair of form `key[=value]`
func mergeContainerArgs(sourceArgs []string, overrideArgs []string) []string {
	destArgMap := map[string]string{}
	parseArgMap(destArgMap, sourceArgs)
	parseArgMap(destArgMap, overrideArgs)

	destArgs := make([]string, len(destArgMap))
	i := 0
	for key, val := range destArgMap {
		if len(val) > 0 {
			destArgs[i] = fmt.Sprintf("%s%s%s", key, argKeyValSeparator, val)
		} else {
			destArgs[i] = key
		}
		i++
	}
	return sort.StringSlice(destArgs)
}

// mergeContainerEnvs merges source container env variables with those
// provided as override env variables.
func mergeContainerEnvs(sourceEnvs []corev1.EnvVar, overrideEnvs []corev1.EnvVar) []corev1.EnvVar {
	destEnvsMap := map[string]corev1.EnvVar{}
	parseEnvMap(destEnvsMap, sourceEnvs)
	parseEnvMap(destEnvsMap, overrideEnvs)

	destEnvs := make([]corev1.EnvVar, 0)
	for _, v := range destEnvsMap {
		destEnvs = append(destEnvs, v)
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
