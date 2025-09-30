package features

import (
	"fmt"
	"strings"
	"testing"

	"k8s.io/component-base/featuregate"

	"github.com/stretchr/testify/assert"
)

// expectedDefaultFeatureState is a map of features with known states
//
// Always update this test map when adding a new FeatureName to the api,
// a test is added below to enforce this.
var expectedDefaultFeatureState = map[bool][]featuregate.Feature{
	// features ENABLED by default,
	// list of features which are expected to be enabled at runtime.
	true: {
		featuregate.Feature("IstioCSR"),
	},

	// features DISABLED by default,
	// list of features which are expected to be disabled at runtime.
	false: {},
}

func TestFeatureGates(t *testing.T) {
	t.Run("runtime features to test should be identical with operator features", func(t *testing.T) {
		testFeatureNames := make([]featuregate.Feature, 0)
		for _, featureNames := range expectedDefaultFeatureState {
			testFeatureNames = append(testFeatureNames, featureNames...)
		}

		knownOperatorFeatures := make([]featuregate.Feature, 0)
		feats := mutableFeatureGate.GetAll()
		for feat := range feats {
			// skip "AllBeta", "AllAlpha": our operator does not use those
			if feat == "AllBeta" || feat == "AllAlpha" {
				continue
			}
			knownOperatorFeatures = append(knownOperatorFeatures, feat)
		}

		assert.Equal(t, knownOperatorFeatures, testFeatureNames,
			`the list of features known to the operator differ from what is being tested here,
			it could be that there was a new Feature added to the api which wasn't added to the tests.
			Please verify "api/operator/v1alpha1" and "pkg/operator/features" have identical features.`)
	})

	t.Run("all runtime features should honor expected default feature state", func(t *testing.T) {
		for expectedDefaultState, features := range expectedDefaultFeatureState {
			for _, featureName := range features {
				assert.Equal(t, expectedDefaultState, DefaultFeatureGate.Enabled(featureName),
					"violated by feature=%s", featureName)
			}
		}
	})

	t.Run("all TechPreview features should be disabled by default", func(t *testing.T) {
		feats := mutableFeatureGate.GetAll()
		for feat, spec := range feats {
			// skip "AllBeta", "AllAlpha": our operator does not use those
			if feat == "AllBeta" || feat == "AllAlpha" {
				continue
			}

			assert.Equal(t, spec.PreRelease == "TechPreview", !spec.Default,
				"prerelease TechPreview %q feature should default to disabled",
				feat)
		}
	})

	t.Run("runtime features should allow enabling all feature gates that are disabled by default", func(t *testing.T) {
		// enable all disabled features
		defaultDisabledFeatures := expectedDefaultFeatureState[false]

		featVals := make([]string, len(defaultDisabledFeatures))
		for i := range featVals {
			featVals[i] = fmt.Sprintf("%s=true", defaultDisabledFeatures[i])
		}

		err := mutableFeatureGate.Set(strings.Join(featVals, ","))
		assert.NoError(t, err)

		// check if all those features were enabled successfully
		for _, featureName := range defaultDisabledFeatures {
			assert.Equal(t, true, DefaultFeatureGate.Enabled(featureName),
				"violated by feature=%s", featureName)
		}
	})
}
