package deployment

import (
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"

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
	}
	if len(unsupportedConfigOverrides.CAInjector.Args) > 0 && deployment.Name == "cert-manager-cainjector" {
		deployment.Spec.Template.Spec.Containers[0].Args = mergeContainerArgs(
			deployment.Spec.Template.Spec.Containers[0].Args,
			unsupportedConfigOverrides.CAInjector.Args)
	}
	if len(unsupportedConfigOverrides.Controller.Args) > 0 && deployment.Name == "cert-manager" {
		deployment.Spec.Template.Spec.Containers[0].Args = mergeContainerArgs(
			deployment.Spec.Template.Spec.Containers[0].Args,
			unsupportedConfigOverrides.Controller.Args)
	}
	return deployment
}

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
	return destArgs
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
