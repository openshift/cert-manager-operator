package istiocsr

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
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
	requestEnqueueLabelValue = "cert-manager-istio-csr"
)

// Reconciler reconciles a IstioCSR object
type Reconciler struct {
	ctrlClient

	ctx           context.Context
	eventRecorder record.EventRecorder
	log           logr.Logger
	scheme        *runtime.Scheme
}

// +kubebuilder:rbac:groups=operator.openshift.io,resources=istiocsrs,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=operator.openshift.io,resources=istiocsrs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operator.openshift.io,resources=istiocsrs/finalizers,verbs=update

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

func BuildCustomClient(mgr ctrl.Manager) (client.Client, error) {
	managedResourceLabelReq, _ := labels.NewRequirement(requestEnqueueLabelKey, selection.Equals, []string{requestEnqueueLabelValue})
	managedResourceLabelReqSelector := labels.NewSelector().Add(*managedResourceLabelReq)

	customCacheOpts := cache.Options{
		HTTPClient: mgr.GetHTTPClient(),
		Scheme:     mgr.GetScheme(),
		Mapper:     mgr.GetRESTMapper(),
		ByObject: map[client.Object]cache.ByObject{
			&certmanagerv1.Certificate{}: {
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
		},
		ReaderFailOnMissingInformer: true,
	}
	customCache, err := cache.New(mgr.GetConfig(), customCacheOpts)
	if err != nil {
		return nil, err
	}
	if _, err = customCache.GetInformer(context.Background(), &v1alpha1.IstioCSR{}); err != nil {
		return nil, err
	}
	if _, err = customCache.GetInformer(context.Background(), &certmanagerv1.Certificate{}); err != nil {
		return nil, err
	}
	if _, err = customCache.GetInformer(context.Background(), &appsv1.Deployment{}); err != nil {
		return nil, err
	}
	if _, err = customCache.GetInformer(context.Background(), &rbacv1.ClusterRole{}); err != nil {
		return nil, err
	}
	if _, err = customCache.GetInformer(context.Background(), &rbacv1.ClusterRoleBinding{}); err != nil {
		return nil, err
	}
	if _, err = customCache.GetInformer(context.Background(), &rbacv1.Role{}); err != nil {
		return nil, err
	}
	if _, err = customCache.GetInformer(context.Background(), &rbacv1.RoleBinding{}); err != nil {
		return nil, err
	}
	if _, err = customCache.GetInformer(context.Background(), &corev1.Service{}); err != nil {
		return nil, err
	}
	if _, err = customCache.GetInformer(context.Background(), &corev1.ServiceAccount{}); err != nil {
		return nil, err
	}
	if _, err = customCache.GetInformer(context.Background(), &corev1.Secret{}); err != nil {
		return nil, err
	}
	if _, err = customCache.GetInformer(context.Background(), &corev1.ConfigMap{}); err != nil {
		return nil, err
	}
	if _, err = customCache.GetInformer(context.Background(), &certmanagerv1.Issuer{}); err != nil {
		return nil, err
	}
	if _, err = customCache.GetInformer(context.Background(), &certmanagerv1.ClusterIssuer{}); err != nil {
		return nil, err
	}

	err = mgr.Add(customCache)
	if err != nil {
		return nil, err
	}

	customClient, err := client.New(mgr.GetConfig(), client.Options{
		HTTPClient: mgr.GetHTTPClient(),
		Scheme:     mgr.GetScheme(),
		Mapper:     mgr.GetRESTMapper(),
		Cache: &client.CacheOptions{
			Reader: customCache,
		},
	})
	if err != nil {
		return nil, err
	}

	return customClient, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	mapFunc := func(ctx context.Context, obj client.Object) []reconcile.Request {
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
				if objLabels[requestEnqueueLabelKey] == requestEnqueueLabelValue {
					return true
				}
				value := objLabels[istiocsrResourceWatchLabelName]
				if value == "" {
					return false
				}
				key := strings.Split(value, "_")
				if len(key) != 2 {
					r.log.Error(fmt.Errorf("invalid label format"), "%s label value(%s) not in expected format on %s resource", istiocsrResourceWatchLabelName, value, obj.GetName())
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
		return object.GetLabels() != nil && object.GetLabels()[requestEnqueueLabelKey] == requestEnqueueLabelValue
	})

	// predicate function to filter events for objects which controller is interested in, but
	// not managed or created by controller.
	controllerWatchResources := predicate.NewPredicateFuncs(func(object client.Object) bool {
		return object.GetLabels() != nil && object.GetLabels()[istiocsrResourceWatchLabelName] != ""
	})

	withIgnoreStatusUpdatePredicates := builder.WithPredicates(predicate.GenerationChangedPredicate{}, controllerManagedResources)
	controllerWatchResourcePredicates := builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}, controllerWatchResources)
	controllerManagedResourcePredicates := builder.WithPredicates(controllerManagedResources)

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
		WatchesMetadata(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(mapFunc), controllerWatchResourcePredicates).
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
		if IsMultipleInstanceError(err) {
			r.eventRecorder.Eventf(istiocsr, corev1.EventTypeWarning, "MultiIstioCSRInstance", "creation of multiple istiocsr instances is not supported, will not be processed")
			err = nil
		}
		return ctrl.Result{}, err
	}

	var errUpdate error = nil
	if err := r.reconcileIstioCSRDeployment(istiocsr, istioCSRCreateRecon); err != nil {
		r.log.Error(err, "failed to reconcile IstioCSR deployment", "request", req)
		if IsIrrecoverableError(err) {
			// Set both conditions atomically before updating status
			degradedChanged := istiocsr.Status.SetCondition(v1alpha1.Degraded, metav1.ConditionTrue, v1alpha1.ReasonFailed, fmt.Sprintf("reconciliation failed with irrecoverable error not retrying: %v", err))
			readyChanged := istiocsr.Status.SetCondition(v1alpha1.Ready, metav1.ConditionFalse, v1alpha1.ReasonReady, "")

			if degradedChanged || readyChanged {
				r.log.V(2).Info("updating istiocsr conditions on irrecoverable error",
					"namespace", istiocsr.GetNamespace(),
					"name", istiocsr.GetName(),
					"degradedChanged", degradedChanged,
					"readyChanged", readyChanged,
					"error", err)
				errUpdate = r.updateCondition(istiocsr, nil)
			}
			return ctrl.Result{}, errUpdate
		} else {
			// Set both conditions atomically before updating status
			degradedChanged := istiocsr.Status.SetCondition(v1alpha1.Degraded, metav1.ConditionFalse, v1alpha1.ReasonReady, "")
			readyChanged := istiocsr.Status.SetCondition(v1alpha1.Ready, metav1.ConditionFalse, v1alpha1.ReasonInProgress, fmt.Sprintf("reconciliation failed, retrying: %v", err))

			if degradedChanged || readyChanged {
				r.log.V(2).Info("updating istiocsr conditions on recoverable error",
					"namespace", istiocsr.GetNamespace(),
					"name", istiocsr.GetName(),
					"degradedChanged", degradedChanged,
					"readyChanged", readyChanged,
					"error", err)
				errUpdate = r.updateCondition(istiocsr, err)
			}
			// For recoverable errors, either requeue manually or return error, not both
			// If status update failed, return the update error; otherwise return the original error
			if errUpdate != nil {
				return ctrl.Result{}, errUpdate
			}
			return ctrl.Result{RequeueAfter: defaultRequeueTime}, nil
		}
	}

	// Set both conditions atomically before updating status on success
	degradedChanged := istiocsr.Status.SetCondition(v1alpha1.Degraded, metav1.ConditionFalse, v1alpha1.ReasonReady, "")
	readyChanged := istiocsr.Status.SetCondition(v1alpha1.Ready, metav1.ConditionTrue, v1alpha1.ReasonReady, "reconciliation successful")

	if degradedChanged || readyChanged {
		r.log.V(2).Info("updating istiocsr conditions on successful reconciliation",
			"namespace", istiocsr.GetNamespace(),
			"name", istiocsr.GetName(),
			"degradedChanged", degradedChanged,
			"readyChanged", readyChanged)
		errUpdate = r.updateCondition(istiocsr, nil)
	}
	return ctrl.Result{}, errUpdate
}

// cleanUp handles deletion of istiocsr.openshift.operator.io gracefully.
func (r *Reconciler) cleanUp(istiocsr *v1alpha1.IstioCSR) (bool, error) {
	// TODO: For GA, handle cleaning up of resources created for installing istio-csr operand.
	// This might require a validation webhook to check for usage of service as GRPC endpoint in
	// any of OpenShift Service Mesh or Istiod deployments to avoid disruptions across cluster.
	r.eventRecorder.Eventf(istiocsr, corev1.EventTypeWarning, "RemoveDeployment", "%s/%s istiocsr marked for deletion, remove reference in istiod deployment and remove all resources created for istiocsr deployment", istiocsr.GetNamespace(), istiocsr.GetName())
	return false, nil
}
