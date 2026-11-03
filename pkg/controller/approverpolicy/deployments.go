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

// deploymentModified compares managed fields, excluding API-defaulted pod/probe
// fields. Optional configuration is compared explicitly to detect resets.
func deploymentModified(desired, existing *appsv1.Deployment) bool {
	if managedMetadataModified(desired, existing) ||
		!ptr.Equal(desired.Spec.Replicas, existing.Spec.Replicas) ||
		!equality.Semantic.DeepEqual(desired.Spec.Selector, existing.Spec.Selector) ||
		managedMapModified(desired.Spec.Template.Labels, existing.Spec.Template.Labels, existing, "f:spec", "f:template", "f:metadata", "f:labels") ||
		managedMapModified(desired.Spec.Template.Annotations, existing.Spec.Template.Annotations, existing, "f:spec", "f:template", "f:metadata", "f:annotations") {
		return true
	}
	// Compare the container spec explicitly since the API fills in several defaults.
	a, b := &desired.Spec.Template.Spec, &existing.Spec.Template.Spec
	if a.ServiceAccountName != b.ServiceAccountName || a.HostNetwork != b.HostNetwork || a.DNSPolicy != b.DNSPolicy ||
		managedMapModified(a.NodeSelector, b.NodeSelector, existing, "f:spec", "f:template", "f:spec", "f:nodeSelector") ||
		!equality.Semantic.DeepEqual(a.Affinity, b.Affinity) ||
		!equality.Semantic.DeepEqual(a.Tolerations, b.Tolerations) ||
		!equality.Semantic.DeepEqual(a.SecurityContext, b.SecurityContext) ||
		len(a.Containers) != len(b.Containers) {
		return true
	}

	for i := range a.Containers {
		x, y := &a.Containers[i], &b.Containers[i]
		if x.Name != y.Name || x.Image != y.Image || x.ImagePullPolicy != y.ImagePullPolicy ||
			!slices.Equal(x.Args, y.Args) || !equality.Semantic.DeepEqual(x.Resources, y.Resources) ||
			!equality.Semantic.DeepEqual(x.SecurityContext, y.SecurityContext) ||
			len(x.Ports) != len(y.Ports) {
			return true
		}
		for j := range x.Ports {
			p, q := x.Ports[j], y.Ports[j]
			// Omitted protocol defaults to TCP server-side; not drift.
			if p.Name != q.Name || p.ContainerPort != q.ContainerPort || q.Protocol != "" && q.Protocol != corev1.ProtocolTCP {
				return true
			}
		}
		// Compared field-by-field since the API fills in several probe defaults.
		if x.ReadinessProbe == nil || y.ReadinessProbe == nil {
			if x.ReadinessProbe != y.ReadinessProbe {
				return true
			}
		} else {
			p, q := x.ReadinessProbe, y.ReadinessProbe
			if p.InitialDelaySeconds != q.InitialDelaySeconds || p.PeriodSeconds != q.PeriodSeconds ||
				p.HTTPGet == nil || q.HTTPGet == nil || p.HTTPGet.Path != q.HTTPGet.Path || p.HTTPGet.Port != q.HTTPGet.Port {
				return true
			}
		}
	}
	return false
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
