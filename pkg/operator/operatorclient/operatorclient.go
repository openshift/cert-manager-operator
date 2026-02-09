package operatorclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/clock"

	operatorv1 "github.com/openshift/api/operator/v1"
	applyoperatorv1 "github.com/openshift/client-go/operator/applyconfigurations/operator/v1"
	"github.com/openshift/library-go/pkg/apiserver/jsonpatch"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	applyconfig "github.com/openshift/cert-manager-operator/pkg/operator/applyconfigurations/operator/v1alpha1"
	operatorconfigclient "github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned/typed/operator/v1alpha1"
	operatorclientinformers "github.com/openshift/cert-manager-operator/pkg/operator/informers/externalversions"
)

var (
	errApplyConfigurationMustHaveValue = errors.New("applyConfiguration must have a value")
)

type OperatorClient struct {
	Informers operatorclientinformers.SharedInformerFactory
	Client    operatorconfigclient.CertManagersGetter
	Clock     clock.PassiveClock
}

var _ v1helpers.OperatorClient = &OperatorClient{}

func (c OperatorClient) ApplyOperatorSpec(_ context.Context, _ string, _ *applyoperatorv1.OperatorSpecApplyConfiguration) (err error) {
	return nil
}

func (c OperatorClient) ApplyOperatorStatus(ctx context.Context, fieldManager string, desiredConfiguration *applyoperatorv1.OperatorStatusApplyConfiguration) (err error) {
	if desiredConfiguration == nil {
		return errApplyConfigurationMustHaveValue
	}

	desired := applyconfig.CertManager("cluster")
	instance, err := c.Client.CertManagers().Get(ctx, "cluster", metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("unable to get operator configuration: %w", err)
	}

	if apierrors.IsNotFound(err) {
		return c.applyStatusForNewInstance(ctx, fieldManager, desired, desiredConfiguration)
	}

	return c.applyStatusForExistingInstance(ctx, fieldManager, desired, desiredConfiguration, instance)
}

func (c OperatorClient) applyStatusForNewInstance(ctx context.Context, fieldManager string, desired *applyconfig.CertManagerApplyConfiguration, desiredConfiguration *applyoperatorv1.OperatorStatusApplyConfiguration) error {
	v1helpers.SetApplyConditionsLastTransitionTime(c.Clock, &desiredConfiguration.Conditions, nil)
	desiredStatus := &applyconfig.CertManagerStatusApplyConfiguration{
		OperatorStatusApplyConfiguration: *desiredConfiguration,
	}
	desired.WithStatus(desiredStatus)
	return c.applyStatus(ctx, fieldManager, desired)
}

func (c OperatorClient) applyStatusForExistingInstance(ctx context.Context, fieldManager string, desired *applyconfig.CertManagerApplyConfiguration, desiredConfiguration *applyoperatorv1.OperatorStatusApplyConfiguration, instance *v1alpha1.CertManager) error {
	previous, err := applyconfig.ExtractCertManagerStatus(instance, fieldManager)
	if err != nil {
		return fmt.Errorf("unable to extract operator configuration: %w", err)
	}

	operatorStatus, err := c.extractOperatorStatus(previous)
	if err != nil {
		return fmt.Errorf("failed to extract operator status: %w", err)
	}

	c.setConditionTransitionTimes(desiredConfiguration, operatorStatus)
	c.canonicalizeStatuses(desiredConfiguration, operatorStatus)

	if c.isStatusUnchanged(desiredConfiguration, operatorStatus) {
		return nil
	}

	c.prepareStatusForApply(desired, desiredConfiguration)
	return c.applyStatus(ctx, fieldManager, desired)
}

func (c OperatorClient) extractOperatorStatus(previous *applyconfig.CertManagerApplyConfiguration) (*applyoperatorv1.OperatorStatusApplyConfiguration, error) {
	operatorStatus := &applyoperatorv1.OperatorStatusApplyConfiguration{}
	if previous == nil || previous.Status == nil {
		return operatorStatus, nil
	}

	jsonBytes, err := json.Marshal(previous.Status.OperatorStatusApplyConfiguration)
	if err != nil {
		return nil, fmt.Errorf("unable to serialize operator configuration: %w", err)
	}
	if err := json.Unmarshal(jsonBytes, operatorStatus); err != nil {
		return nil, fmt.Errorf("unable to deserialize operator configuration: %w", err)
	}
	return operatorStatus, nil
}

func (c OperatorClient) setConditionTransitionTimes(desiredConfiguration, operatorStatus *applyoperatorv1.OperatorStatusApplyConfiguration) {
	if desiredConfiguration.Conditions == nil {
		return
	}
	if operatorStatus != nil {
		v1helpers.SetApplyConditionsLastTransitionTime(c.Clock, &desiredConfiguration.Conditions, operatorStatus.Conditions)
	} else {
		v1helpers.SetApplyConditionsLastTransitionTime(c.Clock, &desiredConfiguration.Conditions, nil)
	}
}

func (c OperatorClient) canonicalizeStatuses(desiredConfiguration, operatorStatus *applyoperatorv1.OperatorStatusApplyConfiguration) {
	v1helpers.CanonicalizeOperatorStatus(desiredConfiguration)
	v1helpers.CanonicalizeOperatorStatus(operatorStatus)
}

func (c OperatorClient) isStatusUnchanged(desiredConfiguration, operatorStatus *applyoperatorv1.OperatorStatusApplyConfiguration) bool {
	original := applyconfig.CertManager("cluster")
	if operatorStatus != nil {
		originalStatus := &applyconfig.CertManagerStatusApplyConfiguration{
			OperatorStatusApplyConfiguration: *operatorStatus,
		}
		original.WithStatus(originalStatus)
	}

	desired := applyconfig.CertManager("cluster")
	desiredStatus := &applyconfig.CertManagerStatusApplyConfiguration{
		OperatorStatusApplyConfiguration: *desiredConfiguration,
	}
	desired.WithStatus(desiredStatus)

	return equality.Semantic.DeepEqual(original, desired)
}

func (c OperatorClient) prepareStatusForApply(desired *applyconfig.CertManagerApplyConfiguration, desiredConfiguration *applyoperatorv1.OperatorStatusApplyConfiguration) {
	desiredStatus := &applyconfig.CertManagerStatusApplyConfiguration{
		OperatorStatusApplyConfiguration: *desiredConfiguration,
	}
	desired.WithStatus(desiredStatus)
}

func (c OperatorClient) applyStatus(ctx context.Context, fieldManager string, desired *applyconfig.CertManagerApplyConfiguration) error {
	_, err := c.Client.CertManagers().ApplyStatus(ctx, desired, metav1.ApplyOptions{
		Force:        true,
		FieldManager: fieldManager,
	})
	if err != nil {
		return fmt.Errorf("unable to Apply for operator using fieldManager %q: %w", fieldManager, err)
	}
	return nil
}

func (c OperatorClient) PatchOperatorStatus(_ context.Context, _ *jsonpatch.PatchSet) (err error) {
	return nil
}

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

func (c OperatorClient) GetOperatorStateWithQuorum(ctx context.Context) (*operatorv1.OperatorSpec, *operatorv1.OperatorStatus, string, error) {
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
	updated := original.DeepCopy()
	updated.ResourceVersion = resourceVersion
	updated.Spec.OperatorSpec = *spec

	ret, err := c.Client.CertManagers().Update(ctx, updated, metav1.UpdateOptions{})
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
	updated := original.DeepCopy()
	updated.ResourceVersion = resourceVersion
	updated.Status.OperatorStatus = *status

	ret, err := c.Client.CertManagers().UpdateStatus(ctx, updated, metav1.UpdateOptions{})
	if err != nil {
		return nil, err
	}

	return &ret.Status.OperatorStatus, nil
}

func (c OperatorClient) EnsureFinalizer(ctx context.Context, finalizer string) error {
	instance, err := c.Informers.Operator().V1alpha1().CertManagers().Lister().Get("cluster")
	if err != nil {
		return fmt.Errorf("failed to get certmanager instance: %w", err)
	}

	finalizers := instance.GetFinalizers()
	if slices.Contains(finalizers, finalizer) {
		return nil
	}

	// updating finalizers
	finalizers = append(finalizers, finalizer)
	err = c.saveFinalizers(ctx, instance, finalizers)
	if err != nil {
		return fmt.Errorf("failed to save finalizers: %w", err)
	}

	return nil
}

func (c OperatorClient) RemoveFinalizer(ctx context.Context, finalizer string) error {
	instance, err := c.Informers.Operator().V1alpha1().CertManagers().Lister().Get("cluster")
	if err != nil {
		return fmt.Errorf("failed to get certmanager instance: %w", err)
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
		return fmt.Errorf("failed to save finalizers: %w", err)
	}
	return nil
}

func (c OperatorClient) saveFinalizers(ctx context.Context, instance *v1alpha1.CertManager, finalizers []string) error {
	clone := instance.DeepCopy()
	clone.SetFinalizers(finalizers)
	_, err := c.Client.CertManagers().Update(ctx, clone, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update certmanager finalizers: %w", err)
	}
	return nil
}
