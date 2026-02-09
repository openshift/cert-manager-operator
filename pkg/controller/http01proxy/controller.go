package http01proxy

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	operatorv1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

const ControllerName = "http01proxy-controller"

// Reconciler reconciles an HTTP01Proxy object
type Reconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func New(mgr ctrl.Manager) (*Reconciler, error) {
	return &Reconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()}, nil
}

//+kubebuilder:rbac:groups=operator.openshift.io,resources=http01proxies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=operator.openshift.io,resources=http01proxies/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=operator.openshift.io,resources=http01proxies/finalizers,verbs=update

// TODO: additional RBAC will be needed to read Challenges and manage Routes/Services

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling HTTP01Proxy", "name", req.NamespacedName)

	// Minimal no-op reconcile for scaffold
	return ctrl.Result{}, nil
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatorv1alpha1.HTTP01Proxy{}).
		Complete(r)
}

// helper to record events (future use)
func (r *Reconciler) recordEventf(obj runtime.Object, eventtype, reason, messageFmt string, args ...interface{}) {
	_ = fmt.Sprintf(messageFmt, args...)
	// intentionally no-op for now; will wire an EventRecorder when we add logic
	_ = corev1.EventTypeNormal
}
