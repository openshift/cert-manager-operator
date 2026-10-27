package approverpolicy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/openshift/cert-manager-operator/pkg/controller/common"
)

func TestServiceModified(t *testing.T) {
	for _, obj := range getServiceObjects(testInstance()) {
		desired := obj.(*corev1.Service)
		t.Run(desired.Name, func(t *testing.T) {
			require.Len(t, desired.Spec.Ports, 1, "test assumes a single port asset")
			require.NotEmpty(t, desired.Spec.Selector, "test assumes a non-empty selector asset")

			for _, tc := range []struct {
				name         string
				mutate       func(*corev1.Service)
				wantModified bool
			}{
				{"identical object", func(*corev1.Service) {}, false},
				{"allocated fields are not drift", func(s *corev1.Service) {
					s.Spec.ClusterIP = "10.0.0.1"
					s.Spec.ClusterIPs = []string{"10.0.0.1"}
					s.Spec.IPFamilies = []corev1.IPFamily{corev1.IPv4Protocol}
					s.Spec.SessionAffinity = corev1.ServiceAffinityNone
				}, false},
				{"port number changed", func(s *corev1.Service) { s.Spec.Ports[0].Port++ }, true},
				{"port protocol changed", func(s *corev1.Service) { s.Spec.Ports[0].Protocol = corev1.ProtocolUDP }, true},
				{"port target changed", func(s *corev1.Service) { s.Spec.Ports[0].TargetPort.IntVal++ }, true},
				{"port added", func(s *corev1.Service) {
					s.Spec.Ports = append(s.Spec.Ports, corev1.ServicePort{Name: "extra", Port: 1234, Protocol: corev1.ProtocolTCP})
				}, true},
				{"port removed", func(s *corev1.Service) { s.Spec.Ports = nil }, true},
				{"type changed", func(s *corev1.Service) { s.Spec.Type = corev1.ServiceTypeLoadBalancer }, true},
				{"selector key added", func(s *corev1.Service) { s.Spec.Selector["extra"] = "value" }, true},
				{"selector value changed", func(s *corev1.Service) {
					for k := range s.Spec.Selector {
						s.Spec.Selector[k] = "different"
					}
				}, true},
				{"managed label missing", func(s *corev1.Service) { delete(s.Labels, common.ManagedResourceLabelKey) }, true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					existing := desired.DeepCopy()
					tc.mutate(existing)
					assert.Equal(t, tc.wantModified, serviceModified(desired, existing))
				})
			}
		})
	}
}
