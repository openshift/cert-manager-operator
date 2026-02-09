package optionalinformer

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
)

type OptionalInformer[GroupInformer any] struct {
	gvr             schema.GroupVersionResource
	discoveryClient discovery.DiscoveryInterface

	InformerFactory *GroupInformer
}

func NewOptionalInformer[groupInformer any](
	_ context.Context,
	gvr schema.GroupVersionResource,
	discoveryClient discovery.DiscoveryInterface,
	informerInitFunc func() groupInformer,
) (*OptionalInformer[groupInformer], error) {
	o := &OptionalInformer[groupInformer]{
		gvr:             gvr,
		discoveryClient: discoveryClient,
	}

	discovered, err := o.Discover()
	if err != nil {
		return nil, err
	}

	if discovered {
		informer := informerInitFunc()
		o.InformerFactory = &informer
	}

	return o, nil
}

// Applicable determines if an active informer was successfully created.
func (o *OptionalInformer[GroupInformer]) Applicable() bool {
	return o.InformerFactory != nil
}

// Discover returns if the required CRD is present on the cluster or not.
func (o *OptionalInformer[GroupInformer]) Discover() (bool, error) {
	_ = o.gvr.GroupVersion().String()
	resources, err := o.discoveryClient.ServerResourcesForGroupVersion(o.gvr.GroupVersion().String())

	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}

		return false, err
	}

	for _, res := range resources.APIResources {
		if res.Name == o.gvr.Resource {
			return true, nil
		}
	}

	return false, nil
}
