package trustmanager

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/go-logr/logr"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

var (
	// requestEnqueueLabelKey is the label key name used for filtering reconcile
	// events to include only the resources created by the controller.
	requestEnqueueLabelKey = "app"

	// requestEnqueueLabelValue is the label value used for filtering reconcile
	// events to include only the resources created by the controller.
	requestEnqueueLabelValue = trustManagerCommonName
)

// Reconciler reconciles a TrustManager object
type Reconciler struct {
	ctrlClient

	ctx           context.Context
	eventRecorder record.EventRecorder
	log           logr.Logger
	scheme        *runtime.Scheme
}

// +kubebuilder:rbac:groups=operator.openshift.io,resources=trustmanagers,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=operator.openshift.io,resources=trustmanagers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.openshift.io,resources=trustmanagers/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificates;issuers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=validatingwebhookconfigurations,verbs=get;list;watch;create;update;patch;delete

// NewCacheBuilder returns a cache builder function configured with label selectors
// for managed resources. This function is used by the manager to create its cache
// to ensure the reconciler reads from the same cache that the controller's watches use.
func NewCacheBuilder(config *rest.Config, opts cache.Options) (cache.Cache, error) {
	managedResourceLabelReq, err := labels.NewRequirement(requestEnqueueLabelKey, selection.Equals, []string{requestEnqueueLabelValue})
	if err != nil {
		return nil, fmt.Errorf("invalid cache label requirement for %q: %w", requestEnqueueLabelKey, err)
	}
	managedResourceLabelReqSelector := labels.NewSelector().Add(*managedResourceLabelReq)

	// Configure cache with label selectors for managed resources
	opts.ByObject = map[client.Object]cache.ByObject{
		// Explicitly include TrustManager to ensure the cache properly watches and syncs all TrustManager objects
		&v1alpha1.TrustManager{}: {},
		// Resources managed by controller (with label selectors)
		&certmanagerv1.Certificate{}: {
			Label: managedResourceLabelReqSelector,
		},
		&certmanagerv1.Issuer{}: {
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
		// ConfigMaps in operator and operand namespaces (for DefaultCAPackage)
		&corev1.ConfigMap{}: {},
	}

	return cache.New(config, opts)
}

// New returns a new Reconciler instance.
func New(mgr ctrl.Manager) (*Reconciler, error) {
	c, err := NewClient(mgr)
	if err != nil {
		return nil, err
	}
	return &Reconciler{
		ctrlClient:    c,
		ctx:           context.Background(),
		eventRecorder: mgr.GetEventRecorderFor(ControllerName),
		log:           ctrl.Log.WithName(ControllerName),
		scheme:        mgr.GetScheme(),
	}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	mapFunc := func(ctx context.Context, obj client.Object) []reconcile.Request {
		r.log.V(4).Info("received reconcile event", "object", fmt.Sprintf("%T", obj), "name", obj.GetName(), "namespace", obj.GetNamespace())

		objLabels := obj.GetLabels()
		if objLabels != nil && objLabels[requestEnqueueLabelKey] == requestEnqueueLabelValue {
			return []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{
						Name: trustManagerObjectName,
					},
				},
			}
		}

		r.log.V(4).Info("object not of interest, ignoring reconcile event", "object", fmt.Sprintf("%T", obj), "name", obj.GetName(), "namespace", obj.GetNamespace())
		return []reconcile.Request{}
	}

	// mapConfigMapFunc maps ConfigMap changes to TrustManager reconcile requests.
	// This watches the trusted CA bundle ConfigMap in operator namespace and the
	// default CA package ConfigMap in operand namespace.
	mapConfigMapFunc := func(ctx context.Context, obj client.Object) []reconcile.Request {
		name := obj.GetName()
		ns := obj.GetNamespace()
		if (name == trustedCABundleConfigMapName && ns == operatorNamespace) ||
			(name == defaultCAPackageConfigMapName && ns == operandNamespace) {
			return []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{
						Name: trustManagerObjectName,
					},
				},
			}
		}

		// Also reconcile on managed ConfigMaps
		objLabels := obj.GetLabels()
		if objLabels != nil && objLabels[requestEnqueueLabelKey] == requestEnqueueLabelValue {
			return []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{
						Name: trustManagerObjectName,
					},
				},
			}
		}

		return []reconcile.Request{}
	}

	// predicate function to ignore events for objects not managed by controller.
	controllerManagedResources := predicate.NewPredicateFuncs(func(object client.Object) bool {
		return object.GetLabels() != nil && object.GetLabels()[requestEnqueueLabelKey] == requestEnqueueLabelValue
	})

	withIgnoreStatusUpdatePredicates := builder.WithPredicates(predicate.GenerationChangedPredicate{}, controllerManagedResources)
	controllerManagedResourcePredicates := builder.WithPredicates(controllerManagedResources)

	// ConfigMap predicate: watch managed ConfigMaps and the CA bundle ConfigMaps
	configMapPredicate := predicate.NewPredicateFuncs(func(object client.Object) bool {
		name := object.GetName()
		ns := object.GetNamespace()
		if (name == trustedCABundleConfigMapName && ns == operatorNamespace) ||
			(name == defaultCAPackageConfigMapName && ns == operandNamespace) {
			return true
		}
		return object.GetLabels() != nil && object.GetLabels()[requestEnqueueLabelKey] == requestEnqueueLabelValue
	})
	configMapPredicates := builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}, configMapPredicate)

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.TrustManager{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Named(ControllerName).
		Watches(&certmanagerv1.Certificate{}, handler.EnqueueRequestsFromMapFunc(mapFunc), withIgnoreStatusUpdatePredicates).
		Watches(&certmanagerv1.Issuer{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerManagedResourcePredicates).
		Watches(&appsv1.Deployment{}, handler.EnqueueRequestsFromMapFunc(mapFunc), withIgnoreStatusUpdatePredicates).
		Watches(&rbacv1.ClusterRole{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerManagedResourcePredicates).
		Watches(&rbacv1.ClusterRoleBinding{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerManagedResourcePredicates).
		Watches(&rbacv1.Role{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerManagedResourcePredicates).
		Watches(&rbacv1.RoleBinding{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerManagedResourcePredicates).
		Watches(&corev1.Service{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerManagedResourcePredicates).
		Watches(&corev1.ServiceAccount{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerManagedResourcePredicates).
		Watches(&corev1.ConfigMap{}, handler.EnqueueRequestsFromMapFunc(mapConfigMapFunc), configMapPredicates).
		Complete(r)
}

// Reconcile function to compare the state specified by the TrustManager object against the actual cluster state,
// and to make the cluster state reflect the state specified by the user.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.log.V(1).Info("reconciling", "request", req)

	// Fetch the trustmanagers.operator.openshift.io CR
	trustManager := &v1alpha1.TrustManager{}
	if err := r.Get(ctx, req.NamespacedName, trustManager); err != nil {
		if errors.IsNotFound(err) {
			// NotFound errors, since they can't be fixed by an immediate
			// requeue (have to wait for a new notification), and can be processed
			// on deleted requests.
			r.log.V(1).Info("trustmanagers.operator.openshift.io object not found, skipping reconciliation", "request", req)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to fetch trustmanagers.operator.openshift.io %q during reconciliation: %w", req.NamespacedName, err)
	}

	if !trustManager.DeletionTimestamp.IsZero() {
		r.log.V(1).Info("trustmanagers.operator.openshift.io is marked for deletion", "name", req.NamespacedName)

		if err := r.cleanUp(trustManager); err != nil {
			return ctrl.Result{}, fmt.Errorf("clean up failed for %q trustmanagers.operator.openshift.io instance deletion: %w", req.NamespacedName, err)
		}

		if err := r.removeFinalizer(ctx, trustManager, finalizer); err != nil {
			return ctrl.Result{}, err
		}

		r.log.V(1).Info("removed finalizer, cleanup complete", "request", req.NamespacedName)
		return ctrl.Result{}, nil
	}

	// Set finalizers on the trustmanagers.operator.openshift.io resource
	if err := r.addFinalizer(ctx, trustManager); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update %q trustmanagers.operator.openshift.io with finalizers: %w", req.NamespacedName, err)
	}

	return r.processReconcileRequest(trustManager, req.NamespacedName)
}

func (r *Reconciler) processReconcileRequest(trustManager *v1alpha1.TrustManager, req types.NamespacedName) (ctrl.Result, error) {
	if err := r.reconcileTrustManagerDeployment(trustManager); err != nil {
		r.log.Error(err, "failed to reconcile TrustManager deployment", "request", req)

		// Set both conditions atomically before updating status
		degradedChanged := trustManager.Status.SetCondition(v1alpha1.Degraded, metav1.ConditionTrue, v1alpha1.ReasonFailed, fmt.Sprintf("reconciliation failed: %v", err))
		readyChanged := trustManager.Status.SetCondition(v1alpha1.Ready, metav1.ConditionFalse, v1alpha1.ReasonFailed, "")

		if degradedChanged || readyChanged {
			r.log.V(2).Info("updating trustmanager conditions on error",
				"name", trustManager.GetName(),
				"degradedChanged", degradedChanged,
				"readyChanged", readyChanged,
				"error", err)
			if updateErr := r.updateCondition(trustManager, err); updateErr != nil {
				return ctrl.Result{}, updateErr
			}
		}
		return ctrl.Result{RequeueAfter: defaultRequeueTime}, nil
	}

	// Update status with observed state
	r.updateObservedStatus(trustManager)

	// Set both conditions atomically before updating status on success
	degradedChanged := trustManager.Status.SetCondition(v1alpha1.Degraded, metav1.ConditionFalse, v1alpha1.ReasonReady, "")
	readyChanged := trustManager.Status.SetCondition(v1alpha1.Ready, metav1.ConditionTrue, v1alpha1.ReasonReady, "reconciliation successful")

	if degradedChanged || readyChanged {
		r.log.V(2).Info("updating trustmanager conditions on successful reconciliation",
			"name", trustManager.GetName(),
			"degradedChanged", degradedChanged,
			"readyChanged", readyChanged)
		if updateErr := r.updateCondition(trustManager, nil); updateErr != nil {
			return ctrl.Result{}, updateErr
		}
	}
	return ctrl.Result{}, nil
}

// cleanUp handles deletion of trustmanagers.operator.openshift.io gracefully.
// Per the EP Non-Goals: deleting the CR only stops reconciliation, it does not
// remove the trust-manager deployment or its associated resources.
func (r *Reconciler) cleanUp(trustManager *v1alpha1.TrustManager) error {
	r.eventRecorder.Eventf(trustManager, corev1.EventTypeWarning, "RemoveDeployment",
		"trustmanager %s marked for deletion, reconciliation of trust-manager resources will stop. "+
			"Resources created for trust-manager deployment will not be removed automatically.",
		trustManager.GetName())
	return nil
}
