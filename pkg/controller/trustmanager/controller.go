package trustmanager

import (
	"context"
	"fmt"
	"reflect"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
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

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

var (
	// requestEnqueueLabelKey is the label key name used for filtering reconcile
	// events to include only the resources created by the controller.
	requestEnqueueLabelKey = "app"

	// requestEnqueueLabelValue is the label value used for filtering reconcile
	// events to include only the resources created by the controller.
	requestEnqueueLabelValue = "trust-manager"
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
		if objLabels != nil {
			if objLabels[requestEnqueueLabelKey] == requestEnqueueLabelValue {
				return []reconcile.Request{
					{
						NamespacedName: types.NamespacedName{
							// TrustManager is cluster-scoped, so only Name is needed.
							Name: trustManagerObjectName,
						},
					},
				}
			}
		}

		r.log.V(4).Info("object not of interest, ignoring reconcile event", "object", fmt.Sprintf("%T", obj), "name", obj.GetName(), "namespace", obj.GetNamespace())
		return []reconcile.Request{}
	}

	// predicate function to ignore events for objects not managed by controller.
	controllerManagedResources := predicate.NewPredicateFuncs(func(object client.Object) bool {
		return object.GetLabels() != nil && object.GetLabels()[requestEnqueueLabelKey] == requestEnqueueLabelValue
	})

	withIgnoreStatusUpdatePredicates := builder.WithPredicates(predicate.GenerationChangedPredicate{}, controllerManagedResources)
	controllerManagedResourcePredicates := builder.WithPredicates(controllerManagedResources)

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.TrustManager{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Named(ControllerName).
		Watches(&appsv1.Deployment{}, handler.EnqueueRequestsFromMapFunc(mapFunc), withIgnoreStatusUpdatePredicates).
		Watches(&rbacv1.ClusterRole{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerManagedResourcePredicates).
		Watches(&rbacv1.ClusterRoleBinding{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerManagedResourcePredicates).
		Watches(&rbacv1.Role{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerManagedResourcePredicates).
		Watches(&rbacv1.RoleBinding{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerManagedResourcePredicates).
		Watches(&corev1.Service{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerManagedResourcePredicates).
		Watches(&corev1.ServiceAccount{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerManagedResourcePredicates).
		Complete(r)
}

// Reconcile function to compare the state specified by the TrustManager object against the actual cluster state,
// and to make the cluster state reflect the state specified by the user.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.log.V(1).Info("reconciling", "request", req)

	// Fetch the trustmanagers.operator.openshift.io CR
	tm := &v1alpha1.TrustManager{}
	if err := r.Get(ctx, req.NamespacedName, tm); err != nil {
		if errors.IsNotFound(err) {
			// NotFound errors, since they can't be fixed by an immediate
			// requeue (have to wait for a new notification), and can be processed
			// on deleted requests.
			r.log.V(1).Info("trustmanagers.operator.openshift.io object not found, skipping reconciliation", "request", req)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to fetch trustmanagers.operator.openshift.io %q during reconciliation: %w", req.NamespacedName, err)
	}

	if !tm.DeletionTimestamp.IsZero() {
		r.log.V(1).Info("trustmanagers.operator.openshift.io is marked for deletion", "name", req.NamespacedName)

		if requeue, err := r.cleanUp(tm); err != nil {
			return ctrl.Result{}, fmt.Errorf("clean up failed for %q trustmanagers.operator.openshift.io instance deletion: %w", req.NamespacedName, err)
		} else if requeue {
			return ctrl.Result{RequeueAfter: defaultRequeueTime}, nil
		}

		if err := r.removeFinalizer(ctx, tm, finalizer); err != nil {
			return ctrl.Result{}, err
		}

		r.log.V(1).Info("removed finalizer, cleanup complete", "request", req.NamespacedName)
		return ctrl.Result{}, nil
	}

	// Set finalizers on the trustmanagers.operator.openshift.io resource
	if err := r.addFinalizer(ctx, tm); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update %q trustmanagers.operator.openshift.io with finalizers: %w", req.NamespacedName, err)
	}

	return r.processReconcileRequest(tm, req.NamespacedName)
}

func (r *Reconciler) processReconcileRequest(tm *v1alpha1.TrustManager, req types.NamespacedName) (ctrl.Result, error) {
	trustManagerCreateRecon := false
	if !containsProcessedAnnotation(tm) && reflect.DeepEqual(tm.Status, v1alpha1.TrustManagerStatus{}) {
		r.log.V(1).Info("starting reconciliation of newly created trustmanager", "name", tm.GetName())
		trustManagerCreateRecon = true
	}

	var errUpdate error
	if err := r.reconcileTrustManagerDeployment(tm, trustManagerCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile TrustManager deployment", "request", req)
		if IsIrrecoverableError(err) {
			// Set both conditions atomically before updating status
			degradedChanged := tm.Status.SetCondition(v1alpha1.Degraded, metav1.ConditionTrue, v1alpha1.ReasonFailed, fmt.Sprintf("reconciliation failed with irrecoverable error not retrying: %v", err))
			readyChanged := tm.Status.SetCondition(v1alpha1.Ready, metav1.ConditionFalse, v1alpha1.ReasonReady, "")

			if degradedChanged || readyChanged {
				r.log.V(2).Info("updating trustmanager conditions on irrecoverable error",
					"name", tm.GetName(),
					"degradedChanged", degradedChanged,
					"readyChanged", readyChanged,
					"error", err)
				errUpdate = r.updateCondition(tm, nil)
			}
			return ctrl.Result{}, errUpdate
		}
		// Set both conditions atomically before updating status
		degradedChanged := tm.Status.SetCondition(v1alpha1.Degraded, metav1.ConditionFalse, v1alpha1.ReasonReady, "")
		readyChanged := tm.Status.SetCondition(v1alpha1.Ready, metav1.ConditionFalse, v1alpha1.ReasonInProgress, fmt.Sprintf("reconciliation failed, retrying: %v", err))

		if degradedChanged || readyChanged {
			r.log.V(2).Info("updating trustmanager conditions on recoverable error",
				"name", tm.GetName(),
				"degradedChanged", degradedChanged,
				"readyChanged", readyChanged,
				"error", err)
			errUpdate = r.updateCondition(tm, err)
		}
		// For recoverable errors, either requeue manually or return error, not both
		// If status update failed, return the update error; otherwise return the original error
		if errUpdate != nil {
			return ctrl.Result{}, errUpdate
		}
		return ctrl.Result{RequeueAfter: defaultRequeueTime}, nil
	}

	// Set both conditions atomically before updating status on success
	degradedChanged := tm.Status.SetCondition(v1alpha1.Degraded, metav1.ConditionFalse, v1alpha1.ReasonReady, "")
	readyChanged := tm.Status.SetCondition(v1alpha1.Ready, metav1.ConditionTrue, v1alpha1.ReasonReady, "reconciliation successful")

	if degradedChanged || readyChanged {
		r.log.V(2).Info("updating trustmanager conditions on successful reconciliation",
			"name", tm.GetName(),
			"degradedChanged", degradedChanged,
			"readyChanged", readyChanged)
		errUpdate = r.updateCondition(tm, nil)
	}
	return ctrl.Result{}, errUpdate
}

// cleanUp handles deletion of trustmanagers.operator.openshift.io gracefully.
func (r *Reconciler) cleanUp(tm *v1alpha1.TrustManager) (bool, error) {
	r.eventRecorder.Eventf(tm, corev1.EventTypeWarning, "RemoveDeployment", "%s trustmanager marked for deletion, removing all resources created for trust-manager deployment", tm.GetName())

	// Clean up cluster-scoped RBAC resources since they won't be garbage collected.
	if err := r.cleanUpClusterScopedResources(tm); err != nil {
		r.log.Error(err, "failed to clean up cluster-scoped resources")
		return true, err
	}

	return false, nil
}

// cleanUpClusterScopedResources removes ClusterRoles and ClusterRoleBindings created by the controller.
func (r *Reconciler) cleanUpClusterScopedResources(tm *v1alpha1.TrustManager) error {
	// Delete ClusterRoleBindings with matching labels
	clusterRoleBindingList := &rbacv1.ClusterRoleBindingList{}
	if err := r.List(r.ctx, clusterRoleBindingList, client.MatchingLabels(controllerDefaultResourceLabels)); err != nil {
		return fmt.Errorf("failed to list clusterrolebinding resources for cleanup: %w", err)
	}
	for i := range clusterRoleBindingList.Items {
		if err := r.Delete(r.ctx, &clusterRoleBindingList.Items[i]); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete clusterrolebinding %s: %w", clusterRoleBindingList.Items[i].GetName(), err)
		}
		r.log.V(1).Info("deleted clusterrolebinding during cleanup", "name", clusterRoleBindingList.Items[i].GetName())
	}

	// Delete ClusterRoles with matching labels
	clusterRoleList := &rbacv1.ClusterRoleList{}
	if err := r.List(r.ctx, clusterRoleList, client.MatchingLabels(controllerDefaultResourceLabels)); err != nil {
		return fmt.Errorf("failed to list clusterrole resources for cleanup: %w", err)
	}
	for i := range clusterRoleList.Items {
		if err := r.Delete(r.ctx, &clusterRoleList.Items[i]); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete clusterrole %s: %w", clusterRoleList.Items[i].GetName(), err)
		}
		r.log.V(1).Info("deleted clusterrole during cleanup", "name", clusterRoleList.Items[i].GetName())
	}

	return nil
}
