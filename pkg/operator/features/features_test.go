package features

import (
	"sync"
	"testing"
	"time"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/stretchr/testify/assert"
	"k8s.io/component-base/featuregate"
)

// expectedDefaultFeatureState is a map of features with known states
//
// Always update this test map when adding a new FeatureName to the api,
// a test is added below to enforce this.
var expectedDefaultFeatureState = map[bool][]v1alpha1.FeatureName{
	// features ENABLED by default,
	// list of features which are expected to be enabled at runtime.
	true: {},

	// features DISABLED by default,
	// list of features which are expected to be disabled at runtime.
	false: {
		v1alpha1.FeatureName("IstioCSR"),
	},
}

func TestFeatureGates(t *testing.T) {
	t.Run("runtime features to test should be identical with operator features", func(t *testing.T) {
		testFeatureNames := make([]v1alpha1.FeatureName, 0)
		for _, featureNames := range expectedDefaultFeatureState {
			testFeatureNames = append(testFeatureNames, featureNames...)
		}

		knownOperatorFeatures := make([]v1alpha1.FeatureName, 0)
		feats := RuntimeFeatures.mutableFeatures.GetAll()
		for feat := range feats {
			// skip "AllBeta", "AllAlpha": our operator does not use those
			if feat == "AllBeta" || feat == "AllAlpha" {
				continue
			}
			knownOperatorFeatures = append(knownOperatorFeatures, v1alpha1.FeatureName(feat))
		}

		assert.Equal(t, knownOperatorFeatures, testFeatureNames,
			`the list of features known to the operator differ from what is being tested here,
			it could be that there was a new Feature added to the api which wasn't added to the tests.
			Please verify "api/operator/v1alpha1" and "pkg/operator/features" have identical features.`)
	})

	t.Run("runtime features should honor expected default feature state", func(t *testing.T) {
		for expectedDefaultState, features := range expectedDefaultFeatureState {
			for _, featureName := range features {
				assert.Equal(t, expectedDefaultState, RuntimeFeatures.IsFeatureEnabled(featureName))
			}
		}
	})

	t.Run("runtime features should allow enabling all feature gates that are disabled by default", func(t *testing.T) {
		// enable all disabled features
		defaultDisabledFeatures := expectedDefaultFeatureState[false]
		err := RuntimeFeatures.EnableMultipleFeatures(defaultDisabledFeatures)
		assert.NoError(t, err)

		// check if all those features were enabled successfully
		for _, featureName := range defaultDisabledFeatures {
			assert.Equal(t, true, RuntimeFeatures.IsFeatureEnabled(featureName))
		}
	})
}

func TestFeatureAccessorWatch(t *testing.T) {
	err := RuntimeFeatures.mutableFeatures.Add(map[featuregate.Feature]featuregate.FeatureSpec{
		featuregate.Feature("FeatureX"): {Default: false, PreRelease: "TechPreview"},
		featuregate.Feature("FeatureY"): {Default: false, PreRelease: "TechPreview"},
		featuregate.Feature("FeatureZ"): {Default: false, PreRelease: "TechPreview"},

		featuregate.Feature("FeatureFoo"): {Default: true, PreRelease: "GA"},
		featuregate.Feature("FeatureBar"): {Default: true, PreRelease: "GA"},

		featuregate.Feature("FeatureA"): {Default: false, PreRelease: "TechPreview"},
		featuregate.Feature("FeatureB"): {Default: false, PreRelease: "TechPreview"},
	})
	assert.NoError(t, err)

	watch1, watch2 := NewTickWatcher(), NewTickWatcher()
	RuntimeFeatures.AddEventWatcher(watch1)
	RuntimeFeatures.AddEventWatcher(watch2)

	watch1Count, watch2Count := 0, 0
	var wg sync.WaitGroup
	wg.Add(2)

	watchObserver := func(watch <-chan struct{}, eventCount *int) {
		defer wg.Done()
		for {
			select {
			case _, ok := <-watch:
				if !ok {
					return
				}
				*eventCount = *eventCount + 1
			case <-time.After(100 * time.Millisecond):
				return
			}
		}
	}

	go watchObserver(watch1.GetTicker(), &watch1Count)
	go watchObserver(watch2.GetTicker(), &watch2Count)

	err = RuntimeFeatures.EnableFeature(v1alpha1.FeatureName("FeatureX")) // event (1)
	assert.NoError(t, err)
	err = RuntimeFeatures.EnableFeature(v1alpha1.FeatureName("FeatureX")) // no event
	assert.NoError(t, err)

	err = RuntimeFeatures.EnableFeature(v1alpha1.FeatureName("FeatureY")) // event (2)
	assert.NoError(t, err)

	watch2.Close()

	err = RuntimeFeatures.EnableFeature(v1alpha1.FeatureName("FeatureZ")) // event (3)
	assert.NoError(t, err)
	err = RuntimeFeatures.EnableFeature(v1alpha1.FeatureName("FeatureFoo")) // no event
	assert.NoError(t, err)
	err = RuntimeFeatures.EnableFeature(v1alpha1.FeatureName("FeatureBar")) // no event
	assert.NoError(t, err)

	err = RuntimeFeatures.EnableMultipleFeatures([]v1alpha1.FeatureName{"FeatureA", "FeatureB"}) // event (4)
	assert.NoError(t, err)
	err = RuntimeFeatures.EnableMultipleFeatures([]v1alpha1.FeatureName{"FeatureX", "FeatureY"}) // no event
	assert.NoError(t, err)

	watch1.Close()

	wg.Wait()
	assert.Equal(t, 4, watch1Count, "watch1 should receive 4 events (FeatureX, FeatureY, FeatureZ, 'FeatureA & FeatureB')")
	assert.Equal(t, 2, watch2Count, "watch2 should receive 2 events (FeatureX and FeatureY) before being closed")
}
