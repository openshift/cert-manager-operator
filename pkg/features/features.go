package features

import (
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/component-base/featuregate"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

var (
	// mutableFeatureGate is the top-level FeatureGate with mutability (read/write).
	mutableFeatureGate featuregate.MutableFeatureGate = featuregate.NewFeatureGate()

	// DefaultFeatureGate is the top-level shared global FeatureGate (read-only).
	DefaultFeatureGate featuregate.FeatureGate = mutableFeatureGate
)

func init() {
	utilruntime.Must(mutableFeatureGate.Add(v1alpha1.OperatorFeatureGates))
}

func SetupWithFlagValue(flagValue string) error {
	if flagValue == "" {
		return nil // use defined defaults
	}
	return mutableFeatureGate.Set(flagValue)
}
