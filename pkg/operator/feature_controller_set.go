package operator

import (
	"context"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/features"
)

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

func (fc *FeatureControllerSet) waitAndRunIfEnabled(ctx context.Context, mgr ctrl.Manager, feat features.FeatureAccessor, watch chan struct{}) {
	shouldEnable := false

	for !shouldEnable {
		select {
		case <-ctx.Done():
			fc.log.V(1).Info("graceful shutdown")
			return

		case <-watch:
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
func (cs FeatureControllerSetFactory) RunWithManagerOnceEnabled(ctx context.Context, mgr ctrl.Manager, feats features.FeatureAccessor) {
	// subscribe to watch features getting enabled at runtime
	featureWatcher := features.NewTickWatcher()
	feats.AddEventWatcher(featureWatcher)

	individualFeatWatch := make([]chan struct{}, len(cs))
	for i, c := range cs {
		individualFeatWatch[i] = make(chan struct{})
		go c.waitAndRunIfEnabled(ctx, mgr, feats, individualFeatWatch[i])
	}

	watchChan := featureWatcher.GetTicker()
	go func() {
		for {
			select {
			case _, ok := <-watchChan:
				if !ok {
					return
				}

				// once an event is received from the feature accessor
				// relay the event to each feature watcher
				for i := range individualFeatWatch {
					individualFeatWatch[i] <- struct{}{}
				}

			case <-ctx.Done():
				return
			}
		}
	}()
}
