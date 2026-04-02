package operator

import (
	"context"
	"fmt"
	"reflect"

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
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/controller/istiocsr"
	"github.com/openshift/cert-manager-operator/pkg/controller/trustmanager"
	"github.com/openshift/cert-manager-operator/pkg/version"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup-manager")
)

// ConfigMap is intentionally excluded from both istioCSRManagedResources and
// trustManagerManagedResources. Multiple controllers need to watch ConfigMaps
// that do not carry the managed-resource label:
//
//  1. TrustManager watches both its managed ConfigMaps (e.g., the default CA
//     package ConfigMap, which carries the managed-resource label) and the
//     cert-manager-operator-trusted-ca-bundle ConfigMap (added in the OLM bundle manifest).
//     The latter does not carry the managed-resource label.
//
//  2. IstioCSR watches both its managed ConfigMaps (with the managed-resource
//     label) and user-created ConfigMaps identified by the
//     istiocsr.openshift.operator.io/watched-by label — a different label key
//     entirely from the managed-resource label (app).
//
// The cache uses a single labels.Selector per GVK. The In operator can match
// multiple values for the same key (e.g., app in (value1, value2)), but
// requirements on different keys are always ANDed. There is no way to express
// "app in (...) OR watched-by exists" in a single selector. A shared app label
// value could solve case 1, but case 2 requires matching across different label
// keys, which the Kubernetes label selector spec does not support.
//
// ConfigMaps therefore use the default unfiltered informer, and each controller
// applies predicate-level filtering to select only the events it cares about.

// istioCSRManagedResources defines the resources managed by the IstioCSR controller.
// These resources will be watched with a label selector filter.
var istioCSRManagedResources = []client.Object{
	&certmanagerv1.Certificate{},
	&appsv1.Deployment{},
	&rbacv1.ClusterRole{},
	&rbacv1.ClusterRoleBinding{},
	&rbacv1.Role{},
	&rbacv1.RoleBinding{},
	&corev1.Service{},
	&corev1.ServiceAccount{},
	&networkingv1.NetworkPolicy{},
}

// trustManagerManagedResources defines the resources managed by the TrustManager controller.
// These resources will be watched with a label selector filter.
//
// cert-manager Issuer (and ClusterIssuer, which is never listed here) must not use a
// managed-resource label selector: IstioCSR reconciles user-created Issuers referenced
// from the spec, which are not labeled by the operator. Those types are left out of
// ByObject so they use the manager cache’s default unfiltered informer per GVK.
var trustManagerManagedResources = []client.Object{
	&certmanagerv1.Certificate{},
	&appsv1.Deployment{},
	&rbacv1.ClusterRole{},
	&rbacv1.ClusterRoleBinding{},
	&rbacv1.Role{},
	&rbacv1.RoleBinding{},
	&corev1.Service{},
	&corev1.ServiceAccount{},
	&admissionregistrationv1.ValidatingWebhookConfiguration{},
}

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

// Manager holds the manager resource for the controller-runtime based controllers.
type Manager struct {
	manager manager.Manager
}

// ControllerConfig specifies which controllers to enable in the unified manager.
type ControllerConfig struct {
	EnableIstioCSR     bool
	EnableTrustManager bool
}

// NewControllerManager creates a unified manager for all enabled operand controllers.
// It shares a single metrics server, cache, and client across all controllers.
func NewControllerManager(config ControllerConfig) (*Manager, error) {
	setupLog.Info("setting up unified operator manager")
	setupLog.Info("controller", "version", version.Get())
	setupLog.Info("enabled controllers", "istioCSR", config.EnableIstioCSR, "trustManager", config.EnableTrustManager)

	cacheBuilder := newUnifiedCacheBuilder(config)

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:   scheme,
		NewCache: cacheBuilder,
		Logger:   ctrl.Log.WithName("operator-manager"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create manager: %w", err)
	}

	// Setup enabled controllers
	if config.EnableIstioCSR {
		if err := setupIstioCSRController(mgr); err != nil {
			return nil, err
		}
	}

	if config.EnableTrustManager {
		if err := setupTrustManagerController(mgr); err != nil {
			return nil, err
		}
	}

	return &Manager{
		manager: mgr,
	}, nil
}

// setupIstioCSRController creates and registers the IstioCSR controller with the manager.
func setupIstioCSRController(mgr ctrl.Manager) error {
	setupLog.Info("setting up controller", "name", istiocsr.ControllerName)
	r, err := istiocsr.New(mgr)
	if err != nil {
		return fmt.Errorf("failed to create %s reconciler object: %w", istiocsr.ControllerName, err)
	}
	if err := r.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("failed to create %s controller: %w", istiocsr.ControllerName, err)
	}
	return nil
}

// setupTrustManagerController creates and registers the TrustManager controller with the manager.
func setupTrustManagerController(mgr ctrl.Manager) error {
	setupLog.Info("setting up controller", "name", trustmanager.ControllerName)
	r, err := trustmanager.New(mgr)
	if err != nil {
		return fmt.Errorf("failed to create %s reconciler object: %w", trustmanager.ControllerName, err)
	}
	if err := r.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("failed to create %s controller: %w", trustmanager.ControllerName, err)
	}
	return nil
}

// newUnifiedCacheBuilder creates a cache builder that combines cache configurations
// for all enabled controllers into a single unified cache.
func newUnifiedCacheBuilder(config ControllerConfig) cache.NewCacheFunc {
	return func(restConfig *rest.Config, opts cache.Options) (cache.Cache, error) {
		objectList, err := buildCacheObjectList(config)
		if err != nil {
			return nil, err
		}
		opts.ByObject = objectList
		return cache.New(restConfig, opts)
	}
}

// buildCacheObjectList creates the cache configuration with label selectors
// for managed resources based on enabled controllers.
// All controllers use common.ManagedResourceLabelKey as the label key.
func buildCacheObjectList(config ControllerConfig) (map[client.Object]cache.ByObject, error) {
	objectList := make(map[client.Object]cache.ByObject)

	if config.EnableIstioCSR {
		if err := addControllerCacheConfig(objectList, istiocsr.RequestEnqueueLabelValue, istioCSRManagedResources); err != nil {
			return nil, fmt.Errorf("failed to configure IstioCSR cache: %w", err)
		}
		// IstioCSR CR - no label filter needed
		objectList[&v1alpha1.IstioCSR{}] = cache.ByObject{}
	}

	if config.EnableTrustManager {
		if err := addControllerCacheConfig(objectList, trustmanager.RequestEnqueueLabelValue, trustManagerManagedResources); err != nil {
			return nil, fmt.Errorf("failed to configure TrustManager cache: %w", err)
		}
		// TrustManager CR - no label filter needed
		objectList[&v1alpha1.TrustManager{}] = cache.ByObject{}
	}

	return objectList, nil
}

// addControllerCacheConfig adds cache configuration for a controller's managed resources.
// All controllers use common.ManagedResourceLabelKey as the label key.
// If a resource type already exists (from another controller), the label selector is updated
// to use the 'In' operator to match resources from either controller.
func addControllerCacheConfig(objectList map[client.Object]cache.ByObject, labelValue string, resources []client.Object) error {
	labelKey := common.ManagedResourceLabelKey

	for _, res := range resources {
		resType := fmt.Sprintf("%T", res)

		if existingKey, existing, found := findExistingCacheEntry(objectList, res); found {
			// Resource already configured by another controller
			// Merge label values using 'In' operator: app in (value1, value2)
			existingReqs, _ := existing.Label.Requirements()
			var existingValues []string
			for _, req := range existingReqs {
				if req.Key() == labelKey {
					existingValues = req.Values().List()
					break
				}
			}

			// Create new requirement with both values
			mergedValues := append(existingValues, labelValue)
			mergedReq, err := labels.NewRequirement(labelKey, selection.In, mergedValues)
			if err != nil {
				return fmt.Errorf("failed to create merged label requirement for key %q with values %v: %w", labelKey, mergedValues, err)
			}
			objectList[existingKey] = cache.ByObject{Label: labels.NewSelector().Add(*mergedReq)}
			setupLog.V(4).Info("merged label selector for shared resource", "type", resType, "values", mergedValues)
		} else {
			// First controller to configure this resource
			labelReq, err := labels.NewRequirement(labelKey, selection.Equals, []string{labelValue})
			if err != nil {
				return fmt.Errorf("failed to create label requirement for key %q with value %q: %w", labelKey, labelValue, err)
			}
			objectList[res] = cache.ByObject{Label: labels.NewSelector().Add(*labelReq)}
		}
	}
	return nil
}

// findExistingCacheEntry looks up an entry in the cache object map by Go type
// rather than pointer identity, since map[client.Object] uses interface comparison
// which compares pointers, not types.
func findExistingCacheEntry(objectList map[client.Object]cache.ByObject, target client.Object) (client.Object, cache.ByObject, bool) {
	targetType := reflect.TypeOf(target)
	for key, val := range objectList {
		if reflect.TypeOf(key) == targetType {
			return key, val, true
		}
	}
	return nil, cache.ByObject{}, false
}

// Start starts the unified controller manager synchronously until ctx is cancelled.
func (mgr *Manager) Start(ctx context.Context) error {
	return mgr.manager.Start(ctx)
}
