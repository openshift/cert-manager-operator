package operator

import (
	"context"
	"fmt"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/istiocsr"
	"github.com/openshift/cert-manager-operator/pkg/controller/trustmanager"
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
	utilruntime.Must(admissionregistrationv1.AddToScheme(scheme))
	utilruntime.Must(certmanagerv1.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

// Manager holds the manager resource for the controller-runtime based controllers
type Manager struct {
	manager manager.Manager
}

// ControllerManagerOptions configures which controllers to register with the manager.
type ControllerManagerOptions struct {
	EnableIstioCSR     bool
	EnableTrustManager bool
}

// NewControllerManager creates a new manager with the specified controllers enabled.
func NewControllerManager(opts ControllerManagerOptions) (*Manager, error) {
	setupLog.Info("setting up operator manager", "istioCSR", opts.EnableIstioCSR, "trustManager", opts.EnableTrustManager)
	setupLog.Info("controller", "version", version.Get())

	// Build a composite cache builder that merges cache configurations from all enabled controllers
	cacheBuilder := buildCompositeCacheBuilder(opts)

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:   scheme,
		NewCache: cacheBuilder,
		Logger:   ctrl.Log.WithName("operator-manager"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create manager: %w", err)
	}

	if opts.EnableIstioCSR {
		r, err := istiocsr.New(mgr)
		if err != nil {
			return nil, fmt.Errorf("failed to create %s reconciler object: %w", istiocsr.ControllerName, err)
		}
		if err := r.SetupWithManager(mgr); err != nil {
			return nil, fmt.Errorf("failed to create %s controller: %w", istiocsr.ControllerName, err)
		}
	}

	if opts.EnableTrustManager {
		r, err := trustmanager.New(mgr)
		if err != nil {
			return nil, fmt.Errorf("failed to create %s reconciler object: %w", trustmanager.ControllerName, err)
		}
		if err := r.SetupWithManager(mgr); err != nil {
			return nil, fmt.Errorf("failed to create %s controller: %w", trustmanager.ControllerName, err)
		}
	}

	// +kubebuilder:scaffold:builder

	return &Manager{
		manager: mgr,
	}, nil
}

// buildCompositeCacheBuilder returns a cache builder for the enabled controllers.
func buildCompositeCacheBuilder(opts ControllerManagerOptions) cache.NewCacheFunc {
	if opts.EnableIstioCSR && !opts.EnableTrustManager {
		return istiocsr.NewCacheBuilder
	}
	if opts.EnableTrustManager && !opts.EnableIstioCSR {
		return trustmanager.NewCacheBuilder
	}
	// Both enabled: use nil to let the manager create a default cache without label filtering.
	// Each controller's watches already have predicate filters, so this is safe.
	return nil
}

// Start starts the operator synchronously until a message is received from ctx.
func (mgr *Manager) Start(ctx context.Context) error {
	setupLog.Info("controller manager is starting")
	return mgr.manager.Start(ctx)
}
