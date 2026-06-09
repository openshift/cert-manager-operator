package common

import (
	"sort"
	"strings"
)

const argKeyValSeparator = "="

// MergeContainerArgs merges source args with override values using a map that
// tracks unique keys for each arg of the form key[=value].
func MergeContainerArgs(sourceArgs []string, overrideArgs []string) []string {
	destArgMap := make(map[string]string)
	ParseArgMap(destArgMap, sourceArgs)
	ParseArgMap(destArgMap, overrideArgs)

	destArgs := make([]string, len(destArgMap))
	i := 0
	for key, val := range destArgMap {
		if len(val) > 0 {
			destArgs[i] = key + argKeyValSeparator + val
		} else {
			destArgs[i] = key
		}
		i++
	}
	sort.Strings(destArgs)
	return destArgs
}

// ParseArgMap adds entries to argMap using keys parsed from each arg (key=[value]).
func ParseArgMap(argMap map[string]string, args []string) {
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

// StripArgsByKeys removes arguments whose key (text before the first '=') is in keys.
func StripArgsByKeys(args []string, keys map[string]struct{}) []string {
	if len(keys) == 0 {
		return args
	}
	out := make([]string, 0, len(args))
	for _, arg := range args {
		key := strings.SplitN(arg, argKeyValSeparator, 2)[0]
		if _, drop := keys[key]; drop {
			continue
		}
		out = append(out, arg)
	}
	return out
}

// ArgKeysSet returns a set of argument keys for use with StripArgsByKeys.
func ArgKeysSet(keys []string) map[string]struct{} {
	m := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		m[k] = struct{}{}
	}
	return m
}
