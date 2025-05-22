package operator

import (
	configv1client "github.com/openshift/client-go/config/clientset/versioned"
	configinformers "github.com/openshift/client-go/config/informers/externalversions"
)

type OptionalConfigInformer struct {
	ConfigClient    configv1client.Clientset
	InformerFactory *configinformers.SharedInformerFactory
}

func (o *OptionalConfigInformer) Exists() bool {
	return o.InformerFactory != nil
}

func (o *OptionalConfigInformer) Discover() {
}
