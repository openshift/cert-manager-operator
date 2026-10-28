package istiocsr

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/tlsprofile"
)

// clusterTLSProfileArgs returns cert-manager-istio-csr gRPC serving TLS flags derived
// from the current apiserver.config.openshift.io/cluster. Every reconcile reads it,
// including the reconcile that runs when the controller starts and when an IstioCSR
// is created. A later TLS profile or adherence change is delivered by the APIServer
// watch, which requeues the active IstioCSR so the next reconcile reads the new value.
//
// It uses the same tlsAdherence gate as the other operands
// (tlsprofile.ResolveHonoredTLSProfile). A nil result means istio-csr keeps its
// upstream defaults. A minimum version below TLS 1.2 returns an error so the
// deployment is not rolled out with flags the operand rejects.
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
	args, err := tlsprofile.IstioCSRServingTLSArgs(effective)
	if err != nil {
		return nil, err
	}
	return args, nil
}

// enqueueActiveIstioCSRRequests maps an apiserver.config.openshift.io/cluster change
// to a reconcile of the IstioCSR instance this controller manages. Later instances are
// rejected by disallowMultipleIstioCSRInstances and do not own a deployment.
func (r *Reconciler) enqueueActiveIstioCSRRequests(ctx context.Context, _ client.Object) []reconcile.Request {
	list := &v1alpha1.IstioCSRList{}
	if err := r.List(ctx, list); err != nil {
		r.log.Error(err, "failed to list istiocsr.openshift.operator.io resources while handling apiserver.config.openshift.io change")
		return nil
	}

	var requests []reconcile.Request
	for i := range list.Items {
		if !isActiveIstioCSR(&list.Items[i], list.Items) {
			continue
		}
		requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&list.Items[i])})
	}
	return requests
}

// isActiveIstioCSR reports whether istiocsr is the instance disallowMultipleIstioCSRInstances
// will reconcile. A processing-rejected annotation excludes it. Another IstioCSR in a
// different namespace that is older, or created at the same time, also excludes it.
func isActiveIstioCSR(istiocsr *v1alpha1.IstioCSR, items []v1alpha1.IstioCSR) bool {
	if containsProcessingRejectedAnnotation(istiocsr) {
		return false
	}
	for i := range items {
		item := &items[i]
		if item.GetNamespace() == istiocsr.GetNamespace() {
			continue
		}
		if !item.CreationTimestamp.Time.After(istiocsr.CreationTimestamp.Time) {
			return false
		}
	}
	return true
}
