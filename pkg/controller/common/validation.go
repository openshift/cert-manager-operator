package common

import (
	corev1 "k8s.io/api/core/v1"
	apivalidation "k8s.io/apimachinery/pkg/api/validation"
	metav1validation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/kubernetes/pkg/apis/core"
	corev1conversion "k8s.io/kubernetes/pkg/apis/core/v1"
	corevalidation "k8s.io/kubernetes/pkg/apis/core/validation"
)

// ValidateNodeSelectorConfig validates the NodeSelector configuration using
// the Kubernetes label validation rules.
func ValidateNodeSelectorConfig(nodeSelector map[string]string, fldPath *field.Path) error {
	return metav1validation.ValidateLabels(nodeSelector, fldPath.Child("nodeSelector")).ToAggregate()
}

// ValidateTolerationsConfig validates the Tolerations configuration using
// the Kubernetes core toleration validation rules.
func ValidateTolerationsConfig(tolerations []corev1.Toleration, fldPath *field.Path) error {
	return corevalidation.ValidateTolerations(ToCoreTolerations(tolerations), fldPath.Child("tolerations"), corevalidation.PodValidationOptions{}).ToAggregate()
}

// ValidateResourceRequirements validates the ResourceRequirements configuration
// using the Kubernetes core resource requirements validation rules.
func ValidateResourceRequirements(requirements corev1.ResourceRequirements, fldPath *field.Path) error {
	var convRequirements core.ResourceRequirements
	_ = corev1conversion.Convert_v1_ResourceRequirements_To_core_ResourceRequirements(&requirements, &convRequirements, nil)
	return corevalidation.ValidateContainerResourceRequirements(&convRequirements, nil, fldPath.Child("resources"), corevalidation.PodValidationOptions{}).ToAggregate()
}

// ValidateAffinityRules validates the Affinity configuration using
// the Kubernetes core affinity validation rules.
func ValidateAffinityRules(affinity *corev1.Affinity, fldPath *field.Path) error {
	var convAffinity core.Affinity
	_ = corev1conversion.Convert_v1_Affinity_To_core_Affinity(affinity, &convAffinity, nil)
	return validateAffinity(&convAffinity, corevalidation.PodValidationOptions{}, fldPath.Child("affinity")).ToAggregate()
}

// ValidateLabelsConfig validates label keys and values using the Kubernetes
// metadata validation rules.
func ValidateLabelsConfig(labels map[string]string, fldPath *field.Path) error {
	return metav1validation.ValidateLabels(labels, fldPath.Child("labels")).ToAggregate()
}

// ValidateAnnotationsConfig validates annotation keys and sizes using the
// Kubernetes metadata validation rules.
func ValidateAnnotationsConfig(annotations map[string]string, fldPath *field.Path) error {
	return apivalidation.ValidateAnnotations(annotations, fldPath.Child("annotations")).ToAggregate()
}

// ToCoreTolerations converts a slice of corev1.Toleration to core.Toleration
// using Kubernetes' auto-generated conversion functions.
func ToCoreTolerations(in []corev1.Toleration) []core.Toleration {
	out := make([]core.Toleration, len(in))
	for i := range in {
		_ = corev1conversion.Convert_v1_Toleration_To_core_Toleration(&in[i], &out[i], nil)
	}
	return out
}

// ToV1Tolerations converts a slice of core.Toleration to corev1.Toleration
// using Kubernetes' auto-generated conversion functions.
func ToV1Tolerations(in []core.Toleration) []corev1.Toleration {
	out := make([]corev1.Toleration, len(in))
	for i := range in {
		_ = corev1conversion.Convert_core_Toleration_To_v1_Toleration(&in[i], &out[i], nil)
	}
	return out
}
