package approverpolicy

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/openshift/cert-manager-operator/pkg/controller/common"
)

func TestSecretOnlyOwnsBootstrapMetadata(t *testing.T) {
	instance := testInstance()
	instance.Spec.ControllerConfig.Annotations = map[string]string{"cert-manager.io/allow-direct-injection": "false"}
	desired := getSecretObject(instance)
	require.Equal(t, "true", desired.Annotations["cert-manager.io/allow-direct-injection"], "asset annotation overrides user annotation")

	payload, err := json.Marshal(desired)
	require.NoError(t, err)
	assert.NotContains(t, string(payload), `"data"`, "data must be entirely omitted, not just empty")
	assert.NotContains(t, string(payload), `"type"`, "type must be entirely omitted, not just empty")

	for _, tc := range []struct {
		name         string
		mutate       func(*corev1.Secret)
		wantModified bool
	}{
		{"identical object", func(*corev1.Secret) {}, false},
		{"runtime certificate data is not drift", func(s *corev1.Secret) {
			s.Data = map[string][]byte{"tls.crt": []byte("runtime certificate"), "tls.key": []byte("runtime key")}
		}, false},
		{"runtime secret type is not drift", func(s *corev1.Secret) { s.Type = corev1.SecretTypeTLS }, false},
		{"label tampered", func(s *corev1.Secret) { s.Labels["app"] = "tampered" }, true},
		{"managed label missing", func(s *corev1.Secret) { delete(s.Labels, common.ManagedResourceLabelKey) }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			existing := desired.DeepCopy()
			tc.mutate(existing)
			assert.Equal(t, tc.wantModified, resourceModified(desired, existing))
		})
	}
}
