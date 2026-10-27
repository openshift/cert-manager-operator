package approverpolicy

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
)

// Reconciler reconciles the cluster-scoped ApproverPolicyManager singleton.
type Reconciler struct {
	common.CtrlClient
	log           logr.Logger
	apiReader     client.Reader // Confirms cache misses and resource deletion.
	eventRecorder record.EventRecorder
}

// +kubebuilder:rbac:groups=operator.openshift.io,resources=approverpolicymanagers,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=operator.openshift.io,resources=approverpolicymanagers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.openshift.io,resources=approverpolicymanagers/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=serviceaccounts;services;secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings;clusterroles;clusterrolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=validatingwebhookconfigurations,verbs=get;list;watch;create;update;patch;delete
// The operator must hold the operand's permissions to create its ClusterRole
// without bypassing Kubernetes RBAC escalation checks.
// +kubebuilder:rbac:groups=policy.cert-manager.io,resources=certificaterequestpolicies,verbs=list;watch
// +kubebuilder:rbac:groups=policy.cert-manager.io,resources=certificaterequestpolicies/status,verbs=patch
// +kubebuilder:rbac:groups=cert-manager.io,resources=signers,verbs=approve
// +kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=create

// New returns a reconciler using the manager's shared client.
func New(mgr ctrl.Manager) (*Reconciler, error) {
	c, err := common.NewClient(mgr)
	if err != nil {
		return nil, err
	}
	return &Reconciler{
		CtrlClient:    c,
		log:           ctrl.Log.WithName(ControllerName),
		apiReader:     mgr.GetAPIReader(),
		eventRecorder: mgr.GetEventRecorderFor(ControllerName),
	}, nil
}

// SetupWithManager registers the singleton and managed-resource event sources.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	enqueue := handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []reconcile.Request {
		r.log.V(4).Info("received reconcile event", "object", fmt.Sprintf("%T", obj), "name", obj.GetName(), "namespace", obj.GetNamespace())
		return []reconcile.Request{
			{NamespacedName: types.NamespacedName{Name: approverPolicyManagerObjectName}},
		}
	})

	managed := builder.WithPredicates(r.managedResourcePredicate())

	return ctrl.NewControllerManagedBy(mgr).
		Named(ControllerName).
		For(&v1alpha1.ApproverPolicyManager{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&appsv1.Deployment{}, enqueue, builder.WithPredicates(r.deploymentPredicate())).
		Watches(&corev1.ServiceAccount{}, enqueue, managed).
		Watches(&corev1.Service{}, enqueue, managed).
		Watches(&rbacv1.ClusterRole{}, enqueue, managed).
		Watches(&rbacv1.ClusterRoleBinding{}, enqueue, managed).
		Watches(&rbacv1.Role{}, enqueue, managed).
		Watches(&rbacv1.RoleBinding{}, enqueue, managed).
		Watches(&admissionregistrationv1.ValidatingWebhookConfiguration{}, enqueue, managed).
		Watches(&corev1.Secret{}, enqueue, managed).
		Complete(r)
}

func (r *Reconciler) managedResourcePredicate() predicate.Predicate {
	isManaged := func(obj client.Object) bool {
		if obj == nil {
			return false
		}
		matches := obj.GetLabels()[common.ManagedResourceLabelKey] == RequestEnqueueLabelValue
		r.log.V(4).Info("predicate evaluation", "object", fmt.Sprintf("%T", obj), "name", obj.GetName(), "namespace", obj.GetNamespace(), "labels", obj.GetLabels(), "matches", matches)
		return matches
	}
	return predicate.Funcs{
		CreateFunc:  func(e event.CreateEvent) bool { return isManaged(e.Object) },
		DeleteFunc:  func(e event.DeleteEvent) bool { return isManaged(e.Object) },
		GenericFunc: func(e event.GenericEvent) bool { return isManaged(e.Object) },
		UpdateFunc: func(e event.UpdateEvent) bool {
			// Check both versions so removing the managed label still reconciles.
			return isManaged(e.ObjectOld) || isManaged(e.ObjectNew)
		},
	}
}

// deploymentPredicate observes spec and metadata drift, but ignores rollout status churn.
func (r *Reconciler) deploymentPredicate() predicate.Predicate {
	return predicate.And(
		predicate.Or(
			predicate.GenerationChangedPredicate{},
			predicate.LabelChangedPredicate{},
			predicate.AnnotationChangedPredicate{},
		),
		r.managedResourcePredicate(),
	)
}

// Reconcile manages the singleton's operand installation and deletion lifecycle.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.log.V(1).Info("reconciling", "request", req)

	instance := &v1alpha1.ApproverPolicyManager{}
	if err := r.Get(ctx, req.NamespacedName, instance); err != nil {
		if apierrors.IsNotFound(err) {
			r.log.V(1).Info("ApproverPolicyManager object not found, skipping reconciliation", "request", req)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to fetch ApproverPolicyManager %q: %w", req.Name, err)
	}

	if !instance.DeletionTimestamp.IsZero() {
		r.log.V(1).Info("ApproverPolicyManager is marked for deletion", "name", instance.Name)
		if !controllerutil.ContainsFinalizer(instance, finalizer) {
			return ctrl.Result{}, nil
		}
		// Clean up the ApproverPolicyManager object.
		pending, err := r.cleanUp(ctx, instance)
		if err != nil {
			// Cleanup failures always retry.
			return r.handleReconcileResult(ctx, instance, common.NewRetryRequiredError(err, "approver-policy cleanup failed"))
		}
		if pending {
			return r.handleReconcileResult(ctx, instance, common.NewRetryRequiredError(errors.New("managed resources are terminating"), "waiting for approver-policy cleanup"))
		}

		// Remove finalizer from the ApproverPolicyManager object.
		if err := r.updateFinalizer(ctx, instance, false); err != nil {
			r.log.Error(err, "failed to remove finalizer", "name", instance.Name)
			return r.handleReconcileResult(ctx, instance, common.NewRetryRequiredError(err, "failed to remove finalizer"))
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer to the ApproverPolicyManager object.
	if err := r.updateFinalizer(ctx, instance, true); err != nil {
		r.log.Error(err, "failed to add finalizer", "name", instance.Name)
		return r.handleReconcileResult(ctx, instance, common.FromClientError(err, "failed to add finalizer"))
	}

	// updateFinalizer may observe deletion on a later cache read and copy it
	// into instance without adding our finalizer. Do not install in that case.
	if !instance.DeletionTimestamp.IsZero() {
		r.log.V(1).Info("ApproverPolicyManager deletion started during finalizer initialization, retrying", "name", instance.Name)
		return ctrl.Result{RequeueAfter: defaultRequeueTime}, nil
	}
	return r.processReconcileRequest(ctx, instance)
}

func (r *Reconciler) processReconcileRequest(ctx context.Context, instance *v1alpha1.ApproverPolicyManager) (ctrl.Result, error) {
	reconcileErr := r.reconcileApproverPolicy(ctx, instance)
	return r.handleReconcileResult(ctx, instance, reconcileErr)
}

func (r *Reconciler) handleReconcileResult(ctx context.Context, instance *v1alpha1.ApproverPolicyManager, reconcileErr error) (ctrl.Result, error) {
	// Calculate desired conditions independently of the cached status. The status
	// writer compares against the latest object, including message and generation
	// changes, and preserves conditions owned by other writers.
	desired := v1alpha1.ConditionalStatus{}
	// The helper computes conditions from an empty status on each pass. Log in
	// updateStatus only after a persisted change, avoiding repeated update logs.
	return common.HandleReconcileResult(
		&desired,
		reconcileErr,
		r.log,
		func(prependErr error) error {
			if err := r.updateStatus(ctx, instance, desired); err != nil {
				return errors.Join(prependErr, err)
			}
			return nil
		}, defaultRequeueTime)
}
