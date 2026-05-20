package common

import (
	"unsafe"

	corev1 "k8s.io/api/core/v1"
	apivalidation "k8s.io/apimachinery/pkg/api/validation"
	metav1validation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/kubernetes/pkg/apis/core"
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
	// convert corev1.Tolerations to core.Tolerations, required for validation.
	convTolerations := *(*[]core.Toleration)(unsafe.Pointer(&tolerations))
	return corevalidation.ValidateTolerations(convTolerations, fldPath.Child("tolerations"), corevalidation.PodValidationOptions{}).ToAggregate()
}

// ValidateResourceRequirements validates the ResourceRequirements configuration
// using the Kubernetes core resource requirements validation rules.
func ValidateResourceRequirements(requirements corev1.ResourceRequirements, fldPath *field.Path) error {
	// convert corev1.ResourceRequirements to core.ResourceRequirements, required for validation.
	convRequirements := *(*core.ResourceRequirements)(unsafe.Pointer(&requirements))
	return corevalidation.ValidateContainerResourceRequirements(&convRequirements, nil, fldPath.Child("resources"), corevalidation.PodValidationOptions{}).ToAggregate()
}

// ValidateAffinityRules validates the Affinity configuration using
// the Kubernetes core affinity validation rules.
func ValidateAffinityRules(affinity *corev1.Affinity, fldPath *field.Path) error {
	// convert corev1.Affinity to core.Affinity, required for validation.
	convAffinity := (*core.Affinity)(unsafe.Pointer(affinity))
	return validateAffinity(convAffinity, corevalidation.PodValidationOptions{}, fldPath.Child("affinity")).ToAggregate()
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
