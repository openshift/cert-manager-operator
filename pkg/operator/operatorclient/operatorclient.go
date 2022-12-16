package operatorclient

import (
	"context"
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	operatorconfigclient "github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned/typed/operator/v1alpha1"
	operatorclientinformers "github.com/openshift/cert-manager-operator/pkg/operator/informers/externalversions"

	"github.com/openshift/library-go/pkg/operator/v1helpers"
)

type OperatorClient struct {
	Informers operatorclientinformers.SharedInformerFactory
	Client    operatorconfigclient.CertManagersGetter
}

var _ v1helpers.OperatorClient = &OperatorClient{}

func (c OperatorClient) GetObjectMeta() (*metav1.ObjectMeta, error) {
	instance, err := c.Informers.Operator().V1alpha1().CertManagers().Lister().Get("cluster")
	if err != nil {
		return nil, err
	}

	return &instance.ObjectMeta, nil
}

func (c OperatorClient) Informer() cache.SharedIndexInformer {
	return c.Informers.Operator().V1alpha1().CertManagers().Informer()
}

func (c OperatorClient) GetOperatorState() (*operatorv1.OperatorSpec, *operatorv1.OperatorStatus, string, error) {
	instance, err := c.Informers.Operator().V1alpha1().CertManagers().Lister().Get("cluster")
	if err != nil {
		return nil, nil, "", err
	}

	return &instance.Spec.OperatorSpec, &instance.Status.OperatorStatus, instance.ResourceVersion, nil
}

func GetUnsupportedConfigOverrides(operatorSpec *operatorv1.OperatorSpec) (*v1alpha1.UnsupportedConfigOverrides, error) {
	if len(operatorSpec.UnsupportedConfigOverrides.Raw) != 0 {
		out := &v1alpha1.UnsupportedConfigOverrides{}
		err := json.Unmarshal(operatorSpec.UnsupportedConfigOverrides.Raw, out)
		if err != nil {
			return nil, err
		}
		return out, nil
	}
	return nil, nil
}

func (c OperatorClient) UpdateOperatorSpec(ctx context.Context, resourceVersion string, spec *operatorv1.OperatorSpec) (*operatorv1.OperatorSpec, string, error) {
	original, err := c.Informers.Operator().V1alpha1().CertManagers().Lister().Get("cluster")
	if err != nil {
		return nil, "", err
	}
	copy := original.DeepCopy()
	copy.ResourceVersion = resourceVersion
	copy.Spec.OperatorSpec = *spec

	ret, err := c.Client.CertManagers().Update(ctx, copy, metav1.UpdateOptions{})
	if err != nil {
		return nil, "", err
	}

	return &ret.Spec.OperatorSpec, ret.ResourceVersion, nil
}

func (c OperatorClient) UpdateOperatorStatus(ctx context.Context, resourceVersion string, status *operatorv1.OperatorStatus) (*operatorv1.OperatorStatus, error) {
	original, err := c.Informers.Operator().V1alpha1().CertManagers().Lister().Get("cluster")
	if err != nil {
		return nil, err
	}
	copy := original.DeepCopy()
	copy.ResourceVersion = resourceVersion
	copy.Status.OperatorStatus = *status

	ret, err := c.Client.CertManagers().UpdateStatus(ctx, copy, metav1.UpdateOptions{})
	if err != nil {
		return nil, err
	}

	return &ret.Status.OperatorStatus, nil
}
