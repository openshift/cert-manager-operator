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

// GetOperatorState uses a lister from shared informers
func (c OperatorClient) GetOperatorState() (*operatorv1.OperatorSpec, *operatorv1.OperatorStatus, string, error) {
	instance, err := c.Informers.Operator().V1alpha1().CertManagers().Lister().Get("cluster")
	if err != nil {
		return nil, nil, "", err
	}

	return &instance.Spec.OperatorSpec, &instance.Status.OperatorStatus, instance.ResourceVersion, nil
}

// GetOperatorStateWithQuorum performs direct server read
func (c OperatorClient) GetOperatorStateWithQuorum(ctx context.Context) (spec *operatorv1.OperatorSpec, status *operatorv1.OperatorStatus, resourceVersion string, err error) {
	instance, err := c.Client.CertManagers().Get(ctx, "cluster", metav1.GetOptions{})
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

func (c OperatorClient) EnsureFinalizer(ctx context.Context, finalizer string) error {
	instance, err := c.Informers.Operator().V1alpha1().CertManagers().Lister().Get("cluster")
	if err != nil {
		return err
	}

	finalizers := instance.GetFinalizers()
	for _, f := range finalizers {
		if f == finalizer {
			return nil
		}
	}

	// updating finalizers
	newFinalizers := append(finalizers, finalizer)
	err = c.saveFinalizers(ctx, instance, newFinalizers)
	if err != nil {
		return err
	}

	return nil
}

func (c OperatorClient) RemoveFinalizer(ctx context.Context, finalizer string) error {
	instance, err := c.Informers.Operator().V1alpha1().CertManagers().Lister().Get("cluster")
	if err != nil {
		return err
	}

	finalizers := instance.GetFinalizers()
	found := false
	newFinalizers := make([]string, 0, len(finalizers))
	for _, f := range finalizers {
		if f == finalizer {
			found = true
			continue
		}
		newFinalizers = append(newFinalizers, f)
	}
	if !found {
		return nil
	}

	err = c.saveFinalizers(ctx, instance, newFinalizers)
	if err != nil {
		return err
	}
	return nil
}

func (c OperatorClient) saveFinalizers(ctx context.Context, instance *v1alpha1.CertManager, finalizers []string) error {
	clone := instance.DeepCopy()
	clone.SetFinalizers(finalizers)
	_, err := c.Client.CertManagers().Update(ctx, clone, metav1.UpdateOptions{})
	return err
}
