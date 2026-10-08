package tlsprofile

import (
	"reflect"
	"sync"

	configv1 "github.com/openshift/api/config/v1"
	configinformersv1 "github.com/openshift/client-go/config/informers/externalversions/config/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

// SecurityProfileWatcher watches apiserver.config.openshift.io/cluster for
// tlsSecurityProfile and tlsAdherence changes and invokes OnChange so the
// operator can restart and pick up the new configuration.
//
// Initial values must be seeded from the profile that was applied (or skipped)
// at process start, including when adherence is Legacy, so a profile change and
// a Strict↔Legacy transition each trigger a restart.
//
// OnChange is invoked at most once per process lifetime. The operator HTTPS
// metrics server cannot be reconfigured in place; a single graceful restart
// is enough. OnChange must not call back into the watcher.
type SecurityProfileWatcher struct {
	mu sync.Mutex

	InitialTLSProfileSpec     configv1.TLSProfileSpec
	InitialTLSAdherencePolicy configv1.TLSAdherencePolicy
	// InitialUnresolvable is set when startup could not resolve the profile
	// and left serving at Controllercmd defaults because adherence did not
	// require honoring it. Spec is ignored while this is true.
	InitialUnresolvable bool

	// OnChange is invoked when either the resolved TLS profile spec or the
	// adherence policy differs from the seeded initial values, or when the
	// live APIServer TLS settings become unresolvable (fail-hard, consistent
	// with bootstrap when adherence requires honoring the profile).
	OnChange func()

	fired bool
}

// Start registers an informer handler for the cluster APIServer. The informer
// factory must be started by the caller after Start returns.
func (w *SecurityProfileWatcher) Start(apiServerInformer configinformersv1.APIServerInformer) error {
	_, err := apiServerInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			apiServer, ok := obj.(*configv1.APIServer)
			if !ok || apiServer.Name != APIServerClusterName {
				return
			}
			w.handle(apiServer)
		},
		UpdateFunc: func(_, newObj interface{}) {
			apiServer, ok := newObj.(*configv1.APIServer)
			if !ok || apiServer.Name != APIServerClusterName {
				return
			}
			w.handle(apiServer)
		},
		// Deletion of the singleton is unexpected; treat as Intermediate/empty and
		// still compare so a later recreation can be detected after restart.
		DeleteFunc: func(obj interface{}) {
			apiServer, ok := obj.(*configv1.APIServer)
			if !ok {
				tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
				if !ok {
					return
				}
				apiServer, ok = tombstone.Obj.(*configv1.APIServer)
				if !ok {
					return
				}
			}
			if apiServer.Name != APIServerClusterName {
				return
			}
			w.handle(&configv1.APIServer{})
		},
	})
	return err
}

func (w *SecurityProfileWatcher) handle(apiServer *configv1.APIServer) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.fired {
		return
	}

	resolved, err := ResolveFromAPIServer(apiServer)
	if err != nil {
		// Same unresolvable profile and adherence as startup: serving is still
		// the Controllercmd default, so a resync must not restart.
		if w.InitialUnresolvable && w.InitialTLSAdherencePolicy == resolved.Adherence {
			return
		}
		// Consistent with bootstrap fail-hard: an unresolvable live config
		// means we can no longer vouch for serving TLS settings.
		klog.Errorf("failed to resolve APIServer TLS settings, restarting to re-resolve: %v", err)
		w.fireLocked()
		return
	}

	if w.InitialUnresolvable {
		klog.Info("APIServer TLS settings became resolvable; triggering operator restart")
		w.fireLocked()
		return
	}

	profileChanged := !reflect.DeepEqual(w.InitialTLSProfileSpec, resolved.Spec)
	adherenceChanged := w.InitialTLSAdherencePolicy != resolved.Adherence
	if !profileChanged && !adherenceChanged {
		return
	}

	if profileChanged {
		klog.Infof("TLS security profile changed from %#v to %#v; triggering operator restart",
			w.InitialTLSProfileSpec, resolved.Spec)
	}
	if adherenceChanged {
		klog.Infof("TLS adherence policy changed from %q to %q; triggering operator restart",
			w.InitialTLSAdherencePolicy, resolved.Adherence)
	}

	w.fireLocked()
}

// fireLocked must be called with w.mu held.
func (w *SecurityProfileWatcher) fireLocked() {
	w.fired = true
	if w.OnChange != nil {
		w.OnChange()
	}
}
