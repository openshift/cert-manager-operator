package deployment

import (
	"fmt"
	"sort"
	"strings"
)

const (
	certmanagerControllerDeployment = "cert-manager"
	certmanagerWebhookDeployment    = "cert-manager-webhook"
	certmanagerCAinjectorDeployment = "cert-manager-cainjector"

	argKeyValSeparator = "="
)

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

// parseArgMap adds new entries to the map using keys
// parsed from each arg (of the form `key=[value]`) from the
// list of args.
func parseArgMap(argMap map[string]string, args []string) {
	for _, arg := range args {
		splitted := strings.Split(arg, argKeyValSeparator)
		if len(splitted) > 0 && arg != "" {
			key := splitted[0]
			// ensure that for given arg eg. "--gate=FeatureA=true"
			// the value remains "FeatureA=true" instead of just "FeatureA"
			value := strings.Join(splitted[1:], argKeyValSeparator)
			argMap[key] = value
		}
	}
}
