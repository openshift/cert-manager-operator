package approverpolicy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"github.com/openshift/cert-manager-operator/pkg/controller/common"
)

func TestServiceAccountModified(t *testing.T) {
	desired := getServiceAccountObject(testInstance())

	for _, tc := range []struct {
		name         string
		mutate       func(*corev1.ServiceAccount)
		wantModified bool
	}{
		{"identical object", func(*corev1.ServiceAccount) {}, false},
		{"generated fields are not drift", func(sa *corev1.ServiceAccount) {
			sa.Secrets = []corev1.ObjectReference{{Name: "generated-token"}}
			sa.ImagePullSecrets = []corev1.LocalObjectReference{{Name: "cluster-pull-secret"}}
		}, false},
		{"label tampered", func(sa *corev1.ServiceAccount) { sa.Labels["app"] = "tampered" }, true},
		{"managed label missing", func(sa *corev1.ServiceAccount) { delete(sa.Labels, common.ManagedResourceLabelKey) }, true},
		{"automount enabled", func(sa *corev1.ServiceAccount) { sa.AutomountServiceAccountToken = ptr.To(true) }, true},
		{"automount disabled", func(sa *corev1.ServiceAccount) { sa.AutomountServiceAccountToken = ptr.To(false) }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			existing := desired.DeepCopy()
			tc.mutate(existing)
			assert.Equal(t, tc.wantModified, serviceAccountModified(desired, existing))
		})
	}
}
