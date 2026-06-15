package common

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/klog/v2"

	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	configinformersv1 "github.com/openshift/client-go/config/informers/externalversions/config/v1"
	libgocrypto "github.com/openshift/library-go/pkg/crypto"

	"github.com/openshift/cert-manager-operator/pkg/tlsprofile"
)

const (
	certmanagerControllerDeployment = "cert-manager"
	certmanagerWebhookDeployment    = "cert-manager-webhook"
	certmanagerCAinjectorDeployment = "cert-manager-cainjector"
)

// WithClusterTLSProfileFromAPIServer applies TLS settings from
// apiserver.config.openshift.io/cluster to cert-manager operands. It is only
// registered when the shared config.openshift.io informer factory is available
// (OpenShift clusters).
func WithClusterTLSProfileFromAPIServer(apiServerInformer configinformersv1.APIServerInformer) func(*operatorv1.OperatorSpec, *appsv1.Deployment) error {
	return func(_ *operatorv1.OperatorSpec, deployment *appsv1.Deployment) error {
		if len(deployment.Spec.Template.Spec.Containers) != 1 {
			return fmt.Errorf("deployment %s/%s: expected 1 container for TLS profile hook, got %d",
				deployment.Namespace, deployment.Name, len(deployment.Spec.Template.Spec.Containers))
		}

		apiServer, err := apiServerInformer.Lister().Get("cluster")
		if err != nil {
			return fmt.Errorf("failed to get apiserver.config.openshift.io/cluster: %w", err)
		}

		adherence := apiServer.Spec.TLSAdherence
		if !libgocrypto.ShouldHonorClusterTLSProfile(adherence) {
			klog.V(4).Infof("skipping cluster TLS profile for deployment %s: apiserver tlsAdherence=%q", deployment.Name, adherence)
			return nil
		}
		if adherence != configv1.TLSAdherencePolicyStrictAllComponents {
			klog.Warningf("apiserver.config.openshift.io/cluster has unknown tlsAdherence %q; treating as StrictAllComponents for cert-manager operands", adherence)
		}

		// Resolve TLSSecurityProfile only after tlsAdherence confirms this operand
		// must honor the cluster profile; invalid profile settings are irrelevant when skipped.
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
		sourceArgs := container.Args
		if effective.MinTLSVersion == configv1.VersionTLS13 {
			sourceArgs = StripArgsByKeys(sourceArgs, ArgKeysSet(tlsprofile.CertManagerCipherSuiteArgKeys))
		}
		container.Args = MergeContainerArgs(sourceArgs, extra)
		deployment.Spec.Template.Spec.Containers[0] = container
		return nil
	}
}
