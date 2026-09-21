package certmanager

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/openshift/cert-manager-operator/pkg/controller/common"
)

const (
	metricsDynamicServingCASecretName = "cert-manager-metrics-ca"
)

// withOperandMetricsTLS enables HTTPS on the cert-manager operand metrics
// listeners (port 9402) using cert-manager's dynamic metrics serving CA.
// Cipher/min-version flags continue to come from WithClusterTLSProfileFromAPIServer.
func withOperandMetricsTLS(_ *operatorv1.OperatorSpec, deployment *appsv1.Deployment) error {
	if len(deployment.Spec.Template.Spec.Containers) != 1 {
		return fmt.Errorf("deployment %s/%s: expected 1 container for metrics TLS hook, got %d",
			deployment.Namespace, deployment.Name, len(deployment.Spec.Template.Spec.Containers))
	}

	extra, ok := operandMetricsTLSArgs(deployment.Name)
	if !ok {
		return nil
	}

	container := &deployment.Spec.Template.Spec.Containers[0]
	container.Args = common.MergeContainerArgs(container.Args, extra)

	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = map[string]string{}
	}
	deployment.Spec.Template.Annotations["prometheus.io/scheme"] = "https"

	return nil
}

func operandMetricsTLSArgs(deploymentName string) ([]string, bool) {
	var dnsNames string
	switch deploymentName {
	case certmanagerControllerDeployment:
		dnsNames = "cert-manager,cert-manager.$(POD_NAMESPACE),cert-manager.$(POD_NAMESPACE).svc"
	case certmanagerWebhookDeployment:
		dnsNames = "cert-manager-webhook,cert-manager-webhook.$(POD_NAMESPACE),cert-manager-webhook.$(POD_NAMESPACE).svc"
	case certmanagerCAinjectorDeployment:
		dnsNames = "cert-manager-cainjector,cert-manager-cainjector.$(POD_NAMESPACE),cert-manager-cainjector.$(POD_NAMESPACE).svc"
	default:
		return nil, false
	}

	return []string{
		"--metrics-dynamic-serving-ca-secret-namespace=$(POD_NAMESPACE)",
		"--metrics-dynamic-serving-ca-secret-name=" + metricsDynamicServingCASecretName,
		"--metrics-dynamic-serving-dns-names=" + dnsNames,
	}, true
}
