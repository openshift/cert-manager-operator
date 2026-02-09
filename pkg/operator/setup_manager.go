package operator

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"

	ctrl "sigs.k8s.io/controller-runtime"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/http01proxy"
	"github.com/openshift/cert-manager-operator/pkg/controller/istiocsr"
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

// Manager holds the manager resource for the istio-csr controller.
type Manager struct {
	manager manager.Manager
}

// NewControllerManager creates a new manager.
func NewControllerManager() (*Manager, error) {
	setupLog.Info("setting up operator manager", "controller", istiocsr.ControllerName)
	setupLog.Info("controller", "version", version.Get())

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		// Use custom cache builder to configure label selectors for managed resources
		NewCache: istiocsr.NewCacheBuilder,
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

	// http01proxy controller
	rh, err := http01proxy.New(mgr)
	if err != nil {
		return nil, fmt.Errorf("failed to create %s reconciler object: %w", http01proxy.ControllerName, err)
	}
	if err := rh.SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("failed to create %s controller: %w", http01proxy.ControllerName, err)
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
