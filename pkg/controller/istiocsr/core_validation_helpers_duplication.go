package istiocsr

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1validation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/kubernetes/pkg/apis/core"
	corevalidation "k8s.io/kubernetes/pkg/apis/core/validation"
)

/*
 * Methods here are the replicates of the methods defined in k8s.io/kubernetes/pkg/apis/core/validation package, which
 * is done just because of the lack of better alternative to use private methods.
 * TODO: Remove this source file when validateAffinity method is made public.
 */

// validateAffinity checks if given affinities are valid.
func validateAffinity(affinity *core.Affinity, opts corevalidation.PodValidationOptions, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if affinity != nil {
		if affinity.NodeAffinity != nil {
			allErrs = append(allErrs, validateNodeAffinity(affinity.NodeAffinity, fldPath.Child("nodeAffinity"))...)
		}
		if affinity.PodAffinity != nil {
			allErrs = append(allErrs, validatePodAffinity(affinity.PodAffinity, opts.AllowInvalidLabelValueInSelector, fldPath.Child("podAffinity"))...)
		}
		if affinity.PodAntiAffinity != nil {
			allErrs = append(allErrs, validatePodAntiAffinity(affinity.PodAntiAffinity, opts.AllowInvalidLabelValueInSelector, fldPath.Child("podAntiAffinity"))...)
		}
	}

	return allErrs
}

// validateNodeAffinity tests that the specified nodeAffinity fields have valid data.
func validateNodeAffinity(na *core.NodeAffinity, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	// TODO: Uncomment the next three lines once RequiredDuringSchedulingRequiredDuringExecution is implemented.
	// if na.RequiredDuringSchedulingRequiredDuringExecution != nil {
	//	allErrs = append(allErrs, ValidateNodeSelector(na.RequiredDuringSchedulingRequiredDuringExecution, fldPath.Child("requiredDuringSchedulingRequiredDuringExecution"))...)
	// }
	if na.RequiredDuringSchedulingIgnoredDuringExecution != nil {
		allErrs = append(allErrs, corevalidation.ValidateNodeSelector(na.RequiredDuringSchedulingIgnoredDuringExecution, false, fldPath.Child("requiredDuringSchedulingIgnoredDuringExecution"))...)
	}
	if len(na.PreferredDuringSchedulingIgnoredDuringExecution) > 0 {
		allErrs = append(allErrs, corevalidation.ValidatePreferredSchedulingTerms(na.PreferredDuringSchedulingIgnoredDuringExecution, fldPath.Child("preferredDuringSchedulingIgnoredDuringExecution"))...)
	}
	return allErrs
}

// validatePodAffinity tests that the specified podAffinity fields have valid data.
func validatePodAffinity(podAffinity *core.PodAffinity, allowInvalidLabelValueInSelector bool, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	// TODO:Uncomment below code once RequiredDuringSchedulingRequiredDuringExecution is implemented.
	// if podAffinity.RequiredDuringSchedulingRequiredDuringExecution != nil {
	//	allErrs = append(allErrs, validatePodAffinityTerms(podAffinity.RequiredDuringSchedulingRequiredDuringExecution, false,
	//		fldPath.Child("requiredDuringSchedulingRequiredDuringExecution"))...)
	// }
	if podAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
		allErrs = append(allErrs, validatePodAffinityTerms(podAffinity.RequiredDuringSchedulingIgnoredDuringExecution, allowInvalidLabelValueInSelector,
			fldPath.Child("requiredDuringSchedulingIgnoredDuringExecution"))...)
	}
	if podAffinity.PreferredDuringSchedulingIgnoredDuringExecution != nil {
		allErrs = append(allErrs, validateWeightedPodAffinityTerms(podAffinity.PreferredDuringSchedulingIgnoredDuringExecution, allowInvalidLabelValueInSelector,
			fldPath.Child("preferredDuringSchedulingIgnoredDuringExecution"))...)
	}
	return allErrs
}

// validatePodAntiAffinity tests that the specified podAntiAffinity fields have valid data.
func validatePodAntiAffinity(podAntiAffinity *core.PodAntiAffinity, allowInvalidLabelValueInSelector bool, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	// TODO:Uncomment below code once RequiredDuringSchedulingRequiredDuringExecution is implemented.
	// if podAntiAffinity.RequiredDuringSchedulingRequiredDuringExecution != nil {
	//	allErrs = append(allErrs, validatePodAffinityTerms(podAntiAffinity.RequiredDuringSchedulingRequiredDuringExecution, false,
	//		fldPath.Child("requiredDuringSchedulingRequiredDuringExecution"))...)
	// }
	if podAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
		allErrs = append(allErrs, validatePodAffinityTerms(podAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution, allowInvalidLabelValueInSelector,
			fldPath.Child("requiredDuringSchedulingIgnoredDuringExecution"))...)
	}
	if podAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution != nil {
		allErrs = append(allErrs, validateWeightedPodAffinityTerms(podAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution, allowInvalidLabelValueInSelector,
			fldPath.Child("preferredDuringSchedulingIgnoredDuringExecution"))...)
	}
	return allErrs
}

// validatePodAffinityTerms tests that the specified podAffinityTerms fields have valid data.
func validatePodAffinityTerms(podAffinityTerms []core.PodAffinityTerm, allowInvalidLabelValueInSelector bool, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	for i, podAffinityTerm := range podAffinityTerms {
		allErrs = append(allErrs, validatePodAffinityTerm(podAffinityTerm, allowInvalidLabelValueInSelector, fldPath.Index(i))...)
	}
	return allErrs
}

// validatePodAffinityTerm tests that the specified podAffinityTerm fields have valid data.
func validatePodAffinityTerm(podAffinityTerm core.PodAffinityTerm, allowInvalidLabelValueInSelector bool, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	allErrs = append(allErrs, corevalidation.ValidatePodAffinityTermSelector(podAffinityTerm, allowInvalidLabelValueInSelector, fldPath)...)
	for _, name := range podAffinityTerm.Namespaces {
		for _, msg := range corevalidation.ValidateNamespaceName(name, false) {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("namespace"), name, msg))
		}
	}
	allErrs = append(allErrs, validateMatchLabelKeysAndMismatchLabelKeys(fldPath, podAffinityTerm.MatchLabelKeys, podAffinityTerm.MismatchLabelKeys, podAffinityTerm.LabelSelector)...)
	if len(podAffinityTerm.TopologyKey) == 0 {
		allErrs = append(allErrs, field.Required(fldPath.Child("topologyKey"), "can not be empty"))
	}
	return append(allErrs, metav1validation.ValidateLabelName(podAffinityTerm.TopologyKey, fldPath.Child("topologyKey"))...)
}

// validateWeightedPodAffinityTerms tests that the specified weightedPodAffinityTerms fields have valid data.
func validateWeightedPodAffinityTerms(weightedPodAffinityTerms []core.WeightedPodAffinityTerm, allowInvalidLabelValueInSelector bool, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	for j, weightedTerm := range weightedPodAffinityTerms {
		if weightedTerm.Weight <= 0 || weightedTerm.Weight > 100 {
			allErrs = append(allErrs, field.Invalid(fldPath.Index(j).Child("weight"), weightedTerm.Weight, "must be in the range 1-100"))
		}
		allErrs = append(allErrs, validatePodAffinityTerm(weightedTerm.PodAffinityTerm, allowInvalidLabelValueInSelector, fldPath.Index(j).Child("podAffinityTerm"))...)
	}
	return allErrs
}

// validateMatchLabelKeysAndMismatchLabelKeys checks if both matchLabelKeys and mismatchLabelKeys are valid.
// - validate that all matchLabelKeys and mismatchLabelKeys are valid label names.
// - validate that the user doesn't specify the same key in both matchLabelKeys and labelSelector.
// - validate that any matchLabelKeys are not duplicated with mismatchLabelKeys.
func validateMatchLabelKeysAndMismatchLabelKeys(fldPath *field.Path, matchLabelKeys, mismatchLabelKeys []string, labelSelector *metav1.LabelSelector) field.ErrorList {
	var allErrs field.ErrorList
	// 1. validate that all matchLabelKeys and mismatchLabelKeys are valid label names.
	allErrs = append(allErrs, validateLabelKeys(fldPath.Child("matchLabelKeys"), matchLabelKeys, labelSelector)...)
	allErrs = append(allErrs, validateLabelKeys(fldPath.Child("mismatchLabelKeys"), mismatchLabelKeys, labelSelector)...)

	// 2. validate that the user doesn't specify the same key in both matchLabelKeys and labelSelector.
	// It doesn't make sense to have the labelselector with the key specified in matchLabelKeys
	// because the matchLabelKeys will be `In` labelSelector which matches with only one value in the key,
	// and we cannot make any further filtering with that key.
	// On the other hand, we may want to have labelSelector with the key specified in mismatchLabelKeys.
	// because the mismatchLabelKeys will be `NotIn` labelSelector,
	// and we may want to filter Pods further with other labelSelector with that key.

	// labelKeysMap is keyed by label key and valued by the index of label key in labelKeys.
	if labelSelector != nil {
		labelKeysMap := map[string]int{}
		for i, key := range matchLabelKeys {
			labelKeysMap[key] = i
		}
		labelSelectorKeys := sets.New[string]()
		for key := range labelSelector.MatchLabels {
			labelSelectorKeys.Insert(key)
		}
		for _, matchExpression := range labelSelector.MatchExpressions {
			key := matchExpression.Key
			if i, ok := labelKeysMap[key]; ok && labelSelectorKeys.Has(key) {
				// Before validateLabelKeysWithSelector is called, the labelSelector has already got the selector created from matchLabelKeys.
				// Here, we found the duplicate key in labelSelector and the key is specified in labelKeys.
				// Meaning that the same key is specified in both labelSelector and matchLabelKeys/mismatchLabelKeys.
				allErrs = append(allErrs, field.Invalid(fldPath.Index(i), key, "exists in both matchLabelKeys and labelSelector"))
			}

			labelSelectorKeys.Insert(key)
		}
	}

	// 3. validate that any matchLabelKeys are not duplicated with mismatchLabelKeys.
	mismatchLabelKeysSet := sets.New(mismatchLabelKeys...)
	for i, k := range matchLabelKeys {
		if mismatchLabelKeysSet.Has(k) {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("matchLabelKeys").Index(i), k, "exists in both matchLabelKeys and mismatchLabelKeys"))
		}
	}

	return allErrs
}

// validateLabelKeys tests that the label keys are a valid label name.
// It's intended to be used for matchLabelKeys or mismatchLabelKeys.
func validateLabelKeys(fldPath *field.Path, labelKeys []string, labelSelector *metav1.LabelSelector) field.ErrorList {
	if len(labelKeys) == 0 {
		return nil
	}

	if labelSelector == nil {
		return field.ErrorList{field.Forbidden(fldPath, "must not be specified when labelSelector is not set")}
	}

	var allErrs field.ErrorList
	for i, key := range labelKeys {
		allErrs = append(allErrs, metav1validation.ValidateLabelName(key, fldPath.Index(i))...)
	}

	return allErrs
}
