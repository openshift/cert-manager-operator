package deployment

import (
	"context"
	"fmt"

	"github.com/openshift/library-go/pkg/controller"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/openshift/cert-manager-operator/apis/operator/v1alpha1"
)

type OwnerReferenceSetter struct {
	operatorClient v1helpers.OperatorClient
}

func NewOwnerReferenceSetter(operatorClient v1helpers.OperatorClient) OwnerReferenceSetter {
	return OwnerReferenceSetter{
		operatorClient: operatorClient,
	}
}

func (o *OwnerReferenceSetter) preprocessResources(_ context.Context, object runtime.Object) (runtime.Object, error) {
	ownerRef, err := createOwnerReference(o.operatorClient)
	if err != nil {
		return nil, err
	}
	controller.EnsureOwnerRef(object.(metav1.Object), ownerRef)
	return object, nil
}

func createOwnerReference(operatorClient v1helpers.OperatorClient) (v1.OwnerReference, error) {
	if operatorClient == nil {
		return v1.OwnerReference{}, fmt.Errorf("operatorclient not found")
	}
	meta, err := operatorClient.GetObjectMeta()
	if err != nil {
		return v1.OwnerReference{}, err
	}
	return v1.OwnerReference{
		//TODO: Consider making it config.openshift.io? This way we could just call
		// oc delete co cert-manager and it would cascade into other objects as well.
		APIVersion:         v1alpha1.GroupVersion.String(),
		Kind:               "CertManager",
		Name:               meta.GetName(),
		UID:                meta.GetUID(),
		BlockOwnerDeletion: &[]bool{false}[0],
		Controller:         &[]bool{false}[0],
	}, nil
}
