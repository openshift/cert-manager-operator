package utils

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	fakediscovery "k8s.io/client-go/discovery/fake"

	operatorv1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned/fake"
)

func createFakeClient(isResourcePresent bool) *fake.Clientset {
	if !isResourcePresent {
		return fake.NewClientset()
	}

	fakeClient := fake.NewClientset(&operatorv1alpha1.CertManager{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
		Status: operatorv1alpha1.CertManagerStatus{},
	})

	// The fake clientset does not populate APIResourceList by default; discovery
	// in tests requires these resources.
	fakeClient.Resources = []*metav1.APIResourceList{
		{
			GroupVersion: operatorv1alpha1.SchemeGroupVersion.String(),
			APIResources: []metav1.APIResource{
				{
					Name:         "certmanagers",
					SingularName: "certmanager",
					Namespaced:   false,
					Group:        operatorv1alpha1.SchemeGroupVersion.Group,
					Version:      operatorv1alpha1.SchemeGroupVersion.Version,
					Kind:         "CertManager",
					Verbs:        []string{"get", "list", "create", "update", "patch", "watch", "delete"},
				},
			},
		},
	}

	return fakeClient
}

type alwaysErrorFakeDiscovery struct {
	fakediscovery.FakeDiscovery
}

// ServerResourcesForGroupVersion is the only func apiResourceDiscoverer's discovery client calls.
func (f *alwaysErrorFakeDiscovery) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	return nil, fmt.Errorf("simulated discovery error")
}

func createErroneousFakeDiscoveryClient() discovery.DiscoveryInterface {
	return &alwaysErrorFakeDiscovery{}
}

func TestDiscoverConsoleCapability(t *testing.T) {
	gvr := schema.GroupVersionResource{
		Group:    "console.openshift.io",
		Version:  "v1",
		Resource: "consoleyamlsamples",
	}

	tests := []struct {
		name      string
		resources []*metav1.APIResourceList
		want      bool
	}{
		{
			name: "console resources present",
			resources: []*metav1.APIResourceList{
				{
					GroupVersion: "console.openshift.io/v1",
					APIResources: []metav1.APIResource{
						{Name: "consoleyamlsamples"},
						{Name: "consolequickstarts"},
					},
				},
			},
			want: true,
		},
		{
			name:      "console resources absent",
			resources: nil,
			want:      false,
		},
		{
			name: "different group present but not console",
			resources: []*metav1.APIResourceList{
				{
					GroupVersion: operatorv1alpha1.SchemeGroupVersion.String(),
					APIResources: []metav1.APIResource{
						{Name: "certmanagers"},
					},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientset()
			fakeClient.Resources = tt.resources

			discoverer := NewResourceDiscoverer(gvr, fakeClient.Discovery())
			got, err := discoverer.Discover()
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}

	t.Run("discovery error propagates", func(t *testing.T) {
		discoverer := NewResourceDiscoverer(gvr, createErroneousFakeDiscoveryClient())
		_, err := discoverer.Discover()
		require.Error(t, err)
	})
}

// TestOptionalInformer covers NewResourceDiscoverer, Discover, and
// InitInformerIfAvailable end-to-end.
func TestOptionalInformer(t *testing.T) {
	type fakeInformerFactoryStub struct{}
	dummyInformerInit := func() fakeInformerFactoryStub {
		return struct{}{}
	}

	fixedGVRForTest := operatorv1alpha1.SchemeGroupVersion.WithResource("certmanagers")

	t.Run("positive cases with no expected errors", func(t *testing.T) {
		tests := []struct {
			isCRDPresent   bool
			expectInformer bool
		}{
			// positive cases with no error
			// false => false, true => true
			{isCRDPresent: false, expectInformer: false},
			{isCRDPresent: true, expectInformer: true},
		}

		for _, tt := range tests {
			fakeClient := createFakeClient(tt.isCRDPresent)

			probe := NewResourceDiscoverer(fixedGVRForTest, fakeClient.Discovery())
			optInformer, err := InitInformerIfAvailable(probe, dummyInformerInit)
			require.NoError(t, err)

			discovered, err := probe.Discover()
			require.NoError(t, err)
			assert.Equal(t, tt.isCRDPresent, discovered, "discovery does not match fake API resource list")

			assert.Equal(t, tt.expectInformer, optInformer.Applicable(), "undesired optional informer applicable(ity)")
			assert.Equal(t, tt.expectInformer, optInformer.InformerFactory != nil, "broken informer factory init func call")
		}
	})

	t.Run("negative case with an expected error", func(t *testing.T) {
		errorProneDiscoveryClient := createErroneousFakeDiscoveryClient()
		_, err := InitInformerIfAvailable(
			NewResourceDiscoverer(fixedGVRForTest, errorProneDiscoveryClient),
			dummyInformerInit,
		)

		require.Error(t, err)
	})
}
