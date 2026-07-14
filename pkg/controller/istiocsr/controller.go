package istiocsr

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
)

// RequestEnqueueLabelValue is the label value used for filtering reconcile
// events to include only the resources created by the IstioCSR controller.
// The label key is common.ManagedResourceLabelKey.
const RequestEnqueueLabelValue = "cert-manager-istio-csr"

// Reconciler reconciles a IstioCSR object.
type Reconciler struct {
	common.CtrlClient

	ctx           context.Context
	eventRecorder record.EventRecorder
	log           logr.Logger
	scheme        *runtime.Scheme
}

// +kubebuilder:rbac:groups=operator.openshift.io,resources=istiocsrs,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=operator.openshift.io,resources=istiocsrs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.openshift.io,resources=istiocsrs/finalizers,verbs=update
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete

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
	mapFunc := func(_ context.Context, obj client.Object) []reconcile.Request {
		r.log.V(4).Info("received reconcile event", "object", fmt.Sprintf("%T", obj), "name", obj.GetName(), "namespace", obj.GetNamespace())

		objLabels := obj.GetLabels()
		if objLabels != nil {
			// will look for custom label set on objects not created in istiocsr namespace, and if it exists,
			// namespace in the reconcile request will be set same, else since label check matches is an object
			// created by controller, and we safely assume, it's in the istiocsr namespace.
			namespace := objLabels[istiocsrNamespaceMappingLabelName]
			if namespace == "" {
				namespace = obj.GetNamespace()
			}

			labelOk := func() bool {
				if objLabels[common.ManagedResourceLabelKey] == RequestEnqueueLabelValue {
					return true
				}
				value := objLabels[IstiocsrResourceWatchLabelName]
				if value == "" {
					return false
				}
				key := strings.Split(value, "_")
				if len(key) != 2 {
					//nolint:err113 // error used for logging with dynamic context
					r.log.Error(fmt.Errorf("invalid label format"), "%s label value(%s) not in expected format on %s resource", IstiocsrResourceWatchLabelName, value, obj.GetName())
					return false
				}
				namespace = key[0]
				return true
			}

			if labelOk() && namespace != "" {
				return []reconcile.Request{
					{
						NamespacedName: types.NamespacedName{
							Name:      istiocsrObjectName,
							Namespace: namespace,
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
		return object.GetLabels() != nil && object.GetLabels()[common.ManagedResourceLabelKey] == RequestEnqueueLabelValue
	})

	// predicate function to filter events for objects which controller is interested in, but
	// not managed or created by controller.
	controllerWatchResources := predicate.NewPredicateFuncs(func(object client.Object) bool {
		return object.GetLabels() != nil && object.GetLabels()[IstiocsrResourceWatchLabelName] != ""
	})

	controllerConfigMapPredicates := predicate.NewPredicateFuncs(func(object client.Object) bool {
		if object.GetLabels() == nil {
			return false
		}
		// Accept if it's a managed ConfigMap OR a watched ConfigMap
		return object.GetLabels()[common.ManagedResourceLabelKey] == RequestEnqueueLabelValue ||
			object.GetLabels()[IstiocsrResourceWatchLabelName] != ""
	})

	withIgnoreStatusUpdatePredicates := builder.WithPredicates(predicate.GenerationChangedPredicate{}, controllerManagedResources)
	controllerWatchResourcePredicates := builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}, controllerWatchResources)
	controllerManagedResourcePredicates := builder.WithPredicates(controllerManagedResources)
	controllerConfigMapWatchPredicates := builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}, controllerConfigMapPredicates)

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.IstioCSR{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Named(ControllerName).
		Watches(&certmanagerv1.Certificate{}, handler.EnqueueRequestsFromMapFunc(mapFunc), withIgnoreStatusUpdatePredicates).
		Watches(&appsv1.Deployment{}, handler.EnqueueRequestsFromMapFunc(mapFunc), withIgnoreStatusUpdatePredicates).
		Watches(&rbacv1.ClusterRole{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerManagedResourcePredicates).
		Watches(&rbacv1.ClusterRoleBinding{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerManagedResourcePredicates).
		Watches(&rbacv1.Role{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerManagedResourcePredicates).
		Watches(&rbacv1.RoleBinding{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerManagedResourcePredicates).
		Watches(&corev1.Service{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerManagedResourcePredicates).
		Watches(&corev1.ServiceAccount{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerManagedResourcePredicates).
		Watches(&corev1.ConfigMap{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerConfigMapWatchPredicates).
		WatchesMetadata(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerWatchResourcePredicates).
		Watches(&networkingv1.NetworkPolicy{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerManagedResourcePredicates).
		Watches(&certmanagerv1.Issuer{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerWatchResourcePredicates).
		Watches(&certmanagerv1.ClusterIssuer{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerWatchResourcePredicates).
		Complete(r)
}

// Reconcile function to compare the state specified by the IstioCSR object against the actual cluster state,
// and to make the cluster state reflect the state specified by the user.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.log.V(1).Info("reconciling", "request", req)

	// Fetch the istiocsr.openshift.operator.io CR
	istiocsr := &v1alpha1.IstioCSR{}
	if err := r.Get(ctx, req.NamespacedName, istiocsr); err != nil {
		if errors.IsNotFound(err) {
			// NotFound errors, since they can't be fixed by an immediate
			// requeue (have to wait for a new notification), and can be processed
			// on deleted requests.
			r.log.V(1).Info("istiocsr.openshift.operator.io object not found, skipping reconciliation", "request", req)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to fetch istiocsr.openshift.operator.io %q during reconciliation: %w", req.NamespacedName, err)
	}

	if !istiocsr.DeletionTimestamp.IsZero() {
		r.log.V(1).Info("istiocsr.openshift.operator.io is marked for deletion", "namespace", req.NamespacedName)

		if requeue, err := r.cleanUp(istiocsr); err != nil {
			return ctrl.Result{}, fmt.Errorf("clean up failed for %q istiocsr.openshift.operator.io instance deletion: %w", req.NamespacedName, err)
		} else if requeue {
			return ctrl.Result{RequeueAfter: defaultRequeueTime}, nil
		}

		if err := r.removeFinalizer(ctx, istiocsr, finalizer); err != nil {
			return ctrl.Result{}, err
		}

		r.log.V(1).Info("removed finalizer, cleanup complete", "request", req.NamespacedName)
		return ctrl.Result{}, nil
	}

	// Set finalizers on the istiocsr.openshift.operator.io resource
	if err := r.addFinalizer(ctx, istiocsr); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update %q istiocsr.openshift.operator.io with finalizers: %w", req.NamespacedName, err)
	}

	return r.processReconcileRequest(istiocsr, req.NamespacedName)
}

func (r *Reconciler) processReconcileRequest(istiocsr *v1alpha1.IstioCSR, req types.NamespacedName) (ctrl.Result, error) {
	istioCSRCreateRecon := false
	if !containsProcessedAnnotation(istiocsr) && reflect.DeepEqual(istiocsr.Status, v1alpha1.IstioCSRStatus{}) {
		r.log.V(1).Info("starting reconciliation of newly created istiocsr", "namespace", istiocsr.GetNamespace(), "name", istiocsr.GetName())
		istioCSRCreateRecon = true
	}

	if err := r.disallowMultipleIstioCSRInstances(istiocsr); err != nil {
		if common.IsMultipleInstanceError(err) {
			r.eventRecorder.Eventf(istiocsr, corev1.EventTypeWarning, "MultiIstioCSRInstance", "creation of multiple istiocsr instances is not supported, will not be processed")
			err = nil
		}
		return ctrl.Result{}, err
	}

	reconcileErr := r.reconcileIstioCSRDeployment(istiocsr, istioCSRCreateRecon)
	if reconcileErr != nil {
		r.log.Error(reconcileErr, "failed to reconcile IstioCSR deployment", "request", req)
	}

	return common.HandleReconcileResult(
		&istiocsr.Status.ConditionalStatus,
		reconcileErr,
		r.log.WithValues("namespace", istiocsr.GetNamespace(), "name", istiocsr.GetName()),
		func(prependErr error) error {
			return r.updateCondition(istiocsr, prependErr)
		},
		defaultRequeueTime,
	)
}

// cleanUp handles deletion of istiocsr.openshift.operator.io gracefully.
//
//nolint:unparam // error return is kept for future implementation
func (r *Reconciler) cleanUp(istiocsr *v1alpha1.IstioCSR) (bool, error) {
	// TODO: For GA, handle cleaning up of resources created for installing istio-csr operand.
	// This might require a validation webhook to check for usage of service as GRPC endpoint in
	// any of OpenShift Service Mesh or Istiod deployments to avoid disruptions across cluster.
	r.eventRecorder.Eventf(istiocsr, corev1.EventTypeWarning, "RemoveDeployment", "%s/%s istiocsr marked for deletion, remove reference in istiod deployment and remove all resources created for istiocsr deployment", istiocsr.GetNamespace(), istiocsr.GetName())
	return false, nil
}
