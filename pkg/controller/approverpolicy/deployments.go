package approverpolicy

import (
	"fmt"
	"maps"
	"os"
	"slices"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/ptr"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

// getDeploymentObject builds the desired approver-policy Deployment. serviceAccountUID, if
// non-empty, is stamped onto the pod template: recreating the ServiceAccount invalidates
// projected tokens in existing pods, so a changed UID must roll the Deployment.
func getDeploymentObject(instance *v1alpha1.ApproverPolicyManager, serviceAccountUID types.UID) (*appsv1.Deployment, error) {
	// Fail fast on invalid config before touching assets.
	if err := validateConfig(instance); err != nil {
		return nil, common.NewIrrecoverableError(err, "invalid approver-policy configuration")
	}

	image := os.Getenv(imageEnvVar)
	if image == "" {
		return nil, common.NewIrrecoverableError(fmt.Errorf("%s is not set", imageEnvVar), "missing approver-policy image")
	}
	obj := common.DecodeObjBytes[*appsv1.Deployment](codecs, appsv1.SchemeGroupVersion, assets.MustAsset(assetName("deployment", operandName)))
	// Fail closed if the asset doesn't match our container assumptions.
	if len(obj.Spec.Template.Spec.Containers) != 1 || obj.Spec.Template.Spec.Containers[0].Name != operandName {
		return nil, common.NewIrrecoverableError(fmt.Errorf("expected container %q", operandName), "invalid approver-policy deployment asset")
	}

	prepareResource(instance, obj)
	obj.Spec.Template.Labels = maps.Clone(obj.Labels)
	obj.Spec.Template.Annotations = maps.Clone(instance.Spec.ControllerConfig.Annotations)
	config := instance.Spec.ApproverPolicyConfig

	logLevel, logFormat := config.LogLevel, config.LogFormat
	if logLevel == 0 {
		logLevel = 1
	}
	if logFormat == "" {
		logFormat = "text"
	}

	container := &obj.Spec.Template.Spec.Containers[0]
	container.Image = image
	// Merge operator-managed log flags into the asset's args; overrides win.
	container.Args = common.MergeContainerArgs(
		container.Args,
		[]string{
			fmt.Sprintf("--log-level=%d", logLevel),
			"--log-format=" + logFormat,
		},
	)

	container.Resources = *config.Resources.DeepCopy()
	obj.Spec.Template.Spec.Affinity = config.Affinity.DeepCopy()
	obj.Spec.Template.Spec.Tolerations = slices.Clone(config.Tolerations)
	maps.Copy(obj.Spec.Template.Spec.NodeSelector, config.NodeSelector)

	if serviceAccountUID != "" {
		if obj.Spec.Template.Annotations == nil {
			obj.Spec.Template.Annotations = map[string]string{}
		}
		obj.Spec.Template.Annotations[serviceAccountUIDAnnotation] = string(serviceAccountUID)
	}
	return obj, nil
}

// validateConfig checks configuration the CRD schema can't validate structurally.
// logLevel/logFormat are skipped: the CRD already enforces their bounds, and
// zero values are treated as "unset" and defaulted in getDeploymentObject.
func validateConfig(instance *v1alpha1.ApproverPolicyManager) error {
	config := instance.Spec.ApproverPolicyConfig
	configPath := field.NewPath("spec", "approverPolicyConfig")
	controllerPath := field.NewPath("spec", "controllerConfig")
	checks := []error{
		common.ValidateResourceRequirements(config.Resources, configPath),
		common.ValidateAffinityRules(config.Affinity, configPath),
		common.ValidateTolerationsConfig(config.Tolerations, configPath),
		common.ValidateNodeSelectorConfig(config.NodeSelector, configPath),
		common.ValidateLabelsConfig(instance.Spec.ControllerConfig.Labels, controllerPath),
		common.ValidateAnnotationsConfig(instance.Spec.ControllerConfig.Annotations, controllerPath),
	}
	for _, err := range checks {
		if err != nil {
			return err
		}
	}
	return nil
}

// deploymentModified extends the shared comparison with the pod networking and
// security fields explicitly managed by approver-policy.
func deploymentModified(desired, existing *appsv1.Deployment) bool {
	if common.DeploymentModified(desired, existing, fieldOwner) {
		return true
	}
	a, b := &desired.Spec.Template.Spec, &existing.Spec.Template.Spec
	return a.HostNetwork != b.HostNetwork || a.DNSPolicy != b.DNSPolicy ||
		!equality.Semantic.DeepEqual(a.SecurityContext, b.SecurityContext)
}

// deploymentReady reports whether the last rollout finished: generation observed,
// replicas fully updated/ready/available, and the Available condition is true.
func deploymentReady(obj *appsv1.Deployment) bool {
	replicas := ptr.Deref(obj.Spec.Replicas, 1)
	if !obj.DeletionTimestamp.IsZero() || replicas < 1 || obj.Status.ObservedGeneration < obj.Generation ||
		obj.Status.Replicas != replicas || obj.Status.UpdatedReplicas != replicas ||
		obj.Status.ReadyReplicas != replicas || obj.Status.AvailableReplicas != replicas ||
		obj.Status.UnavailableReplicas != 0 {
		return false
	}
	for _, condition := range obj.Status.Conditions {
		if condition.Type == appsv1.DeploymentAvailable && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}
