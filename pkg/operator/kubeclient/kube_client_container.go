package kubeclient

import (
	certmanoperatorclient "github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned"
	configv1client "github.com/openshift/client-go/config/clientset/versioned"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type KubeClientContainer struct {
	KubeConfig                *kubernetes.Clientset
	DynamicClient             dynamic.Interface
	ConfigClient              *configv1client.Clientset
	CertManagerOperatorClient *certmanoperatorclient.Clientset
	ApiExtensionsClient       *apiextensionsclient.Clientset
}

func NewKubeClientContainer(protoKubeConfig *rest.Config, kubeConfig *rest.Config) (KubeClientContainer, error) {
	emptyRef := KubeClientContainer{}
	kubeClient, err := kubernetes.NewForConfig(protoKubeConfig)
	if err != nil {
		return emptyRef, err
	}

	configClient, err := configv1client.NewForConfig(kubeConfig)
	if err != nil {
		return emptyRef, err
	}

	dynamicClient, err := dynamic.NewForConfig(protoKubeConfig)
	if err != nil {
		return emptyRef, err
	}

	certManagerOperatorClient, err := certmanoperatorclient.NewForConfig(kubeConfig)
	if err != nil {
		return emptyRef, err
	}

	apiextensionsClient, err := apiextensionsclient.NewForConfig(kubeConfig)
	if err != nil {
		return emptyRef, err
	}
	return KubeClientContainer{
		KubeConfig:                kubeClient,
		DynamicClient:             dynamicClient,
		ConfigClient:              configClient,
		CertManagerOperatorClient: certManagerOperatorClient,
		ApiExtensionsClient:       apiextensionsClient,
	}, nil
}

func (c *KubeClientContainer) ToKubeClientHolder() *resourceapply.ClientHolder {
	return resourceapply.NewKubeClientHolder(c.KubeConfig).WithDynamicClient(c.DynamicClient).WithAPIExtensionsClient(c.ApiExtensionsClient)
}
