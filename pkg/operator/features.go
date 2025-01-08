package operator

import (
	"sync"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/certmanager"
)

type Features struct {
	mu              *sync.RWMutex
	enabledFeatures map[v1alpha1.FeatureName]struct{}
}

var (
	// runtimeFeatures tracks list of known features at runtime
	runtimeFeatures *Features

	_ certmanager.FeatureAccessor = runtimeFeatures
)

func init() {
	runtimeFeatures = &Features{
		mu:              &sync.RWMutex{},
		enabledFeatures: make(map[v1alpha1.FeatureName]struct{}),
	}
}

func (f *Features) EnableFeature(feat v1alpha1.FeatureName) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if !f.isEnabled(feat) {
		f.enabledFeatures[feat] = struct{}{}
	}
}

func (f *Features) EnableMultipleFeatures(features []v1alpha1.FeatureName) {
	for _, feat := range features {
		runtimeFeatures.EnableFeature(feat)
	}
}

func (f *Features) IsFeatureEnabled(feat v1alpha1.FeatureName) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()

	return f.isEnabled(feat)
}

// isEnabled checks if feature exists, without lock
func (f *Features) isEnabled(feat v1alpha1.FeatureName) bool {
	_, exists := f.enabledFeatures[feat]
	return exists
}
