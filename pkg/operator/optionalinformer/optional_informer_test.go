package optionalinformer

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

	// the fake client set does not populate API resource list by default
	// which is required to make a fake discovery call
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

// ServerResourcesForGroupVersion is the only func that OptionalInformer's discovery client calls.
func (f *alwaysErrorFakeDiscovery) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	return nil, fmt.Errorf("expected foo error")
}

func createErroneousFakeDiscoveryClient() discovery.DiscoveryInterface {
	return &alwaysErrorFakeDiscovery{}
}

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

			optInformer, err := NewOptionalInformer(context.TODO(), fixedGVRForTest,
				fakeClient.Discovery(), dummyInformerInit)
			require.NoError(t, err)

			discovered, err := optInformer.Discover()
			require.NoError(t, err)
			assert.Equal(t, tt.isCRDPresent, discovered, "discovery does not match CRD registration")

			assert.Equal(t, tt.expectInformer, optInformer.Applicable(), "undesired optional informer applicable(ity)")
			assert.Equal(t, tt.expectInformer, optInformer.InformerFactory != nil, "broken informer factory init func call")
		}
	})

	t.Run("negative case with an expected error", func(t *testing.T) {
		errorProneDiscoveryClient := createErroneousFakeDiscoveryClient()
		_, err := NewOptionalInformer(context.TODO(), fixedGVRForTest,
			errorProneDiscoveryClient, dummyInformerInit)

		require.Error(t, err)
	})
}
