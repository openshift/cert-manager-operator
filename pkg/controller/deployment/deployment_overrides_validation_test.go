package deployment

import (
	"testing"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/stretchr/testify/assert"

	corev1 "k8s.io/api/core/v1"
	k8sresource "k8s.io/apimachinery/pkg/api/resource"
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
