package operator

import (
	"context"
	"fmt"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
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
	utilruntime.Must(admissionregistrationv1.AddToScheme(scheme))
	utilruntime.Must(certmanagerv1.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

// Manager holds the manager resource for the operator controllers
type Manager struct {
	manager manager.Manager
}

// newCombinedCacheBuilder returns a cache builder function that configures label selectors
// for resources managed by both the istiocsr and trustmanager controllers. When the TrustManager
// feature gate is enabled, the cache includes entries for trust-manager managed resources.
func newCombinedCacheBuilder(trustManagerEnabled bool) cache.NewCacheFunc {
	return func(config *rest.Config, opts cache.Options) (cache.Cache, error) {
		istiocsrLabelReq, err := labels.NewRequirement("app", selection.Equals, []string{"cert-manager-istio-csr"})
		if err != nil {
			return nil, fmt.Errorf("invalid cache label requirement for istiocsr: %w", err)
		}
		istiocsrSelector := labels.NewSelector().Add(*istiocsrLabelReq)

		// Configure cache with label selectors for istiocsr managed resources
		opts.ByObject = map[client.Object]cache.ByObject{
			&v1alpha1.IstioCSR{}: {},
			&certmanagerv1.Certificate{}: {
				Label: istiocsrSelector,
			},
			&appsv1.Deployment{}: {
				Label: istiocsrSelector,
			},
			&rbacv1.ClusterRole{}: {
				Label: istiocsrSelector,
			},
			&rbacv1.ClusterRoleBinding{}: {
				Label: istiocsrSelector,
			},
			&rbacv1.Role{}: {
				Label: istiocsrSelector,
			},
			&rbacv1.RoleBinding{}: {
				Label: istiocsrSelector,
			},
			&corev1.Service{}: {
				Label: istiocsrSelector,
			},
			&corev1.ServiceAccount{}: {
				Label: istiocsrSelector,
			},
			&networkingv1.NetworkPolicy{}: {
				Label: istiocsrSelector,
			},
		}

		if trustManagerEnabled {
			trustManagerLabelReq, err := labels.NewRequirement("app", selection.Equals, []string{"trust-manager"})
			if err != nil {
				return nil, fmt.Errorf("invalid cache label requirement for trustmanager: %w", err)
			}
			trustManagerSelector := labels.NewSelector().Add(*trustManagerLabelReq)

			// For shared resource types, use a selector that matches either controller's label
			eitherLabelReq, err := labels.NewRequirement("app", selection.In, []string{"cert-manager-istio-csr", "trust-manager"})
			if err != nil {
				return nil, fmt.Errorf("invalid cache label requirement for combined selector: %w", err)
			}
			eitherSelector := labels.NewSelector().Add(*eitherLabelReq)

			// Add TrustManager CR to cache
			opts.ByObject[&v1alpha1.TrustManager{}] = cache.ByObject{}

			// Add ValidatingWebhookConfiguration for trust-manager
			opts.ByObject[&admissionregistrationv1.ValidatingWebhookConfiguration{}] = cache.ByObject{
				Label: trustManagerSelector,
			}

			// Update shared resource types to use the combined selector
			opts.ByObject[&certmanagerv1.Certificate{}] = cache.ByObject{Label: eitherSelector}
			opts.ByObject[&appsv1.Deployment{}] = cache.ByObject{Label: eitherSelector}
			opts.ByObject[&rbacv1.ClusterRole{}] = cache.ByObject{Label: eitherSelector}
			opts.ByObject[&rbacv1.ClusterRoleBinding{}] = cache.ByObject{Label: eitherSelector}
			opts.ByObject[&rbacv1.Role{}] = cache.ByObject{Label: eitherSelector}
			opts.ByObject[&rbacv1.RoleBinding{}] = cache.ByObject{Label: eitherSelector}
			opts.ByObject[&corev1.Service{}] = cache.ByObject{Label: eitherSelector}
			opts.ByObject[&corev1.ServiceAccount{}] = cache.ByObject{Label: eitherSelector}

			// ConfigMap is used by trust-manager for default CA package
			opts.ByObject[&corev1.ConfigMap{}] = cache.ByObject{Label: trustManagerSelector}
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
		// Use combined cache builder to configure label selectors for resources
		// managed by both istiocsr and trustmanager controllers.
		NewCache: newCombinedCacheBuilder(trustManagerEnabled),
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
		tmReconciler, err := trustmanager.New(mgr)
		if err != nil {
			return nil, fmt.Errorf("failed to create %s reconciler object: %w", trustmanager.ControllerName, err)
		}
		if err := tmReconciler.SetupWithManager(mgr); err != nil {
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
