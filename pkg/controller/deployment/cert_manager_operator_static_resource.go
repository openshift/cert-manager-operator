package deployment

import (
	"github.com/openshift/cert-manager-operator/pkg/operator/operatorclient"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/staticresourcecontroller"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

const (
	certManagerOperatorStaticResourcesControllerName = operatorName + "-operator-static-resources-"
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
		injectNamespace(operatorclient.OperatorNamespace),
		certManagerOperatorAssetFiles,
		kubeClientContainer,
		operatorClient,
		eventsRecorder,
	).AddKubeInformers(kubeInformersForNamespaces)
}

func injectNamespace(namespace string) resourceapply.AssetFunc {
	return func(name string) ([]byte, error) {
		content := assets.MustAsset(name)
		var obj unstructured.Unstructured
		err := yaml.Unmarshal(content, &obj)
		if err != nil {
			return nil, err
		}
		obj.SetNamespace(namespace)
		return yaml.Marshal(&obj)
	}
}
