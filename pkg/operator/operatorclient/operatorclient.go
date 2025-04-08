package operatorclient

import (
	"context"
	"encoding/json"
	"fmt"
	applyconfig "github.com/openshift/cert-manager-operator/pkg/operator/applyconfigurations/operator/v1alpha1"
	applyoperatorv1 "github.com/openshift/client-go/operator/applyconfigurations/operator/v1"

	//v1 "github.com/openshift/client-go/operator/applyconfigurations/operator/v1"

	"github.com/openshift/library-go/pkg/apiserver/jsonpatch"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/clock"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	operatorconfigclient "github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned/typed/operator/v1alpha1"
	operatorclientinformers "github.com/openshift/cert-manager-operator/pkg/operator/informers/externalversions"

	"github.com/openshift/library-go/pkg/operator/v1helpers"
)

type OperatorClient struct {
	Informers operatorclientinformers.SharedInformerFactory
	Client    operatorconfigclient.CertManagersGetter
	clock     clock.PassiveClock
}

var _ v1helpers.OperatorClient = &OperatorClient{}

func (c OperatorClient) ApplyOperatorSpec(ctx context.Context, fieldManager string, applyConfiguration *applyoperatorv1.OperatorSpecApplyConfiguration) (err error) {
	if applyConfiguration == nil {
		return fmt.Errorf("applyConfiguration must have a value")
	}

	desired := applyconfig.CertManager("cluster")
	desired.Spec.OperatorLogLevel = applyConfiguration.OperatorLogLevel
	desired.Spec.LogLevel = applyConfiguration.LogLevel
	desired.Spec.ManagementState = applyConfiguration.ManagementState
	desired.Spec.UnsupportedConfigOverrides = applyConfiguration.UnsupportedConfigOverrides
	desired.Spec.ObservedConfig = applyConfiguration.ObservedConfig
	instance, err := c.Client.CertManagers().Get(ctx, "cluster", metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
	// do nothing and proceed with the apply
	case err != nil:
		return fmt.Errorf("unable to get operator configuration: %w", err)
	default:
		original := &applyconfig.CertManagerApplyConfiguration{}
		//err := managedfields.ExtractInto(instance, internal.Parser().Type("com.github.openshift.cert-manager-operator.api.v1.CertManager"), fieldManager, original, "")
		//if err != nil {
		//	return nil
		//}
		original.WithName(instance.Name)

		original.WithKind(instance.Kind)
		original.WithAPIVersion(instance.APIVersion)

		if equality.Semantic.DeepEqual(original, desired) {
			return nil
		}
	}

	_, err = c.Client.CertManagers().Apply(ctx, desired, metav1.ApplyOptions{
		Force:        true,
		FieldManager: fieldManager,
	})
	if err != nil {
		return fmt.Errorf("unable to Apply for operator using fieldManager %q: %w", fieldManager, err)
	}

	return nil
}

func (c OperatorClient) ApplyOperatorStatus(ctx context.Context, fieldManager string, applyConfiguration *applyoperatorv1.OperatorStatusApplyConfiguration) (err error) {
	if applyConfiguration == nil {

		return fmt.Errorf("applyConfiguration must have a value")
	}

	desired := applyconfig.CertManager("cluster")
	//desired.Status.Conditions = make([]operatorv1.OperatorCondition, len(applyConfiguration.Conditions))
	//for i := range applyConfiguration.Conditions {
	//	desired.Status.Conditions[i] = operatorv1.OperatorCondition{
	//		Type:               *applyConfiguration.Conditions[i].Type,
	//		Status:             *applyConfiguration.Conditions[i].Status,
	//		LastTransitionTime: *applyConfiguration.Conditions[i].LastTransitionTime,
	//		Reason:             *applyConfiguration.Conditions[i].Reason,
	//		Message:            *applyConfiguration.Conditions[i].Message,
	//	}
	//}

	desired.Status.Conditions = applyConfiguration.Conditions
	desired.Status.ObservedGeneration = applyConfiguration.ObservedGeneration
	desired.Status.LatestAvailableRevision = applyConfiguration.LatestAvailableRevision
	desired.Status.ReadyReplicas = applyConfiguration.ReadyReplicas
	desired.Status.Version = applyConfiguration.Version
	instance, err := c.Client.CertManagers().Get(ctx, "cluster", metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		// do nothing and proceed with the apply
		v1helpers.SetApplyConditionsLastTransitionTime(c.clock, &applyConfiguration.Conditions, nil)
		ts := c.clock.Now()
		for i := range desired.Status.Conditions {
			desired.Status.Conditions[i].LastTransitionTime = &metav1.Time{Time: ts}
		}

	case err != nil:
		return fmt.Errorf("unable to get operator configuration: %w", err)
	default:
		//previous, err := v1.ExtractOpenShiftControllerManagerStatus(instance, fieldManager)
		//if err != nil {
		//	return fmt.Errorf("unable to extract operator configuration: %w", err)
		//}

		//operatorStatus := &v1.OperatorStatusApplyConfiguration{}
		//if previous.Status != nil {
		//	jsonBytes, err := json.Marshal(previous.Status)
		//	if err != nil {
		//		return fmt.Errorf("unable to serialize operator configuration: %w", err)
		//	}
		//	if err := json.Unmarshal(jsonBytes, operatorStatus); err != nil {
		//		return fmt.Errorf("unable to deserialize operator configuration: %w", err)
		//	}
		//}

		//switch {
		//case applyConfiguration.Conditions != nil && operatorStatus != nil:
		//	v1helpers.SetApplyConditionsLastTransitionTime(c.clock, &applyConfiguration.Conditions, operatorStatus.Conditions)
		//case applyConfiguration.Conditions != nil && operatorStatus == nil:
		//	v1helpers.SetApplyConditionsLastTransitionTime(c.clock, &applyConfiguration.Conditions, nil)
		//}

		//v1helpers.CanonicalizeOperatorStatus(applyConfiguration)
		//v1helpers.CanonicalizeOperatorStatus(operatorStatus)
		original := &applyconfig.CertManagerApplyConfiguration{}
		original.WithName(instance.Name)

		original.WithKind(instance.Kind)
		original.WithAPIVersion(instance.APIVersion)

		//if equality.Semantic.DeepEqual(original, desired) {
		//	return nil
		//}
		//original := v1.OpenShiftControllerManager("cluster")
		//if operatorStatus != nil {
		//	originalStatus := &v1.OpenShiftControllerManagerStatusApplyConfiguration{
		//		OperatorStatusApplyConfiguration: *operatorStatus,
		//	}
		//	original.WithStatus(originalStatus)
		//}
		//
		//desiredStatus := &v1.OpenShiftControllerManagerStatusApplyConfiguration{
		//	OperatorStatusApplyConfiguration: *applyConfiguration,
		//}
		//desired.WithStatus(desiredStatus)

		if equality.Semantic.DeepEqual(original, desired) {
			return nil
		}
	}

	_, err = c.Client.CertManagers().ApplyStatus(ctx, desired, metav1.ApplyOptions{
		Force:        true,
		FieldManager: fieldManager,
	})
	if err != nil {
		return fmt.Errorf("unable to Apply for operator using fieldManager %q: %w", fieldManager, err)
	}

	return nil
}

func (c OperatorClient) PatchOperatorStatus(ctx context.Context, jsonPatch *jsonpatch.PatchSet) (err error) {
	jsonPatchBytes, err := jsonPatch.Marshal()
	if err != nil {
		return err
	}
	_, err = c.Client.CertManagers().Patch(ctx, "cluster", types.JSONPatchType, jsonPatchBytes, metav1.PatchOptions{}, "/status")
	return err
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
