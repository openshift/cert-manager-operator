package approverpolicy

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/features"
)

// Reconciler reconciles the cluster-scoped ApproverPolicyManager singleton.
type Reconciler struct {
	common.CtrlClient
	log logr.Logger
}

// +kubebuilder:rbac:groups=operator.openshift.io,resources=approverpolicymanagers,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=operator.openshift.io,resources=approverpolicymanagers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.openshift.io,resources=approverpolicymanagers/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=serviceaccounts;services;secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings;clusterroles;clusterrolebindings,verbs=get;list;watch
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=validatingwebhookconfigurations,verbs=get;list;watch

// New returns a reconciler using the manager's shared client.
func New(mgr ctrl.Manager) (*Reconciler, error) {
	c, err := common.NewClient(mgr)
	if err != nil {
		return nil, err
	}
	return &Reconciler{CtrlClient: c, log: ctrl.Log.WithName(ControllerName)}, nil
}

// SetupWithManager registers the singleton and managed-resource event sources.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	if !features.IsApproverPolicyManagerFeatureGateEnabled() {
		return nil
	}
	enqueue := handler.EnqueueRequestsFromMapFunc(func(context.Context, client.Object) []reconcile.Request {
		return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: approverPolicyManagerObjectName}}}
	})
	managed := builder.WithPredicates(managedResourcePredicate())
	return ctrl.NewControllerManagedBy(mgr).
		Named(ControllerName).
		For(&v1alpha1.ApproverPolicyManager{}, builder.WithPredicates(singletonPredicate())).
		Watches(&appsv1.Deployment{}, enqueue, managed).
		Watches(&corev1.ServiceAccount{}, enqueue, managed).
		Watches(&corev1.Service{}, enqueue, managed).
		Watches(&rbacv1.ClusterRole{}, enqueue, managed).
		Watches(&rbacv1.ClusterRoleBinding{}, enqueue, managed).
		Watches(&rbacv1.Role{}, enqueue, managed).
		Watches(&rbacv1.RoleBinding{}, enqueue, managed).
		Watches(&admissionregistrationv1.ValidatingWebhookConfiguration{}, enqueue, managed).
		// Match IstioCSR's metadata-only Secret watch. Do not filter the shared
		// Secret informer: IstioCSR also watches unlabeled user Secrets.
		WatchesMetadata(&corev1.Secret{}, enqueue, managed).
		Complete(r)
}

func singletonPredicate() predicate.Predicate {
	isSingleton := func(obj client.Object) bool {
		return obj != nil && obj.GetName() == approverPolicyManagerObjectName && obj.GetNamespace() == ""
	}
	return predicate.Funcs{
		CreateFunc:  func(e event.CreateEvent) bool { return isSingleton(e.Object) },
		DeleteFunc:  func(e event.DeleteEvent) bool { return isSingleton(e.Object) },
		GenericFunc: func(e event.GenericEvent) bool { return isSingleton(e.Object) },
		UpdateFunc: func(e event.UpdateEvent) bool {
			if e.ObjectOld == nil || !isSingleton(e.ObjectNew) {
				return false
			}
			return e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration() ||
				!reflect.DeepEqual(e.ObjectOld.GetLabels(), e.ObjectNew.GetLabels()) ||
				!reflect.DeepEqual(e.ObjectOld.GetAnnotations(), e.ObjectNew.GetAnnotations()) ||
				!reflect.DeepEqual(e.ObjectOld.GetFinalizers(), e.ObjectNew.GetFinalizers()) ||
				!e.ObjectOld.GetDeletionTimestamp().Equal(e.ObjectNew.GetDeletionTimestamp())
		},
	}
}

func managedResourcePredicate() predicate.Predicate {
	isManaged := func(obj client.Object) bool {
		return obj != nil && obj.GetLabels()[common.ManagedResourceLabelKey] == RequestEnqueueLabelValue
	}
	return predicate.Funcs{
		CreateFunc:  func(e event.CreateEvent) bool { return isManaged(e.Object) },
		DeleteFunc:  func(e event.DeleteEvent) bool { return isManaged(e.Object) },
		GenericFunc: func(e event.GenericEvent) bool { return isManaged(e.Object) },
		UpdateFunc: func(e event.UpdateEvent) bool {
			// Admit label removal and Deployment status changes as well as spec
			// changes, so later operand readiness checks can observe rollout.
			return isManaged(e.ObjectOld) || isManaged(e.ObjectNew)
		},
	}
}

// Reconcile bootstraps the singleton lifecycle. Operand installation is added
// separately; until then this controller must not signal operand readiness.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if !features.IsApproverPolicyManagerFeatureGateEnabled() ||
		req.Name != approverPolicyManagerObjectName || req.Namespace != "" {
		return ctrl.Result{}, nil
	}

	instance := &v1alpha1.ApproverPolicyManager{}
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to fetch ApproverPolicyManager %q: %w", req.Name, err)
	}

	if !instance.DeletionTimestamp.IsZero() {
		// This scaffold creates no operand resources. Before introducing resource
		// installation, add cleanup here and wait for completion before releasing
		// the finalizer: CR disappearance will allow the built-in approver to resume.
		if err := r.updateFinalizer(ctx, instance, false); err != nil {
			return r.handleReconcileResult(ctx, instance, common.FromClientError(err, "failed to remove finalizer"))
		}
		return ctrl.Result{}, nil
	}

	if err := r.updateFinalizer(ctx, instance, true); err != nil {
		return r.handleReconcileResult(ctx, instance, common.FromClientError(err, "failed to add finalizer"))
	}
	// The fresh read used for the finalizer may have observed deletion beginning.
	if !instance.DeletionTimestamp.IsZero() {
		return ctrl.Result{RequeueAfter: defaultRequeueTime}, nil
	}
	return r.processReconcileRequest(ctx, instance)
}

func (r *Reconciler) processReconcileRequest(ctx context.Context, instance *v1alpha1.ApproverPolicyManager) (ctrl.Result, error) {
	// Replace this pending result with operand reconciliation and an observed
	// readiness check when installation is implemented. A nil result here would
	// publish Ready=True and prematurely permit the future approval handoff.
	err := errors.New("approver-policy operand installation is pending")
	return r.handleReconcileResult(ctx, instance, common.NewRetryRequiredError(err, "waiting for approver-policy"))
}

func (r *Reconciler) handleReconcileResult(ctx context.Context, instance *v1alpha1.ApproverPolicyManager, reconcileErr error) (ctrl.Result, error) {
	// Calculate desired conditions independently of the cached status. The status
	// writer compares against the latest object, including message and generation
	// changes, and preserves conditions owned by other writers.
	desired := v1alpha1.ConditionalStatus{}
	return common.HandleReconcileResult(&desired, reconcileErr, r.log.WithValues("name", instance.Name),
		func(prependErr error) error {
			if err := r.updateStatus(ctx, instance, desired); err != nil {
				return errors.Join(prependErr, err)
			}
			return nil
		}, defaultRequeueTime)
}
