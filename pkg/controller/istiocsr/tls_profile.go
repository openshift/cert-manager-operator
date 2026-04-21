package istiocsr

import (
	"context"

	configv1 "github.com/openshift/api/config/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/tlsprofile"
)

// servingTLSArgsFromCluster returns istio-csr CLI flags derived from
// apiserver.config.openshift.io/cluster when the object exists and is readable.
// If the API is unavailable or the cluster object is absent, it returns nil, nil
// so the operand keeps upstream defaults.
func (r *Reconciler) servingTLSArgsFromCluster(ctx context.Context) ([]string, error) {
	api := &configv1.APIServer{}
	if err := r.Get(ctx, client.ObjectKey{Name: "cluster"}, api); err != nil {
		if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			return nil, nil
		}
		return nil, err
	}
	spec, err := tlsprofile.EffectiveSpec(api.Spec.TLSSecurityProfile)
	if err != nil {
		return nil, err
	}
	return tlsprofile.IstioCSRServingGRPCArgs(spec), nil
}

func (r *Reconciler) enqueueAllIstioCSRRequests(ctx context.Context, _ client.Object) []reconcile.Request {
	list := &v1alpha1.IstioCSRList{}
	if err := r.List(ctx, list); err != nil {
		r.log.Error(err, "failed to list IstioCSR resources while handling apiserver.config.openshift.io change")
		return nil
	}
	out := make([]reconcile.Request, 0, len(list.Items))
	for i := range list.Items {
		out = append(out, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&list.Items[i])})
	}
	return out
}
