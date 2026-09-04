package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/kubernetes/pkg/apis/core"
	"k8s.io/utils/ptr"
)

func TestToCoreTolerations_RoundTrip(t *testing.T) {
	input := []corev1.Toleration{
		{Key: "key1", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoSchedule},
		{Key: "key2", Operator: corev1.TolerationOpEqual, Value: "val2", Effect: corev1.TaintEffectNoExecute, TolerationSeconds: ptr.To(int64(300))},
	}
	core := ToCoreTolerations(input)
	require.Len(t, core, 2)
	roundTripped := ToV1Tolerations(core)
	require.Equal(t, input, roundTripped)
}

func TestToCoreTolerations_Nil(t *testing.T) {
	require.Empty(t, ToCoreTolerations(nil))
	require.Empty(t, ToV1Tolerations(nil))
}

func TestToCoreTolerations_Empty(t *testing.T) {
	require.Empty(t, ToCoreTolerations([]corev1.Toleration{}))
	require.Empty(t, ToV1Tolerations([]core.Toleration{}))
}

func TestValidateLabelsConfig(t *testing.T) {
	fldPath := field.NewPath("spec")

	t.Run("valid labels pass", func(t *testing.T) {
		labels := map[string]string{
			"app":                      "test",
			"example.com/my-component": "frontend",
		}
		err := ValidateLabelsConfig(labels, fldPath)
		require.NoError(t, err)
	})

	t.Run("invalid label key returns error", func(t *testing.T) {
		labels := map[string]string{
			"INVALID KEY WITH SPACES": "value",
		}
		err := ValidateLabelsConfig(labels, fldPath)
		require.Error(t, err)
	})

	t.Run("empty labels pass", func(t *testing.T) {
		err := ValidateLabelsConfig(map[string]string{}, fldPath)
		require.NoError(t, err)
	})
}

func TestValidateAnnotationsConfig(t *testing.T) {
	fldPath := field.NewPath("spec")

	t.Run("valid annotations pass", func(t *testing.T) {
		annotations := map[string]string{
			"kubectl.kubernetes.io/last-applied-configuration": "{}",
			"my-annotation": "some-value",
		}
		err := ValidateAnnotationsConfig(annotations, fldPath)
		require.NoError(t, err)
	})

	t.Run("invalid annotation key returns error", func(t *testing.T) {
		annotations := map[string]string{
			"invalid key!@#$": "value",
		}
		err := ValidateAnnotationsConfig(annotations, fldPath)
		require.Error(t, err)
	})
}

func TestValidateNodeSelectorConfig(t *testing.T) {
	fldPath := field.NewPath("spec")

	t.Run("valid nodeSelector passes", func(t *testing.T) {
		nodeSelector := map[string]string{
			"kubernetes.io/os": "linux",
			"node-role":        "worker",
		}
		err := ValidateNodeSelectorConfig(nodeSelector, fldPath)
		require.NoError(t, err)
	})

	t.Run("empty value key passes", func(t *testing.T) {
		nodeSelector := map[string]string{
			"node.kubernetes.io/instance-type": "",
		}
		err := ValidateNodeSelectorConfig(nodeSelector, fldPath)
		require.NoError(t, err)
	})

	t.Run("invalid key returns error", func(t *testing.T) {
		nodeSelector := map[string]string{
			"BAD KEY!!!": "value",
		}
		err := ValidateNodeSelectorConfig(nodeSelector, fldPath)
		require.Error(t, err)
	})
}

func TestValidateTolerationsConfig(t *testing.T) {
	fldPath := field.NewPath("spec")

	t.Run("valid tolerations pass", func(t *testing.T) {
		tolerations := []corev1.Toleration{
			{
				Key:      "node.kubernetes.io/not-ready",
				Operator: corev1.TolerationOpExists,
				Effect:   corev1.TaintEffectNoSchedule,
			},
			{
				Key:      "dedicated",
				Operator: corev1.TolerationOpEqual,
				Value:    "cert-manager",
				Effect:   corev1.TaintEffectNoSchedule,
			},
		}
		err := ValidateTolerationsConfig(tolerations, fldPath)
		require.NoError(t, err)
	})

	t.Run("invalid operator returns error", func(t *testing.T) {
		tolerations := []corev1.Toleration{
			{
				Key:      "key",
				Operator: corev1.TolerationOperator("InvalidOp"),
				Effect:   corev1.TaintEffectNoSchedule,
			},
		}
		err := ValidateTolerationsConfig(tolerations, fldPath)
		require.Error(t, err)
	})

	t.Run("empty tolerations pass", func(t *testing.T) {
		err := ValidateTolerationsConfig([]corev1.Toleration{}, fldPath)
		require.NoError(t, err)
	})
}

func TestValidateResourceRequirements(t *testing.T) {
	fldPath := field.NewPath("spec")

	t.Run("valid cpu and memory pass", func(t *testing.T) {
		reqs := corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
		}
		err := ValidateResourceRequirements(reqs, fldPath)
		require.NoError(t, err)
	})

	t.Run("negative cpu returns error", func(t *testing.T) {
		reqs := corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU: resource.MustParse("-100m"),
			},
		}
		err := ValidateResourceRequirements(reqs, fldPath)
		require.Error(t, err)
	})

	t.Run("empty requirements pass", func(t *testing.T) {
		err := ValidateResourceRequirements(corev1.ResourceRequirements{}, fldPath)
		require.NoError(t, err)
	})
}

func TestValidateAffinityRules(t *testing.T) {
	fldPath := field.NewPath("spec")

	t.Run("nil affinity passes", func(t *testing.T) {
		err := ValidateAffinityRules(nil, fldPath)
		require.NoError(t, err)
	})

	t.Run("valid node affinity passes", func(t *testing.T) {
		affinity := &corev1.Affinity{
			NodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								{
									Key:      "kubernetes.io/os",
									Operator: corev1.NodeSelectorOpIn,
									Values:   []string{"linux"},
								},
							},
						},
					},
				},
			},
		}
		err := ValidateAffinityRules(affinity, fldPath)
		require.NoError(t, err)
	})

	t.Run("invalid label selector in node affinity returns error", func(t *testing.T) {
		affinity := &corev1.Affinity{
			NodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								{
									Key:      "kubernetes.io/os",
									Operator: corev1.NodeSelectorOperator("BadOp"),
									Values:   []string{"linux"},
								},
							},
						},
					},
				},
			},
		}
		err := ValidateAffinityRules(affinity, fldPath)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not a valid selector operator")
	})
}
