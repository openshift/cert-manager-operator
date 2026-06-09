package operator

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/istiocsr"
	"github.com/openshift/cert-manager-operator/pkg/controller/trustmanager"
	"github.com/openshift/cert-manager-operator/pkg/features"
	"github.com/openshift/cert-manager-operator/pkg/version"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup-manager")
)

func init() {
	ctrllog.SetLogger(klog.NewKlogr())

	utilruntime.Must(clientscheme.AddToScheme(scheme))
	utilruntime.Must(appsv1.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(networkingv1.AddToScheme(scheme))
	utilruntime.Must(rbacv1.AddToScheme(scheme))
	utilruntime.Must(certmanagerv1.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

// Manager holds the manager resource for the operator controllers
type Manager struct {
	manager manager.Manager
}

// newCacheBuilder returns a cache builder function that configures label selectors
// for managed resources from all enabled controllers. When TrustManager feature gate
// is enabled, the cache includes resources from both istiocsr and trustmanager
// controllers. This ensures that label-filtered resource types (Deployments, Services,
// RBAC, etc.) from both controllers are properly cached.
func newCacheBuilder(trustManagerEnabled bool) cache.NewCacheFunc {
	return func(config *rest.Config, opts cache.Options) (cache.Cache, error) {
		// Determine which label values to include in the cache selector.
		// Both istiocsr and trustmanager use the "app" label key for filtering.
		managedLabelValues := []string{"cert-manager-istio-csr"}
		if trustManagerEnabled {
			managedLabelValues = append(managedLabelValues, "trust-manager")
		}

		managedResourceLabelReq, err := labels.NewRequirement("app", selection.In, managedLabelValues)
		if err != nil {
			return nil, fmt.Errorf("invalid cache label requirement for %q: %w", "app", err)
		}
		managedResourceLabelReqSelector := labels.NewSelector().Add(*managedResourceLabelReq)

		// Configure cache with label selectors for managed resources
		opts.ByObject = map[client.Object]cache.ByObject{
			// Explicitly include IstioCSR to ensure the cache properly watches and syncs all IstioCSR objects
			&v1alpha1.IstioCSR{}: {},
			// Resources managed by istiocsr controller (with label selectors)
			&certmanagerv1.Certificate{}: {
				Label: managedResourceLabelReqSelector,
			},
			&appsv1.Deployment{}: {
				Label: managedResourceLabelReqSelector,
			},
			&rbacv1.ClusterRole{}: {
				Label: managedResourceLabelReqSelector,
			},
			&rbacv1.ClusterRoleBinding{}: {
				Label: managedResourceLabelReqSelector,
			},
			&rbacv1.Role{}: {
				Label: managedResourceLabelReqSelector,
			},
			&rbacv1.RoleBinding{}: {
				Label: managedResourceLabelReqSelector,
			},
			&corev1.Service{}: {
				Label: managedResourceLabelReqSelector,
			},
			&corev1.ServiceAccount{}: {
				Label: managedResourceLabelReqSelector,
			},
			&networkingv1.NetworkPolicy{}: {
				Label: managedResourceLabelReqSelector,
			},
		}

		// If TrustManager is enabled, also include TrustManager objects in the cache
		if trustManagerEnabled {
			opts.ByObject[&v1alpha1.TrustManager{}] = cache.ByObject{}
		}

		return cache.New(config, opts)
	}
}

// NewControllerManager creates a new manager.
func NewControllerManager() (*Manager, error) {
	setupLog.Info("setting up operator manager", "controller", istiocsr.ControllerName)
	setupLog.Info("controller", "version", version.Get())

	trustManagerEnabled := features.DefaultFeatureGate.Enabled(v1alpha1.FeatureTrustManager)

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		// Use combined cache builder to configure label selectors for managed resources
		// from all enabled controllers.
		NewCache: newCacheBuilder(trustManagerEnabled),
		Logger:   ctrl.Log.WithName("operator-manager"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create manager: %w", err)
	}

	r, err := istiocsr.New(mgr)
	if err != nil {
		return nil, fmt.Errorf("failed to create %s reconciler object: %w", istiocsr.ControllerName, err)
	}
	if err := r.SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("failed to create %s controller: %w", istiocsr.ControllerName, err)
	}
	// Register trust-manager controller if the TrustManager feature gate is enabled
	if trustManagerEnabled {
		setupLog.Info("TrustManager feature gate is enabled, registering trust-manager controller")
		tmr, err := trustmanager.New(mgr)
		if err != nil {
			return nil, fmt.Errorf("failed to create %s reconciler object: %w", trustmanager.ControllerName, err)
		}
		if err := tmr.SetupWithManager(mgr); err != nil {
			return nil, fmt.Errorf("failed to create %s controller: %w", trustmanager.ControllerName, err)
		}
	} else {
		setupLog.Info("TrustManager feature gate is disabled, skipping trust-manager controller registration")
	}
	// +kubebuilder:scaffold:builder

	return &Manager{
		manager: mgr,
	}, nil
}

// Start starts the operator synchronously until a message is received from ctx.
func (mgr *Manager) Start(ctx context.Context) error {
	mgr.manager.GetEventRecorderFor("cert-manager-istio-csr-controller").Event(&v1alpha1.IstioCSR{}, corev1.EventTypeNormal, "ControllerStarted", "controller is starting")
	return mgr.manager.Start(ctx)
}
