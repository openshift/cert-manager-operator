package features

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"k8s.io/component-base/featuregate"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		featuregate.Feature("TrustManager"),
	},

	// features DISABLED by default,
	// list of features which are expected to be disabled at runtime.
	false: {
		featuregate.Feature("HTTP01Proxy"),
		featuregate.Feature("ApproverPolicyManager"),
	},
}

func TestFeatureGates(t *testing.T) {
	t.Run("runtime features to test should be identical with operator features", func(t *testing.T) {
		testFeatureNames := make([]featuregate.Feature, 0)
		for _, featureNames := range expectedDefaultFeatureState {
			testFeatureNames = append(testFeatureNames, featureNames...)
		}
		slices.Sort(testFeatureNames)

		knownOperatorFeatures := make([]featuregate.Feature, 0)
		feats := mutableFeatureGate.GetAll()
		for feat := range feats {
			// skip "AllBeta", "AllAlpha": our operator does not use those
			if feat == "AllBeta" || feat == "AllAlpha" {
				continue
			}
			knownOperatorFeatures = append(knownOperatorFeatures, feat)
		}
		slices.Sort(knownOperatorFeatures)

		assert.ElementsMatch(t, knownOperatorFeatures, testFeatureNames,
			`the list of features known to the operator differ from what is being tested here,
			it could be that there was a new Feature added to the api which wasn't added to the tests.
			Please verify "api/operator/v1alpha1" and "pkg/features" have identical features.`)
	})

	t.Run("all runtime features should honor expected default feature state", func(t *testing.T) {
		for expectedDefaultState, features := range expectedDefaultFeatureState {
			for _, featureName := range features {
				assert.Equal(t, expectedDefaultState, DefaultFeatureGate.Enabled(featureName),
					"violated by feature=%s", featureName)
			}
		}
	})

	t.Run("all pre-GA features should be disabled by default", func(t *testing.T) {
		feats := mutableFeatureGate.GetAll()
		for feat, spec := range feats {
			// skip "AllBeta", "AllAlpha": our operator does not use those
			if feat == "AllBeta" || feat == "AllAlpha" {
				continue
			}

			isPreGA := spec.PreRelease == "TechPreview" || spec.PreRelease == featuregate.Alpha || spec.PreRelease == featuregate.Beta
			assert.Equal(t, isPreGA, !spec.Default,
				"pre-GA %q feature (prerelease=%s) should default to disabled",
				feat, spec.PreRelease)
		}
	})

	t.Run("pre-GA features should use the TechPreview label", func(t *testing.T) {
		// Red Hat ships pre-GA operator features as Technology Preview. Mixing
		// upstream Kubernetes labels (Alpha/Beta) with "TechPreview" makes the
		// support level unclear to users, so every pre-GA gate uses one label.
		for feat, spec := range v1alpha1.OperatorFeatureGates {
			if spec.PreRelease == featuregate.GA || spec.PreRelease == featuregate.Deprecated {
				continue
			}
			assert.Equal(t, "TechPreview", string(spec.PreRelease),
				"feature %q should be labeled TechPreview, got %q", feat, spec.PreRelease)
		}
	})

	t.Run("runtime should allow enabling all operator featuregates that default to off", func(t *testing.T) {
		// enable all disabled features
		defaultDisabledFeatures := expectedDefaultFeatureState[false]
		t.Cleanup(func() {
			for _, feature := range defaultDisabledFeatures {
				require.NoError(t, mutableFeatureGate.Set(fmt.Sprintf("%s=false", feature)))
			}
		})

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

// TestIsTrustManagerFeatureGateEnabled covers the TrustManager operator featuregate
// (--unsupported-addon-features).
func TestIsTrustManagerFeatureGateEnabled(t *testing.T) {
	defer func() {
		// TrustManager defaults to true; restore after test
		require.NoError(t, SetupWithFlagValue("TrustManager=true"))
	}()

	tests := []struct {
		name   string
		prep   func(t *testing.T)
		assert func(t *testing.T)
	}{
		{
			name: "returns true when operator featuregate is on",
			prep: func(t *testing.T) {
				t.Helper()
				require.NoError(t, SetupWithFlagValue("TrustManager=true"))
			},
			assert: func(t *testing.T) {
				t.Helper()
				assert.True(t, IsTrustManagerFeatureGateEnabled())
			},
		},
		{
			name: "returns false when operator featuregate is off",
			prep: func(t *testing.T) {
				t.Helper()
				require.NoError(t, SetupWithFlagValue("TrustManager=false"))
			},
			assert: func(t *testing.T) {
				t.Helper()
				assert.False(t, IsTrustManagerFeatureGateEnabled())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.prep(t)
			tt.assert(t)
		})
	}
}

// TestIsIstioCSRFeatureGateEnabled covers the IstioCSR operator featuregate
// (--unsupported-addon-features). IstioCSR is GA and enabled by default,
// so it does not depend on cluster FeatureSet.
func TestIsIstioCSRFeatureGateEnabled(t *testing.T) {
	defer func() {
		// IstioCSR defaults to true; restore after test
		require.NoError(t, SetupWithFlagValue("IstioCSR=true"))
	}()

	tests := []struct {
		name   string
		prep   func(t *testing.T)
		assert func(t *testing.T)
	}{
		{
			name: "returns true when operator featuregate is on",
			prep: func(t *testing.T) {
				t.Helper()
				require.NoError(t, SetupWithFlagValue("IstioCSR=true"))
			},
			assert: func(t *testing.T) {
				t.Helper()
				assert.True(t, IsIstioCSRFeatureGateEnabled())
			},
		},
		{
			name: "returns false when operator featuregate is off",
			prep: func(t *testing.T) {
				t.Helper()
				require.NoError(t, SetupWithFlagValue("IstioCSR=false"))
			},
			assert: func(t *testing.T) {
				t.Helper()
				assert.False(t, IsIstioCSRFeatureGateEnabled())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.prep(t)
			tt.assert(t)
		})
	}
}

func TestSetupWithFlagValue(t *testing.T) {
	tests := []struct {
		name      string
		flagValue string
		wantErr   bool
	}{
		{
			name:      "unknown feature gate is rejected",
			flagValue: "InvalidFeature=true",
			wantErr:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := SetupWithFlagValue(tt.flagValue)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestIsApproverPolicyManagerFeatureGateEnabled(t *testing.T) {
	originalMutable, originalDefault := mutableFeatureGate, DefaultFeatureGate
	mutableFeatureGate = featuregate.NewFeatureGate()
	require.NoError(t, mutableFeatureGate.Add(v1alpha1.OperatorFeatureGates))
	DefaultFeatureGate = mutableFeatureGate
	t.Cleanup(func() {
		mutableFeatureGate, DefaultFeatureGate = originalMutable, originalDefault
	})
	require.False(t, IsApproverPolicyManagerFeatureGateEnabled())
	for _, enabled := range []bool{true, false} {
		require.NoError(t, SetupWithFlagValue(fmt.Sprintf("ApproverPolicyManager=%t", enabled)))
		assert.Equal(t, enabled, IsApproverPolicyManagerFeatureGateEnabled())
	}
}
