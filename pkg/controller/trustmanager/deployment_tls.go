package trustmanager

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"

	configv1 "github.com/openshift/api/config/v1"

	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/tlsprofile"
)

// applyClusterTLSProfile merges cluster TLS security profile flags onto the
// trust-manager webhook container when apiserver tlsAdherence requires it.
// When the APIServer resource is missing (non-OpenShift) or adherence does not
// require enforcement, this is a no-op.
func (r *Reconciler) applyClusterTLSProfile(deployment *appsv1.Deployment) error {
	if r.CtrlClient == nil {
		return nil
	}

	effective, err := tlsprofile.ResolveHonoredTLSProfile(
		r.ctx,
		tlsprofile.NewClientReaderAPIServerFetch(r.CtrlClient),
		"trust-manager",
		tlsprofile.FetchErrorPropagateExceptNotFound,
	)
	if err != nil {
		return err
	}
	if effective == nil {
		return nil
	}

	return applyTrustManagerWebhookTLSArgs(deployment, effective)
}

// applyTrustManagerWebhookTLSArgs merges profile-derived webhook TLS flags onto
// the trust-manager container. Exported for unit tests via package-level use.
func applyTrustManagerWebhookTLSArgs(deployment *appsv1.Deployment, spec *configv1.TLSProfileSpec) error {
	extra := tlsprofile.TrustManagerWebhookTLSArgs(spec)
	if len(extra) == 0 {
		return nil
	}

	for i := range deployment.Spec.Template.Spec.Containers {
		if deployment.Spec.Template.Spec.Containers[i].Name != trustManagerContainerName {
			continue
		}
		sourceArgs := deployment.Spec.Template.Spec.Containers[i].Args
		if spec != nil && spec.MinTLSVersion == configv1.VersionTLS13 {
			sourceArgs = common.StripArgsByKeys(sourceArgs, common.ArgKeysSet(tlsprofile.TrustManagerCipherSuiteArgKeys))
		}
		deployment.Spec.Template.Spec.Containers[i].Args = common.MergeContainerArgs(sourceArgs, extra)
		return nil
	}
	return fmt.Errorf("deployment %s/%s missing container %q", deployment.Namespace, deployment.Name, trustManagerContainerName)
}
