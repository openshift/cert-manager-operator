package istiocsr

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/tlsprofile"
)

// clusterTLSProfileArgs returns cert-manager-istio-csr gRPC serving TLS flags derived
// from apiserver.config.openshift.io/cluster. It uses the same tlsAdherence gate as
// the other operands (tlsprofile.ResolveHonoredTLSProfile). A nil result means
// istio-csr keeps its upstream defaults.
func (r *Reconciler) clusterTLSProfileArgs() ([]string, error) {
	effective, err := tlsprofile.ResolveHonoredTLSProfile(
		r.ctx,
		tlsprofile.NewClientReaderAPIServerFetch(r.CtrlClient),
		"istio-csr",
		tlsprofile.FetchErrorPropagateExceptNotFound,
	)
	if err != nil {
		return nil, err
	}
	if effective == nil {
		return nil, nil
	}
	return tlsprofile.IstioCSRServingTLSArgs(effective), nil
}

// enqueueAllIstioCSRRequests maps an apiserver.config.openshift.io/cluster change event
// to reconcile requests for every existing IstioCSR resource, so a cluster-wide TLS
// profile change is re-applied to the istio-csr deployment.
func (r *Reconciler) enqueueAllIstioCSRRequests(ctx context.Context, _ client.Object) []reconcile.Request {
	list := &v1alpha1.IstioCSRList{}
	if err := r.List(ctx, list); err != nil {
		r.log.Error(err, "failed to list istiocsr.openshift.operator.io resources while handling apiserver.config.openshift.io change")
		return nil
	}

	requests := make([]reconcile.Request, 0, len(list.Items))
	for i := range list.Items {
		requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&list.Items[i])})
	}
	return requests
}
