package operator

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
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
	utilruntime.Must(rbacv1.AddToScheme(scheme))
	utilruntime.Must(certmanagerv1.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

// Manager holds the manager resource for the istio-csr controller
type Manager struct {
	manager manager.Manager
}

// New creates a new manager.
func New() (*Manager, error) {
	setupLog.Info("controller", "version", version.Get())

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Logger:                 ctrl.Log.WithName("operator manager"),
		Scheme:                 scheme,
		HealthProbeBindAddress: ":8081",
		LeaderElection:         false,
		LeaderElectionID:       "10c233b5.certmanagers.operator.openshift.io",
		NewClient: func(config *rest.Config, options client.Options) (client.Client, error) {
			return client.New(config, options)
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create manager: %w", err)
	}
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return nil, fmt.Errorf("failed to set up health check: %w", err)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return nil, fmt.Errorf("failed to set up ready check: %w", err)
	}
	// +kubebuilder:scaffold:builder

	return &Manager{
		manager: mgr,
	}, nil
}

// Start starts the operator synchronously until a message is received from ctx.
func (mgr *Manager) Start(ctx context.Context) error {
	// TODO: can we pass this to ManagedController.SetupWithManager?
	mgr.manager.GetEventRecorderFor("cert-manager-istio-csr-controller").Event(&v1alpha1.IstioCSR{}, corev1.EventTypeNormal, "ControllerStarted", "controller is starting")
	return mgr.manager.Start(ctx)
}
