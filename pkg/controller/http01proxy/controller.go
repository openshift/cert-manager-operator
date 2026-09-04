package http01proxy

import (
	"context"
	"fmt"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/go-logr/logr"
	configv1 "github.com/openshift/api/config/v1"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
)

// Reconciler reconciles HTTP01Proxy objects and manages the nftables MachineConfig on baremetal clusters.
type Reconciler struct {
	common.CtrlClient

	eventRecorder record.EventRecorder
	log           logr.Logger

	cachedPlatform *platformInfo
	platformMu     sync.Mutex
}

// +kubebuilder:rbac:groups=operator.openshift.io,resources=http01proxies,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=operator.openshift.io,resources=http01proxies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.openshift.io,resources=http01proxies/finalizers,verbs=update
// +kubebuilder:rbac:groups=config.openshift.io,resources=infrastructures,verbs=get;list;watch
// +kubebuilder:rbac:groups=machineconfiguration.openshift.io,resources=machineconfigs,verbs=get;list;watch;create;update;patch;delete

func New(mgr ctrl.Manager) (*Reconciler, error) {
	c, err := common.NewClient(mgr)
	if err != nil {
		return nil, err
	}
	return &Reconciler{
		CtrlClient:    c,
		eventRecorder: mgr.GetEventRecorderFor(ControllerName),
		log:           ctrl.Log.WithName(ControllerName),
	}, nil
}

// SetupWithManager registers the controller with the manager and sets up watches for HTTP01Proxy and Infrastructure resources.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	infrastructureMapFunc := func(ctx context.Context, obj client.Object) []reconcile.Request {
		if obj.GetName() != "cluster" {
			return []reconcile.Request{}
		}
		r.platformMu.Lock()
		r.cachedPlatform = nil
		r.platformMu.Unlock()
		r.log.V(2).Info("infrastructure/cluster changed, invalidated platform cache")

		return []reconcile.Request{
			{
				NamespacedName: types.NamespacedName{
					Name:      http01proxyObjectName,
					Namespace: common.OperatorNamespace,
				},
			},
		}
	}

	builder := ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.HTTP01Proxy{}).
		Named(ControllerName)

	// Only watch Infrastructure if the CRD exists (MicroShift does not serve config.openshift.io).
	if _, err := mgr.GetRESTMapper().RESTMapping(infrastructureGVK.GroupKind(), infrastructureGVK.Version); err == nil {
		builder = builder.Watches(&configv1.Infrastructure{}, handler.EnqueueRequestsFromMapFunc(infrastructureMapFunc))
	} else {
		r.log.V(1).Info("Infrastructure CRD not available, skipping watch")
	}

	return builder.Complete(r)
}

// Reconcile handles a single reconciliation loop for an HTTP01Proxy resource.
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

func (r *Reconciler) cleanUp(ctx context.Context, proxy *v1alpha1.HTTP01Proxy) error {
	r.log.V(1).Info("cleaning up http01proxy resources", "namespace", proxy.GetNamespace(), "name", proxy.GetName())
	r.eventRecorder.Eventf(proxy, corev1.EventTypeNormal, "CleanUp", "cleaning up resources for http01proxy %s/%s", proxy.GetNamespace(), proxy.GetName())

	if err := r.deleteMachineConfig(ctx); err != nil {
		return fmt.Errorf("failed to delete MachineConfig: %w", err)
	}

	return nil
}
