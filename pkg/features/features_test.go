package features

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgotesting "k8s.io/client-go/testing"
	"k8s.io/component-base/featuregate"

	ocpfeaturegate "github.com/openshift/api/config/v1"
	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	configfake "github.com/openshift/client-go/config/clientset/versioned/fake"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newFakeConfigClient returns a fake OpenShift config clientset (openshift/client-go …/versioned/fake).
// When withFeatureGateAPI is true, cs.Resources is set so FakeDiscovery serves featuregates for
// config.openshift.io/v1. objs are seeded into the tracker (e.g. featuregates/cluster).
func newFakeConfigClient(t *testing.T, withFeatureGateAPI bool, objs ...runtime.Object) *configfake.Clientset {
	t.Helper()
	cs := configfake.NewClientset(objs...)
	if withFeatureGateAPI {
		cs.Resources = []*metav1.APIResourceList{
			{
				GroupVersion: ocpfeaturegate.GroupVersion.String(),
				APIResources: []metav1.APIResource{{Name: openshiftFeatureGateResource, Namespaced: false, Kind: "FeatureGate"}},
			},
		}
	}
	return cs
}

func clusterFeatureGateObject(featureSet ocpfeaturegate.FeatureSet) *ocpfeaturegate.FeatureGate {
	return &ocpfeaturegate.FeatureGate{
		TypeMeta: metav1.TypeMeta{APIVersion: ocpfeaturegate.GroupVersion.String(), Kind: "FeatureGate"},
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
		Spec: ocpfeaturegate.FeatureGateSpec{
			FeatureGateSelection: ocpfeaturegate.FeatureGateSelection{FeatureSet: featureSet},
		},
	}
}

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
	false: {
		featuregate.Feature("TrustManager"),
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

		assert.Equal(t, knownOperatorFeatures, testFeatureNames,
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

	t.Run("runtime should allow enabling all operator featuregates that default to off", func(t *testing.T) {
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

func TestAllowedPreviewClusterFeatureSets(t *testing.T) {
	tests := []struct {
		name    string
		fs      ocpfeaturegate.FeatureSet
		allowed bool
	}{
		{name: "CustomNoUpgrade is allowed", fs: ocpfeaturegate.CustomNoUpgrade, allowed: true},
		{name: "DevPreviewNoUpgrade is allowed", fs: ocpfeaturegate.DevPreviewNoUpgrade, allowed: true},
		{name: "TechPreviewNoUpgrade is allowed", fs: ocpfeaturegate.TechPreviewNoUpgrade, allowed: true},
		{name: "OKD is allowed", fs: FeatureSetOKD, allowed: true},
		{name: "Default cluster featureset is not allowed", fs: ocpfeaturegate.Default, allowed: false},
		{name: "unknown featureset is not allowed", fs: "Unknown", allowed: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := allowedPreviewFeatureSets.Has(tt.fs)
			assert.Equal(t, tt.allowed, got, "featureSet=%q", tt.fs)
		})
	}
}

// TestIsTrustManagerFeatureGateEnabled covers cluster featureset plus TrustManager operator
// featuregate (--unsupported-addon-features).
func TestIsTrustManagerFeatureGateEnabled(t *testing.T) {
	defer func() {
		_ = SetupWithFlagValue("TrustManager=false")
	}()

	tests := []struct {
		name   string
		prep   func(t *testing.T) *configfake.Clientset
		assert func(t *testing.T, st *FeatureGateState)
	}{
		{
			name: "returns false when cluster feature gate discovery fails",
			prep: func(t *testing.T) *configfake.Clientset {
				t.Helper()
				cs := newFakeConfigClient(t, true, clusterFeatureGateObject(ocpfeaturegate.TechPreviewNoUpgrade))
				cs.AddReactor("get", "resource", func(clientgotesting.Action) (bool, runtime.Object, error) {
					return true, nil, fmt.Errorf("simulated discovery failure")
				})
				return cs
			},
			assert: func(t *testing.T, st *FeatureGateState) {
				t.Helper()
				assert.False(t, st.IsTrustManagerFeatureGateEnabled())
				require.Error(t, st.Err())
				assert.True(t, errors.Is(st.Err(), ErrFeatureGateDiscovery))
			},
		},
		{
			name: "returns false when featuregates/cluster cannot be read",
			prep: func(t *testing.T) *configfake.Clientset {
				t.Helper()
				require.NoError(t, SetupWithFlagValue("TrustManager=true"))
				cs := newFakeConfigClient(t, true)
				cs.AddReactor("get", "featuregates", func(action clientgotesting.Action) (bool, runtime.Object, error) {
					if a, ok := action.(clientgotesting.GetAction); ok && a.GetName() == "cluster" && a.GetNamespace() == "" {
						return true, nil, fmt.Errorf("simulated featuregates/cluster get failure")
					}
					return false, nil, nil
				})
				return cs
			},
			assert: func(t *testing.T, st *FeatureGateState) {
				t.Helper()
				assert.False(t, st.IsTrustManagerFeatureGateEnabled())
				assert.True(t, errors.Is(st.Err(), ErrFeatureGateClusterGet))
			},
		},
		{
			name: "returns true when cluster preview featureset and operator featuregate on",
			prep: func(t *testing.T) *configfake.Clientset {
				t.Helper()
				require.NoError(t, SetupWithFlagValue("TrustManager=true"))
				return newFakeConfigClient(t, true, clusterFeatureGateObject(ocpfeaturegate.TechPreviewNoUpgrade))
			},
			assert: func(t *testing.T, st *FeatureGateState) {
				t.Helper()
				assert.True(t, st.IsTrustManagerFeatureGateEnabled())
				assert.NoError(t, st.Err())
			},
		},
		{
			name: "returns false when cluster featureset is Default even with operator featuregate set",
			prep: func(t *testing.T) *configfake.Clientset {
				t.Helper()
				require.NoError(t, SetupWithFlagValue("TrustManager=true"))
				return newFakeConfigClient(t, true, clusterFeatureGateObject(ocpfeaturegate.Default))
			},
			assert: func(t *testing.T, st *FeatureGateState) {
				t.Helper()
				assert.False(t, st.IsTrustManagerFeatureGateEnabled())
			},
		},
		{
			name: "returns false when operator featuregate off even with allowed cluster featureset",
			prep: func(t *testing.T) *configfake.Clientset {
				t.Helper()
				require.NoError(t, SetupWithFlagValue("TrustManager=false"))
				return newFakeConfigClient(t, true, clusterFeatureGateObject(ocpfeaturegate.TechPreviewNoUpgrade))
			},
			assert: func(t *testing.T, st *FeatureGateState) {
				t.Helper()
				assert.False(t, st.IsTrustManagerFeatureGateEnabled())
			},
		},
		{
			name: "returns true when FeatureGate API not served and operator featuregate is on",
			prep: func(t *testing.T) *configfake.Clientset {
				t.Helper()
				origMutable := mutableFeatureGate
				origDefault := DefaultFeatureGate
				freshMutable := featuregate.NewFeatureGate()
				utilruntime.Must(freshMutable.Add(v1alpha1.OperatorFeatureGates))
				mutableFeatureGate = freshMutable
				DefaultFeatureGate = freshMutable
				t.Cleanup(func() {
					mutableFeatureGate = origMutable
					DefaultFeatureGate = origDefault
				})
				require.NoError(t, freshMutable.Set("TrustManager=true"))
				return newFakeConfigClient(t, false)
			},
			assert: func(t *testing.T, st *FeatureGateState) {
				t.Helper()
				assert.False(t, st.apiPresent)
				assert.NoError(t, st.Err())
				assert.True(t, st.IsTrustManagerFeatureGateEnabled())
			},
		},
		{
			name: "returns false when FeatureGate API not served and operator featuregate is off",
			prep: func(t *testing.T) *configfake.Clientset {
				t.Helper()
				require.NoError(t, SetupWithFlagValue("TrustManager=false"))
				return newFakeConfigClient(t, false)
			},
			assert: func(t *testing.T, st *FeatureGateState) {
				t.Helper()
				assert.False(t, st.apiPresent)
				assert.NoError(t, st.Err())
				assert.False(t, st.IsTrustManagerFeatureGateEnabled())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs := tt.prep(t)
			st := NewFeatureGateState(t.Context(), cs)
			tt.assert(t, st)
		})
	}
}
