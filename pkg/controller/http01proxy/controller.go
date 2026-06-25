package http01proxy

import (
	"context"
	"fmt"
	"os"
	"sync"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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

// RequestEnqueueLabelValue is the label value used for filtering reconcile events.
const RequestEnqueueLabelValue = http01proxyCommonName

// Reconciler reconciles an HTTP01Proxy object.
type Reconciler struct {
	common.CtrlClient

	eventRecorder record.EventRecorder
	log           logr.Logger

	proxyImage string

	cachedPlatform *platformInfo
	platformMu     sync.Mutex
}

// +kubebuilder:rbac:groups=operator.openshift.io,resources=http01proxies,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=operator.openshift.io,resources=http01proxies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.openshift.io,resources=http01proxies/finalizers,verbs=update
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=config.openshift.io,resources=clusterversions;ingresses;infrastructures,verbs=get;list;watch
// +kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,resourceNames=privileged,verbs=use

// New returns a new Reconciler instance.
func New(mgr ctrl.Manager) (*Reconciler, error) {
	c, err := common.NewClient(mgr)
	if err != nil {
		return nil, err
	}
	return &Reconciler{
		CtrlClient:    c,
		eventRecorder: mgr.GetEventRecorderFor(ControllerName),
		log:           ctrl.Log.WithName(ControllerName),
		proxyImage:    os.Getenv(http01proxyImageNameEnvVarName),
	}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	mapFunc := func(ctx context.Context, obj client.Object) []reconcile.Request {
		r.log.V(4).Info("received reconcile event", "object", fmt.Sprintf("%T", obj), "name", obj.GetName(), "namespace", obj.GetNamespace())

		objLabels := obj.GetLabels()
		if objLabels != nil && objLabels[common.ManagedResourceLabelKey] == RequestEnqueueLabelValue {
			namespace := obj.GetNamespace()
			if namespace == "" {
				namespace = common.OperatorNamespace
			}
			return []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{
						Name:      http01proxyObjectName,
						Namespace: namespace,
					},
				},
			}
		}

		r.log.V(4).Info("object not of interest, ignoring", "object", fmt.Sprintf("%T", obj), "name", obj.GetName())
		return []reconcile.Request{}
	}

	controllerManagedResources := predicate.NewPredicateFuncs(func(object client.Object) bool {
		return object.GetLabels() != nil && object.GetLabels()[common.ManagedResourceLabelKey] == RequestEnqueueLabelValue
	})

	controllerManagedResourcePredicates := builder.WithPredicates(controllerManagedResources)
	withIgnoreStatusUpdatePredicates := builder.WithPredicates(predicate.GenerationChangedPredicate{}, controllerManagedResources)

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.HTTP01Proxy{}).
		Named(ControllerName).
		Watches(&appsv1.DaemonSet{}, handler.EnqueueRequestsFromMapFunc(mapFunc), withIgnoreStatusUpdatePredicates).
		Watches(&rbacv1.ClusterRole{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerManagedResourcePredicates).
		Watches(&rbacv1.ClusterRoleBinding{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerManagedResourcePredicates).
		Watches(&corev1.ServiceAccount{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerManagedResourcePredicates).
		Watches(&networkingv1.NetworkPolicy{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerManagedResourcePredicates).
		Complete(r)
}

// Reconcile compares the state specified by the HTTP01Proxy object against the actual cluster state.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.log.V(1).Info("reconciling", "request", req)

	if req.Namespace != common.OperatorNamespace {
		r.log.V(1).Info("ignoring http01proxy in unexpected namespace", "namespace", req.Namespace, "expected", common.OperatorNamespace)
		return ctrl.Result{}, nil
	}

	proxy := &v1alpha1.HTTP01Proxy{}
	if err := r.Get(ctx, req.NamespacedName, proxy); err != nil {
		if errors.IsNotFound(err) {
			r.log.V(1).Info("http01proxy object not found, skipping reconciliation", "request", req)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to fetch http01proxy %q during reconciliation: %w", req.NamespacedName, err)
	}

	if !proxy.DeletionTimestamp.IsZero() {
		r.log.V(1).Info("http01proxy is marked for deletion", "namespace", req.NamespacedName)

		if err := r.cleanUp(ctx, proxy); err != nil {
			return ctrl.Result{}, fmt.Errorf("clean up failed for %q http01proxy deletion: %w", req.NamespacedName, err)
		}

		if err := r.removeFinalizer(ctx, proxy); err != nil {
			return ctrl.Result{}, err
		}

		r.log.V(1).Info("removed finalizer, cleanup complete", "request", req.NamespacedName)
		return ctrl.Result{}, nil
	}

	if err := r.addFinalizer(ctx, proxy); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update %q http01proxy with finalizers: %w", req.NamespacedName, err)
	}

	return r.processReconcileRequest(ctx, proxy, req.NamespacedName)
}

func (r *Reconciler) processReconcileRequest(ctx context.Context, proxy *v1alpha1.HTTP01Proxy, req types.NamespacedName) (ctrl.Result, error) {
	if !common.ContainsAnnotation(proxy, controllerProcessedAnnotation) && len(proxy.Status.Conditions) == 0 {
		r.log.V(1).Info("starting reconciliation of newly created http01proxy", "namespace", proxy.GetNamespace(), "name", proxy.GetName())
	}

	reconcileErr := r.reconcileHTTP01ProxyDeployment(ctx, proxy)
	if reconcileErr != nil {
		r.log.Error(reconcileErr, "failed to reconcile HTTP01Proxy deployment", "request", req)
	}

	return common.HandleReconcileResult(
		&proxy.Status.ConditionalStatus,
		reconcileErr,
		r.log.WithValues("namespace", proxy.GetNamespace(), "name", proxy.GetName()),
		func(prependErr error) error {
			return r.updateCondition(ctx, proxy, prependErr)
		},
		defaultRequeueTime,
	)
}

// cleanUp handles deletion of http01proxy gracefully.
func (r *Reconciler) cleanUp(ctx context.Context, proxy *v1alpha1.HTTP01Proxy) error {
	r.log.V(1).Info("cleaning up http01proxy resources", "namespace", proxy.GetNamespace(), "name", proxy.GetName())
	r.eventRecorder.Eventf(proxy, corev1.EventTypeNormal, "CleanUp", "cleaning up resources for http01proxy %s/%s", proxy.GetNamespace(), proxy.GetName())

	if err := r.deleteDaemonSet(ctx, proxy); err != nil {
		return fmt.Errorf("failed to delete daemonset: %w", err)
	}
	if err := r.deleteServiceAccount(ctx, proxy); err != nil {
		return fmt.Errorf("failed to delete serviceaccount: %w", err)
	}
	if err := r.deleteRBACResources(ctx); err != nil {
		return fmt.Errorf("failed to delete rbac resources: %w", err)
	}
	if err := r.deleteNetworkPolicies(ctx, proxy); err != nil {
		return fmt.Errorf("failed to delete network policies: %w", err)
	}

	return nil
}
