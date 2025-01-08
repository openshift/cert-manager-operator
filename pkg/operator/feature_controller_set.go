package operator

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	cm "github.com/openshift/cert-manager-operator/pkg/controller/certmanager"
	ctrl "sigs.k8s.io/controller-runtime"
)

const retryInterval = 2 * time.Minute

type FeatureControllerSet struct {
	v1alpha1.FeatureName

	controllers []ManagedController
	log         logr.Logger
}

func NewFeatureControllerSet(feature v1alpha1.FeatureName, controllers []ManagedController) *FeatureControllerSet {
	return &FeatureControllerSet{
		FeatureName: feature,
		controllers: controllers,
	}
}

func (fc *FeatureControllerSet) run(mgr ctrl.Manager) error {
	for _, c := range fc.controllers {
		err := c.SetupWithManager(mgr)
		if err != nil {
			return err
		}
	}

	return nil
}

func (fc *FeatureControllerSet) pollAndRunIfEnabled(ctx context.Context, mgr ctrl.Manager, feat cm.FeatureAccessor) {
	shouldEnable := false

	for !shouldEnable {
		select {
		case <-ctx.Done():
			fc.log.V(1).Info("graceful shutdown")
			return

		case <-time.Tick(retryInterval):
			if feat.IsFeatureEnabled(fc.FeatureName) {
				shouldEnable = true
			}

		}
	}

	err := fc.run(mgr)
	if err != nil {
		fc.log.V(1).Error(err, "could not start controller(s)", "feature", fc.FeatureName)
	}
}

type FeatureControllerSetFactory []FeatureControllerSet

// RunWithManagerOnceEnabled
func (cs FeatureControllerSetFactory) RunWithManagerOnceEnabled(ctx context.Context, mgr ctrl.Manager, features cm.FeatureAccessor) {
	for _, c := range cs {
		go c.pollAndRunIfEnabled(ctx, mgr, features)
	}
}
