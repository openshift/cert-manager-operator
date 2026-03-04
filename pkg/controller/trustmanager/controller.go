package trustmanager

import (
	"context"
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/go-logr/logr"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
)

// RequestEnqueueLabelValue is the label value used for filtering reconcile
// events to include only the resources created by the TrustManager controller.
// The label key is common.ManagedResourceLabelKey.
const RequestEnqueueLabelValue = "cert-manager-trust-manager"

// Reconciler reconciles a TrustManager object.
type Reconciler struct {
	common.CtrlClient

	ctx           context.Context
	eventRecorder record.EventRecorder
	log           logr.Logger
	scheme        *runtime.Scheme
}

// TODO: Add more RBAC rules as resources are implemented
// +kubebuilder:rbac:groups=operator.openshift.io,resources=trustmanagers,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=operator.openshift.io,resources=trustmanagers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.openshift.io,resources=trustmanagers/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch

// New returns a new Reconciler instance.
func New(mgr ctrl.Manager) (*Reconciler, error) {
	c, err := common.NewClient(mgr)
	if err != nil {
		return nil, err
	}
	return &Reconciler{
		CtrlClient:    c,
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
			if objLabels[common.ManagedResourceLabelKey] == RequestEnqueueLabelValue {
				return []reconcile.Request{
					{
						NamespacedName: types.NamespacedName{
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
		labels := object.GetLabels()
		matches := labels != nil && labels[common.ManagedResourceLabelKey] == RequestEnqueueLabelValue
		r.log.V(4).Info("predicate evaluation", "object", fmt.Sprintf("%T", object), "name", object.GetName(), "namespace", object.GetNamespace(), "labels", labels, "matches", matches)
		return matches
	})

	controllerManagedResourcePredicates := builder.WithPredicates(controllerManagedResources)

	// TODO: Add more watches as resources are implemented
	return ctrl.NewControllerManagedBy(mgr).
		// GenerationChangedPredicate ignores status-only updates
		For(&v1alpha1.TrustManager{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Named(ControllerName).
		Watches(&corev1.ServiceAccount{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerManagedResourcePredicates).
		Complete(r)
}

// Reconcile function to compare the state specified by the TrustManager object against the actual cluster state,
// and to make the cluster state reflect the state specified by the user.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.log.V(1).Info("reconciling", "request", req)

	// Fetch the trustmanager.openshift.operator.io CR
	trustManager := &v1alpha1.TrustManager{}
	// Note: No namespace because TrustManager is cluster-scoped
	if err := r.Get(ctx, types.NamespacedName{Name: req.Name}, trustManager); err != nil {
		if errors.IsNotFound(err) {
			// NotFound errors, since they can't be fixed by an immediate
			// requeue (have to wait for a new notification), and can be processed
			// on deleted requests.
			r.log.V(1).Info("trustmanager.openshift.operator.io object not found, skipping reconciliation", "request", req)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to fetch trustmanager.openshift.operator.io %q during reconciliation: %w", req.NamespacedName, err)
	}

	if !trustManager.DeletionTimestamp.IsZero() {
		r.log.V(1).Info("trustmanager.openshift.operator.io is marked for deletion", "name", req.NamespacedName)

		if requeue, err := r.cleanUp(trustManager); err != nil {
			return ctrl.Result{}, fmt.Errorf("clean up failed for %q trustmanager.openshift.operator.io instance deletion: %w", req.NamespacedName, err)
		} else if requeue {
			return ctrl.Result{RequeueAfter: defaultRequeueTime}, nil
		}

		if err := r.removeFinalizer(ctx, trustManager, finalizer); err != nil {
			return ctrl.Result{}, err
		}

		r.log.V(1).Info("removed finalizer, cleanup complete", "request", req.NamespacedName)
		return ctrl.Result{}, nil
	}

	// Set finalizers on the trustmanager.openshift.operator.io resource
	if err := r.addFinalizer(ctx, trustManager); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update %q trustmanager.openshift.operator.io with finalizers: %w", req.NamespacedName, err)
	}

	return r.processReconcileRequest(trustManager, req.NamespacedName)
}

func (r *Reconciler) processReconcileRequest(trustManager *v1alpha1.TrustManager, req types.NamespacedName) (ctrl.Result, error) {
	trustManagerCreateRecon := false
	if !containsProcessedAnnotation(trustManager) && reflect.DeepEqual(trustManager.Status, v1alpha1.TrustManagerStatus{}) {
		r.log.V(1).Info("starting reconciliation of newly created trustmanager", "name", trustManager.GetName())
		trustManagerCreateRecon = true
	}

	var errUpdate error = nil
	if err := r.reconcileTrustManagerDeployment(trustManager, trustManagerCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile TrustManager deployment", "request", req)
		if common.IsIrrecoverableError(err) {
			// Permanent failure - don't retry
			// Set Degraded=True, Ready=False
			// Set both conditions atomically before updating status
			degradedChanged := trustManager.Status.SetCondition(v1alpha1.Degraded, metav1.ConditionTrue, v1alpha1.ReasonFailed, fmt.Sprintf("reconciliation failed with irrecoverable error not retrying: %v", err))
			readyChanged := trustManager.Status.SetCondition(v1alpha1.Ready, metav1.ConditionFalse, v1alpha1.ReasonFailed, "")

			if degradedChanged || readyChanged {
				r.log.V(2).Info("updating trustmanager conditions on irrecoverable error",
					"name", trustManager.GetName(),
					"degradedChanged", degradedChanged,
					"readyChanged", readyChanged,
					"error", err)
				errUpdate = r.updateCondition(trustManager, nil)
			}
			return ctrl.Result{}, errUpdate
		} else {
			// Temporary failure - retry after delay
			// Set Degraded=False, Ready=False with "in progress" message
			// Set both conditions atomically before updating status
			degradedChanged := trustManager.Status.SetCondition(v1alpha1.Degraded, metav1.ConditionFalse, v1alpha1.ReasonReady, "")
			readyChanged := trustManager.Status.SetCondition(v1alpha1.Ready, metav1.ConditionFalse, v1alpha1.ReasonInProgress, fmt.Sprintf("reconciliation failed, retrying: %v", err))

			if degradedChanged || readyChanged {
				r.log.V(2).Info("updating trustmanager conditions on recoverable error",
					"name", trustManager.GetName(),
					"degradedChanged", degradedChanged,
					"readyChanged", readyChanged,
					"error", err)
				errUpdate = r.updateCondition(trustManager, err)
			}
			// For recoverable errors, either requeue manually or return error, not both
			// If status update failed, return the update error; otherwise return the original error
			if errUpdate != nil {
				return ctrl.Result{}, errUpdate
			}
			return ctrl.Result{RequeueAfter: defaultRequeueTime}, nil
		}
	}

	// Success - update status
	// Set both conditions atomically before updating status on success
	degradedChanged := trustManager.Status.SetCondition(v1alpha1.Degraded, metav1.ConditionFalse, v1alpha1.ReasonReady, "")
	readyChanged := trustManager.Status.SetCondition(v1alpha1.Ready, metav1.ConditionTrue, v1alpha1.ReasonReady, "reconciliation successful")

	if degradedChanged || readyChanged {
		r.log.V(2).Info("updating trustmanager conditions on successful reconciliation",
			"name", trustManager.GetName(),
			"degradedChanged", degradedChanged,
			"readyChanged", readyChanged)
		errUpdate = r.updateCondition(trustManager, nil)
	}
	return ctrl.Result{}, errUpdate
}

// cleanUp handles deletion of trustmanager.openshift.operator.io gracefully.
func (r *Reconciler) cleanUp(trustManager *v1alpha1.TrustManager) (bool, error) {
	// TODO: For GA, handle cleaning up of resources created for installing trust-manager operand.
	// As per Non-Goals in the enhancement, removing the TrustManager CR will not remove the
	// trust-manager deployment or its associated resources.
	r.eventRecorder.Eventf(trustManager, corev1.EventTypeWarning, "RemoveDeployment", "%s trustmanager marked for deletion, remove all resources created for trustmanager deployment manually", trustManager.GetName())
	return false, nil
}
