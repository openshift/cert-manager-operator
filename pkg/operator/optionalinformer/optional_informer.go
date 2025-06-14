package optionalinformer

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
)

type OptionalInformer[GroupInformer any] struct {
	gvr             schema.GroupVersionResource
	discoveryClient discovery.DiscoveryInterface

	InformerFactory *GroupInformer
}

func NewOptionalInformer[groupInformer any](
	ctx context.Context,
	gvr schema.GroupVersionResource,
	discoveryClient discovery.DiscoveryInterface,
	informerInitFunc func() groupInformer,
) *OptionalInformer[groupInformer] {
	o := &OptionalInformer[groupInformer]{
		gvr:             gvr,
		discoveryClient: discoveryClient,
	}

	if o.Discover() {
		informer := informerInitFunc()
		o.InformerFactory = &informer
	}

	return o
}

// Applicable determines if an active informer was successfully created
func (o *OptionalInformer[GroupInformer]) Applicable() bool {
	return o.InformerFactory != nil
}

// Discover returns if the required CRD is present on the cluster or not
func (o *OptionalInformer[GroupInformer]) Discover() bool {
	resources, err := o.discoveryClient.ServerResourcesForGroupVersion(o.gvr.GroupVersion().String())

	// NOTE: When using with configv1.Infrastructure,
	// on a microshift cluster, here ALWAYS errors out with:
	// "the server could not find the requested resource"
	// probably, because the "config.openshift.io" API group is absent?
	if err != nil {
		return false
	}

	for _, res := range resources.APIResources {
		if res.Name == o.gvr.Resource {
			return true
		}
	}

	return false
}
