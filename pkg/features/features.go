package features

import (
	"context"
	"errors"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	k8sfeaturegate "k8s.io/component-base/featuregate"
	ctrl "sigs.k8s.io/controller-runtime"

	configv1 "github.com/openshift/api/config/v1"
	configv1client "github.com/openshift/client-go/config/clientset/versioned"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/utils"
)

const (
	// openshiftFeatureGateResource is the API resource name for cluster FeatureGate objects.
	openshiftFeatureGateResource = "featuregates"

	// FeatureSetOKD is the OKD cluster featureset value.
	// TODO: After openshift/api is updated to 4.22 (or newer) release content, remove this constant and use the upstream configv1 OKD FeatureSet instead.
	FeatureSetOKD configv1.FeatureSet = "OKD"
)

// allowedPreviewFeatureSets is the set of cluster featureset values that
// indicate preview or custom features: CustomNoUpgrade, DevPreviewNoUpgrade, TechPreviewNoUpgrade, OKD.
var allowedPreviewFeatureSets = sets.New[configv1.FeatureSet](
	configv1.CustomNoUpgrade,
	configv1.DevPreviewNoUpgrade,
	configv1.TechPreviewNoUpgrade,
	FeatureSetOKD,
)

var (
	// mutableFeatureGate holds operator featuregates from --unsupported-addon-features (read/write).
	mutableFeatureGate k8sfeaturegate.MutableFeatureGate = k8sfeaturegate.NewFeatureGate()

	// DefaultFeatureGate is the read-only view of operator featuregates.
	DefaultFeatureGate k8sfeaturegate.FeatureGate = mutableFeatureGate
)

var (
	// ErrFeatureGateDiscovery marks failures discovering whether the cluster serves the
	// config.openshift.io/v1 featuregates resource (excluding NotFound, which means not served).
	ErrFeatureGateDiscovery = errors.New("cluster feature gate API discovery failed")

	// ErrFeatureGateClusterGet marks failures reading featuregates.config.openshift.io/cluster
	// when that API is known to be served.
	ErrFeatureGateClusterGet = errors.New("featuregates.config.openshift.io/cluster read failed")
)

// log is the package logger for cluster featureset and operator featuregate.
var log = ctrl.Log.WithName("features")

// FeatureGateState represents the current availability and configuration
// state of the FeatureGate API.
type FeatureGateState struct {
	// apiPresent is true if discovery reports the featuregates resource is served
	// for config.openshift.io/v1.
	apiPresent bool

	// isPreviewEnabled is true if the cluster is using a preview FeatureSet
	// (e.g., TechPreviewNoUpgrade, CustomNoUpgrade, or OKD).
	isPreviewEnabled bool

	// stateErr, if non-nil, is why cluster preview gating failed closed: discovery failure, or
	// failure reading featuregates/cluster. It must not be treated like "API absent".
	// Use [errors.Is] with [ErrFeatureGateDiscovery], [ErrFeatureGateClusterGet].
	// "Featureset not in allow-list" does not set stateErr.
	stateErr error
}

// init registers operator featuregates (e.g. TrustManager, IstioCSR) with mutableFeatureGate.
func init() {
	utilruntime.Must(mutableFeatureGate.Add(v1alpha1.OperatorFeatureGates))
}

// SetupWithFlagValue parses flagValue as operator featuregates from --unsupported-addon-features
// and applies them to mutableFeatureGate. If flagValue is empty, defaults are left unchanged.
func SetupWithFlagValue(flagValue string) error {
	if flagValue == "" {
		return nil // use defined defaults
	}
	return mutableFeatureGate.Set(flagValue)
}

// NewFeatureGateState performs a one-time discovery of the featuregates API and,
// when it is served, loads featuregates/cluster to set isPreviewEnabled.
//
// The returned *FeatureGateState is always non-nil. Errors do not propagate from this function:
// stateErr is set so feature checks can fail closed without failing operator startup.
func NewFeatureGateState(ctx context.Context, configClient configv1client.Interface) *FeatureGateState {
	t := new(FeatureGateState)

	featureGateGVR := configv1.GroupVersion.WithResource(openshiftFeatureGateResource)
	discoverer := utils.NewResourceDiscoverer(featureGateGVR, configClient.Discovery())
	exists, err := discoverer.Discover()
	if err != nil {
		t.stateErr = fmt.Errorf("%w: %w", ErrFeatureGateDiscovery, err)
		return t
	}
	if !exists {
		t.apiPresent = false
		return t
	}

	t.apiPresent = true
	allowed, getErr := readClusterPreviewFeatureGate(ctx, configClient)
	if getErr != nil {
		t.stateErr = fmt.Errorf("%w: %w", ErrFeatureGateClusterGet, getErr)
		t.isPreviewEnabled = false
		return t
	}
	t.isPreviewEnabled = allowed
	return t
}

// readClusterPreviewFeatureGate returns whether spec.featureSet is in the preview allow-list and
// returns a non-nil error only when featuregates/cluster could not be read.
func readClusterPreviewFeatureGate(ctx context.Context, configClient configv1client.Interface) (bool, error) {
	featureGate, err := configClient.ConfigV1().FeatureGates().Get(ctx, "cluster", metav1.GetOptions{})
	if err != nil {
		log.Error(err, "cluster featureset: failed to get featuregates.config.openshift.io/cluster")
		return false, err
	}
	featureSet := featureGate.Spec.FeatureSet
	if !allowedPreviewFeatureSets.Has(featureSet) {
		log.V(1).Info("cluster featureset: spec.featureSet not in allowed list", "featureSet", featureSet)
		return false, nil
	}
	return true, nil
}

// IsIstioCSRFeatureGateEnabled returns true if the IstioCSR operator featuregate is enabled
// (--unsupported-addon-features=IstioCSR=true). FeatureIstioCSR is GA and does not depend on the
// cluster FeatureSet; the featuregate remains available so users can disable it when unused.
func IsIstioCSRFeatureGateEnabled() bool {
	return DefaultFeatureGate.Enabled(v1alpha1.FeatureIstioCSR)
}

// IsTrustManagerFeatureGateEnabled reports whether the TrustManager operand may run.
// When config.openshift.io FeatureGate is served, the cluster must use a preview FeatureSet
// (e.g. TechPreviewNoUpgrade); spec.featureSet Default does not start TrustManager even if
// --unsupported-addon-features=TrustManager=true. The internal operator featuregate must also be on.
// When stateErr is set, TrustManager stays off. When the FeatureGate API is not served, only the
// internal gate applies.
func (f *FeatureGateState) IsTrustManagerFeatureGateEnabled() bool {
	if !f.passesClusterPreviewGating(string(v1alpha1.FeatureTrustManager)) {
		return false
	}
	if !DefaultFeatureGate.Enabled(v1alpha1.FeatureTrustManager) {
		log.V(1).Info("TrustManager feature: internal featuregate is not enabled")
		return false
	}
	log.V(1).Info("TrustManager feature: enabled")
	return true
}

// Err returns the failure recorded during [NewFeatureGateState], or nil if none.
func (f *FeatureGateState) Err() error {
	if f == nil {
		return nil
	}
	return f.stateErr
}

// passesClusterPreviewGating reports whether cluster-side preview gating allows the caller
// to continue with a TechPreview feature. The caller must still check
// DefaultFeatureGate (--unsupported-addon-features). The feature value is used only for log fields.
//
// It returns false when stateErr is set, or when the API is served but the featureset is not in
// the preview allow-list.
//
// It returns true when the API is served, featuregates/cluster was read successfully, the featureset
// allows preview, and when the API is not served (only the internal operator featuregate applies).
func (f *FeatureGateState) passesClusterPreviewGating(feature string) bool {
	if f.stateErr != nil {
		switch {
		case errors.Is(f.stateErr, ErrFeatureGateClusterGet):
			log.Error(f.stateErr, "cluster feature gate: cannot read featuregates/cluster; feature disabled", "feature", feature)
		case errors.Is(f.stateErr, ErrFeatureGateDiscovery):
			log.Error(f.stateErr, "cluster feature gate: discovery failed; feature disabled", "feature", feature)
		default:
			log.Error(f.stateErr, "cluster feature gate: error; feature disabled", "feature", feature)
		}
		return false
	}
	if f.apiPresent {
		if !f.isPreviewEnabled {
			log.V(1).Info("cluster feature gate: preview featureset not enabled", "feature", feature)
			return false
		}
		log.V(1).Info("cluster feature gate: API served and preview featureset is also enabled", "feature", feature)
	} else {
		log.V(1).Info("cluster feature gate: API not served; using internal featuregate only", "feature", feature)
	}
	return true
}
