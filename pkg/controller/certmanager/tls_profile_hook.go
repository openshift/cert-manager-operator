package deployment

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"

	operatorv1 "github.com/openshift/api/operator/v1"
	configinformersv1 "github.com/openshift/client-go/config/informers/externalversions/config/v1"

	"github.com/openshift/cert-manager-operator/pkg/tlsprofile"
)

// withClusterTLSProfileFromAPIServer applies TLS settings from
// apiserver.config.openshift.io/cluster to cert-manager operands. It is only
// registered when the shared config.openshift.io informer factory is available
// (OpenShift clusters).
func withClusterTLSProfileFromAPIServer(apiServerInformer configinformersv1.APIServerInformer) func(*operatorv1.OperatorSpec, *appsv1.Deployment) error {
	return func(_ *operatorv1.OperatorSpec, deployment *appsv1.Deployment) error {
		if len(deployment.Spec.Template.Spec.Containers) != 1 {
			return nil
		}

		apiServer, err := apiServerInformer.Lister().Get("cluster")
		if err != nil {
			return fmt.Errorf("failed to get apiserver.config.openshift.io/cluster: %w", err)
		}

		effective, err := tlsprofile.EffectiveSpec(apiServer.Spec.TLSSecurityProfile)
		if err != nil {
			return err
		}

		var extra []string
		switch deployment.Name {
		case certmanagerWebhookDeployment:
			extra = tlsprofile.CertManagerWebhookTLSArgs(effective)
		case certmanagerControllerDeployment, certmanagerCAinjectorDeployment:
			extra = tlsprofile.CertManagerOperandMetricsTLSArgs(effective)
		default:
			return nil
		}

		container := deployment.Spec.Template.Spec.Containers[0]
		container.Args = mergeContainerArgs(container.Args, extra)
		deployment.Spec.Template.Spec.Containers[0] = container
		return nil
	}
}
