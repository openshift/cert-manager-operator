package istiocsr

import (
	"github.com/openshift/cert-manager-operator/pkg/tlsprofile"
)

// clusterTLSProfileArgs returns cert-manager-istio-csr gRPC serving TLS flags derived
// from the TLS config initialized at controller startup. The TLS config is resolved
// once from apiserver.config.openshift.io/cluster; subsequent changes to the cluster
// TLS profile or adherence trigger an operator restart which re-initializes this config.
//
// It returns nil when the cluster does not require operands to honor the cluster-wide
// TLS profile, so istio-csr keeps its upstream defaults. A minimum version below TLS 1.2
// returns an error so the deployment is not rolled out with flags the operand rejects.
func (r *Reconciler) clusterTLSProfileArgs() ([]string, error) {
	if !r.tlsConfig.Honor {
		r.log.V(4).Info("skipping cluster TLS profile for istio-csr deployment", "tlsAdherence", r.tlsConfig.Adherence)
		return nil, nil
	}

	if r.tlsConfig.Spec == nil {
		r.log.V(4).Info("no TLS profile spec available, using upstream defaults")
		return nil, nil
	}

	args, err := tlsprofile.IstioCSRServingTLSArgs(r.tlsConfig.Spec)
	if err != nil {
		return nil, err
	}
	return args, nil
}
