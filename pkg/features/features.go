package features

import (
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	k8sfeaturegate "k8s.io/component-base/featuregate"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

var (
	// mutableFeatureGate holds operator featuregates from --unsupported-addon-features (read/write).
	mutableFeatureGate k8sfeaturegate.MutableFeatureGate = k8sfeaturegate.NewFeatureGate()

	// DefaultFeatureGate is the read-only view of operator featuregates.
	DefaultFeatureGate k8sfeaturegate.FeatureGate = mutableFeatureGate
)

// log is the package logger for operator featuregate enablement checks.
var log = ctrl.Log.WithName("features")

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

// IsIstioCSRFeatureGateEnabled returns true if the IstioCSR operator featuregate is enabled
// (--unsupported-addon-features=IstioCSR=true). FeatureIstioCSR is GA and does not depend on the
// cluster FeatureSet; the featuregate remains available so users can disable it when unused.
func IsIstioCSRFeatureGateEnabled() bool {
	if !DefaultFeatureGate.Enabled(v1alpha1.FeatureIstioCSR) {
		log.V(1).Info("IstioCSR feature: internal featuregate is not enabled")
		return false
	}
	log.V(1).Info("IstioCSR feature: enabled")
	return true
}

// IsTrustManagerFeatureGateEnabled reports whether the TrustManager operand may run.
//
// FeatureTrustManager is GA and enabled by default; it does not depend on the cluster FeatureSet.
// The featuregate remains available so users can disable it when unused
// (--unsupported-addon-features=TrustManager=false).
func IsTrustManagerFeatureGateEnabled() bool {
	if !DefaultFeatureGate.Enabled(v1alpha1.FeatureTrustManager) {
		log.V(1).Info("TrustManager feature: internal featuregate is not enabled")
		return false
	}
	log.V(1).Info("TrustManager feature: enabled")
	return true
}

// IsApproverPolicyManagerFeatureGateEnabled reports whether the internal operator
// gate is enabled.
func IsApproverPolicyManagerFeatureGateEnabled() bool {
	return DefaultFeatureGate.Enabled(v1alpha1.FeatureApproverPolicyManager)
}
