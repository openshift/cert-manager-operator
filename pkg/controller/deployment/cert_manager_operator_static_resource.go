package deployment

import (
	"bytes"
	"github.com/openshift/cert-manager-operator/pkg/operator/operatorclient"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/staticresourcecontroller"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

const (
	certManagerOperatorStaticResourcesControllerName = operatorName + "-operator-static-resources-"
	namespaceKey                                     = "${NAMESPACE}"
)

var (
	certManagerOperatorAssetFiles = []string{
		"cert-manager-deployment/network-policy/operator-allow-egress-to-api-server.yaml",
		"cert-manager-deployment/network-policy/operator-allow-ingress-to-metrics.yaml",
		"cert-manager-deployment/network-policy/operator-deny-all-pod-selector.yaml",
	}
)

func NewCertManagerOperatorStaticResourcesController(operatorClient v1helpers.OperatorClient,
	kubeClientContainer *resourceapply.ClientHolder,
	kubeInformersForNamespaces v1helpers.KubeInformersForNamespaces,
	eventsRecorder events.Recorder,
) factory.Controller {
	return staticresourcecontroller.NewStaticResourceController(
		certManagerOperatorStaticResourcesControllerName,
		replaceNamespaceFunc(operatorclient.OperatorNamespace),
		certManagerOperatorAssetFiles,
		kubeClientContainer,
		operatorClient,
		eventsRecorder,
	).AddKubeInformers(kubeInformersForNamespaces)
}

func replaceNamespaceFunc(namespace string) resourceapply.AssetFunc {
	return func(name string) ([]byte, error) {
		content, err := assets.Asset(name)
		if err != nil {
			panic(err)
		}
		return bytes.ReplaceAll(content, []byte(namespaceKey), []byte(namespace)), nil
	}
}
