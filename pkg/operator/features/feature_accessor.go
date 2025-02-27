package features

import "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"

type TickWatch interface {
	SendTick() error
	GetTicker() <-chan struct{}

	Close()
	IsClosed() bool
}

type FeatureAccessor interface {
	// EnableFeature enables a newly known feature, calls are idempotent.
	EnableFeature(feature v1alpha1.FeatureName) error
	// EnableFeature enables many features at once.
	EnableMultipleFeatures(features []v1alpha1.FeatureName) error
	// IsFeatureEnabled returns true if the feature was enabled before.
	IsFeatureEnabled(feature v1alpha1.FeatureName) bool
	// AddWatcher allows adding a channel which will be sent an event when
	// new feature(s) get enabled.
	AddEventWatcher(eventWatcher TickWatch)
}
