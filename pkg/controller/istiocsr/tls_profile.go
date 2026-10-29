package istiocsr

import (
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
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
		// Snapshot TLS cannot be applied to the operand. Treat as irrecoverable so
		// Ready/Degraded surface a user-actionable message instead of a generic
		// retry. A later APIServer profile change restarts the operator and
		// re-reads the snapshot.
		return nil, common.NewIrrecoverableError(err, "cluster TLS profile cannot be applied to istio-csr")
	}
	return args, nil
}
