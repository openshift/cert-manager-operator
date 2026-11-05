package tlsprofile

import (
	"reflect"
	"sync"

	configv1 "github.com/openshift/api/config/v1"
	configinformersv1 "github.com/openshift/client-go/config/informers/externalversions/config/v1"
	libgocrypto "github.com/openshift/library-go/pkg/crypto"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

// SecurityProfileWatcher watches apiserver.config.openshift.io/cluster for
// tlsSecurityProfile and tlsAdherence changes and invokes OnChange so the
// operator can restart and pick up the new configuration.
//
// Initial values must be seeded from the profile applied at process start.
// A profile change restarts the process only when that profile is honored.
// A change in whether the profile is honored also restarts it. A profile edit
// that stays on Legacy does not: the metrics listener ignores the profile, so
// a restart would only hand off leader election.
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

	// OnChange is invoked when the honored TLS profile differs from the seeded
	// spec, or when whether the profile is honored changes. It is also invoked
	// when live settings become unresolvable while they must be honored
	// (fail-hard, consistent with bootstrap).
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

// handle restarts once when the honored profile changes, or when whether that profile is honored changes.
func (w *SecurityProfileWatcher) handle(apiServer *configv1.APIServer) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.fired {
		return
	}

	resolved, err := ResolveFromAPIServer(apiServer)
	initialHonor := libgocrypto.ShouldHonorClusterTLSProfile(w.InitialTLSAdherencePolicy)
	honorFlipped := initialHonor != resolved.Honor
	if err != nil {
		// The profile is still not applied. A resync must not restart.
		if !honorFlipped && !resolved.Honor {
			return
		}
		// Honored settings are unresolvable, or honor flipped. Restart so startup
		// can fail hard or drop a profile that is no longer applied.
		klog.Errorf("failed to resolve APIServer TLS settings, restarting to re-resolve: %v", err)
		w.fireLocked()
		return
	}

	// Startup spec is not meaningful when the profile could not be resolved.
	profileChanged := !w.InitialUnresolvable && !reflect.DeepEqual(w.InitialTLSProfileSpec, resolved.Spec)
	if !honorFlipped && !(profileChanged && resolved.Honor) {
		return
	}

	if profileChanged && resolved.Honor {
		klog.Infof("TLS security profile changed from %#v to %#v; triggering operator restart",
			w.InitialTLSProfileSpec, resolved.Spec)
	}
	if honorFlipped {
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
