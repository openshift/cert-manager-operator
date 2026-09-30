package istiocsr

import (
	"context"

	configv1 "github.com/openshift/api/config/v1"
	libgocrypto "github.com/openshift/library-go/pkg/crypto"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/tlsprofile"
)

// clusterAPIServerName is the singleton name of apiserver.config.openshift.io/cluster.
const clusterAPIServerName = "cluster"

// clusterTLSProfileArgs returns cert-manager-istio-csr gRPC serving TLS flags derived
// from apiserver.config.openshift.io/cluster, honoring the same tlsAdherence gate used
// for the other cert-manager operands (see pkg/controller/common.WithClusterTLSProfileFromAPIServer).
// It returns nil, nil when the cluster does not require operands to honor the cluster-wide
// TLS profile, so istio-csr keeps its upstream defaults.
func (r *Reconciler) clusterTLSProfileArgs() ([]string, error) {
	apiServer := &configv1.APIServer{}
	if err := r.Get(r.ctx, client.ObjectKey{Name: clusterAPIServerName}, apiServer); err != nil {
		return nil, common.FromClientError(err, "failed to get apiserver.config.openshift.io/%s", clusterAPIServerName)
	}

	adherence := apiServer.Spec.TLSAdherence
	if !libgocrypto.ShouldHonorClusterTLSProfile(adherence) {
		r.log.V(4).Info("skipping cluster TLS profile for istio-csr deployment", "tlsAdherence", adherence)
		return nil, nil
	}
	if adherence != configv1.TLSAdherencePolicyStrictAllComponents {
		r.log.Info("apiserver.config.openshift.io/cluster has unknown tlsAdherence; treating as StrictAllComponents for istio-csr", "tlsAdherence", adherence)
	}

	// Resolve TLSSecurityProfile only after tlsAdherence confirms istio-csr must honor the
	// cluster profile; invalid profile settings are irrelevant when skipped.
	effective, err := tlsprofile.EffectiveSpec(apiServer.Spec.TLSSecurityProfile)
	if err != nil {
		return nil, err
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
