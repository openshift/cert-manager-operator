package certmanager

import "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"

type FeatureAccessor interface {
	// EnableFeature enables a newly known feature, calls are idempotent.
	EnableFeature(feature v1alpha1.FeatureName)
	// EnableFeature enables many features at once.
	EnableMultipleFeatures(features []v1alpha1.FeatureName)
	// IsFeatureEnabled returns true if the feature was enabled before.
	IsFeatureEnabled(feature v1alpha1.FeatureName) bool
}
