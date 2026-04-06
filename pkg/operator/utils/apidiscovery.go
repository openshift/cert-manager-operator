// Package utils provides small shared helpers for the cert-manager operator,
// including optional API discovery (whether a GroupVersionResource is served)
// and shared informer wiring that runs only when that API exists.
package utils

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
)

// ResourceDiscoverer reports whether a GroupVersionResource is served by the
// cluster API server (via discovery). Implementations include the value returned
// from NewResourceDiscoverer.
type ResourceDiscoverer interface {
	Discover() (bool, error)
}

// apiResourceDiscoverer implements ResourceDiscoverer using the discovery API.
type apiResourceDiscoverer struct {
	gvr             schema.GroupVersionResource
	discoveryClient discovery.DiscoveryInterface
}

// OptionalInformer holds a typed informer factory only when the target API
// resource exists on the cluster. Populated by InitInformerIfAvailable when
// discovery reports the resource is served.
type OptionalInformer[GroupInformer any] struct {
	InformerFactory *GroupInformer
}

// NewResourceDiscoverer returns a ResourceDiscoverer for the given GVR and
// discovery client. Discover is not called until Discover or
// InitInformerIfAvailable runs.
func NewResourceDiscoverer(gvr schema.GroupVersionResource, discoveryClient discovery.DiscoveryInterface) ResourceDiscoverer {
	return &apiResourceDiscoverer{
		gvr:             gvr,
		discoveryClient: discoveryClient,
	}
}

// Discover reports whether the API server lists a.gvr.Resource in the
// APIResources for the group/version in a.gvr. A NotFound error from discovery is
// treated as not present (false, nil); other errors are returned.
func (a *apiResourceDiscoverer) Discover() (bool, error) {
	resources, err := a.discoveryClient.ServerResourcesForGroupVersion(a.gvr.GroupVersion().String())
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to discover resources: %w", err)
	}

	for _, res := range resources.APIResources {
		if res.Name == a.gvr.Resource {
			return true, nil
		}
	}

	return false, nil
}

// InitInformerIfAvailable calls d.Discover(), and if the resource is served it
// calls informerInitFunc and stores the result in InformerFactory; otherwise
// InformerFactory remains nil and no error is returned (the API is treated as
// unavailable).
func InitInformerIfAvailable[groupInformer any](
	d ResourceDiscoverer,
	informerInitFunc func() groupInformer,
) (*OptionalInformer[groupInformer], error) {
	o := new(OptionalInformer[groupInformer])

	discovered, err := d.Discover()
	if err != nil {
		return nil, err
	}

	if discovered {
		informer := informerInitFunc()
		o.InformerFactory = &informer
	}

	return o, nil
}

// Applicable reports whether an informer factory was set (the API was present).
func (o *OptionalInformer[GroupInformer]) Applicable() bool {
	return o.InformerFactory != nil
}
