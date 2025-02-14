package features

import (
	"sync"

	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/component-base/featuregate"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

// Features implements the FeatureAccessor interface using a MutableFeatureGate
type Features struct {
	// mu locks the eventWatchers from concurrent writes
	mu sync.Mutex
	// mutableFeatures is a mutable version of k8s DefaultFeatureGate.
	mutableFeatures featuregate.MutableFeatureGate
	// eventWatchers are subscriber channels which receive an event when
	// feature(s) get enabled
	eventWatchers []TickWatch
}

var (
	// RuntimeFeatures tracks list of known features at runtime
	RuntimeFeatures *Features

	_ FeatureAccessor = RuntimeFeatures
)

func init() {
	RuntimeFeatures = &Features{
		mutableFeatures: featuregate.NewFeatureGate(),
		eventWatchers:   make([]TickWatch, 0),
	}

	// make the DefaultMutableFeatureGate aware of the operator features
	utilruntime.Must(RuntimeFeatures.mutableFeatures.Add(v1alpha1.OperatorFeatureGates))
}

func (f *Features) EnableMultipleFeatures(features []v1alpha1.FeatureName) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	featMap := make(map[string]bool)
	for _, feat := range features {
		if !f.IsFeatureEnabled(feat) {
			featMap[string(feat)] = true
		}
	}

	// no new features to enable
	if len(featMap) == 0 {
		return nil
	}

	err := RuntimeFeatures.mutableFeatures.SetFromMap(featMap)
	if err != nil {
		return err
	}

	// send an event to all the watchers, when any feature state was changed.
	for _, eventWatcher := range f.eventWatchers {
		if !eventWatcher.IsClosed() {
			err = eventWatcher.SendTick()
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (f *Features) EnableFeature(feat v1alpha1.FeatureName) error {
	return f.EnableMultipleFeatures([]v1alpha1.FeatureName{feat})
}

func (f *Features) IsFeatureEnabled(feat v1alpha1.FeatureName) bool {
	return RuntimeFeatures.mutableFeatures.Enabled(featuregate.Feature(feat))
}

func (f *Features) AddEventWatcher(eventWatcher TickWatch) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.eventWatchers = append(f.eventWatchers, eventWatcher)
}
