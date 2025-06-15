package optionalinformer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorv1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned/fake"
)

func TestOptionalInformer(t *testing.T) {
	type fakeInformerFactoryStub struct{}
	dummyInformerInit := func() fakeInformerFactoryStub {
		return struct{}{}
	}

	fixedGVRForTest := operatorv1alpha1.SchemeGroupVersion.WithResource("certmanagers")

	tests := []struct {
		isCRDPresent   bool
		expectInformer bool
	}{
		// false => false, true => true
		{isCRDPresent: false, expectInformer: false},
		{isCRDPresent: true, expectInformer: true},
	}

	for _, tt := range tests {
		fakeClient := createFakeClientForDiscovery(tt.isCRDPresent)

		optinInformer := NewOptionalInformer(context.TODO(), fixedGVRForTest,
			fakeClient.Discovery(), dummyInformerInit)

		assert.Equal(t, tt.isCRDPresent, optinInformer.Discover(), "discovery does not match CRD registration")

		assert.Equal(t, tt.expectInformer, optinInformer.Applicable(), "undesired optional informer applicable(ity)")
		assert.Equal(t, tt.expectInformer, optinInformer.InformerFactory != nil, "broken informer factory init func call")
	}
}

func createFakeClientForDiscovery(isResourcePresent bool) *fake.Clientset {
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
	fakeClient.Fake.Resources = []*metav1.APIResourceList{
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
