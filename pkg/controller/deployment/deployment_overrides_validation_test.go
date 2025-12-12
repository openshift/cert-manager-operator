package deployment

import (
	"testing"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/stretchr/testify/assert"

	corev1 "k8s.io/api/core/v1"
	k8sresource "k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

func TestValidateResources(t *testing.T) {
	tests := []struct {
		name                   string
		resources              v1alpha1.CertManagerResourceRequirements
		resourceNamesSupported []string
		errorExpected          bool
	}{
		{
			name: "validate cpu resource name in resources limits",
			resources: v1alpha1.CertManagerResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU: k8sresource.MustParse("10m"),
				},
			},
			resourceNamesSupported: []string{string(corev1.ResourceCPU)},
			errorExpected:          false,
		},
		{
			name: "validate memory resource name in resources limits",
			resources: v1alpha1.CertManagerResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: k8sresource.MustParse("32Mi"),
				},
			},
			resourceNamesSupported: []string{string(corev1.ResourceMemory)},
			errorExpected:          false,
		},
		{
			name: "validate cpu and memory resource names in resources limits",
			resources: v1alpha1.CertManagerResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("10m"),
					corev1.ResourceMemory: k8sresource.MustParse("32Mi"),
				},
			},
			resourceNamesSupported: []string{string(corev1.ResourceCPU), string(corev1.ResourceMemory)},
			errorExpected:          false,
		},
		{
			name: "validate cpu resource name in resources requests",
			resources: v1alpha1.CertManagerResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU: k8sresource.MustParse("10m"),
				},
			},
			resourceNamesSupported: []string{string(corev1.ResourceCPU)},
			errorExpected:          false,
		},
		{
			name: "validate memory resource name in resources requests",
			resources: v1alpha1.CertManagerResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: k8sresource.MustParse("32Mi"),
				},
			},
			resourceNamesSupported: []string{string(corev1.ResourceMemory)},
			errorExpected:          false,
		},
		{
			name: "validate cpu and memory resource names in resources requests",
			resources: v1alpha1.CertManagerResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("10m"),
					corev1.ResourceMemory: k8sresource.MustParse("32Mi"),
				},
			},
			resourceNamesSupported: []string{string(corev1.ResourceCPU), string(corev1.ResourceMemory)},
			errorExpected:          false,
		},
		{
			name: "validate cpu and memory resource names in resources limits and requests",
			resources: v1alpha1.CertManagerResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("10m"),
					corev1.ResourceMemory: k8sresource.MustParse("32Mi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("10m"),
					corev1.ResourceMemory: k8sresource.MustParse("32Mi"),
				},
			},
			resourceNamesSupported: []string{string(corev1.ResourceCPU), string(corev1.ResourceMemory)},
			errorExpected:          false,
		},
		{
			name: "unsupported resource name in resources limits and requests should return error",
			resources: v1alpha1.CertManagerResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceEphemeralStorage: k8sresource.MustParse("10Gi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceEphemeralStorage: k8sresource.MustParse("10Gi"),
				},
			},
			resourceNamesSupported: []string{string(corev1.ResourceCPU), string(corev1.ResourceMemory)},
			errorExpected:          true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateResources(tc.resources, tc.resourceNamesSupported)
			if tc.errorExpected {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateScheduling(t *testing.T) {
	tests := []struct {
		name          string
		scheduling    v1alpha1.CertManagerScheduling
		errorExpected bool
	}{
		{
			name: "valid node selector should be accepted",
			scheduling: v1alpha1.CertManagerScheduling{
				NodeSelector: map[string]string{
					"nodeLabel1": "value1",
					"nodeLabel2": "value2",
				},
			},
			errorExpected: false,
		},
		{
			name: "invalid node selector should not be accepted",
			scheduling: v1alpha1.CertManagerScheduling{
				NodeSelector: map[string]string{
					"/nodeLabel1":  "value1",
					"node/Label/2": "value2",
					"":             "value3",
				},
			},
			errorExpected: true,
		},
		{
			name: "valid tolerations should be accepted",
			scheduling: v1alpha1.CertManagerScheduling{
				Tolerations: []corev1.Toleration{
					{
						Key:      "tolerationKey1",
						Operator: "Exists",
						Effect:   "NoSchedule",
					},
					{
						Key:      "tolerationKey2",
						Operator: "Equal",
						Value:    "value2",
						Effect:   "NoSchedule",
					},
					{
						Key:      "tolerationKey3",
						Operator: "Equal",
						Effect:   "NoSchedule",
					},
				},
			},
			errorExpected: false,
		},
		{
			name: "invalid tolerations should not be accepted",
			scheduling: v1alpha1.CertManagerScheduling{
				Tolerations: []corev1.Toleration{
					{
						Key:      "tolerationKey1",
						Operator: "Exists",
						Value:    "value1",
						Effect:   "NoSchedule",
					},
					{
						Key:      "",
						Operator: "Equal",
						Effect:   "NoSchedule",
					},
				},
			},
			errorExpected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateScheduling(tc.scheduling, field.NewPath("overridesScheduling"))
			if tc.errorExpected {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
