package approverpolicy

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/openshift/cert-manager-operator/pkg/controller/common"
)

func TestWebhookObject(t *testing.T) {
	instance := testInstance()
	instance.Spec.ControllerConfig.Annotations = map[string]string{"cert-manager.io/inject-ca-from-secret": "wrong/secret"}
	desired := getWebhookObject(instance)
	assert.Equal(t, operandNamespace+"/"+tlsSecretName, desired.Annotations["cert-manager.io/inject-ca-from-secret"],
		"the asset's cainjector annotation overrides any user-configured value")
}

func TestWebhookModified(t *testing.T) {
	desired := getWebhookObject(testInstance())
	require.Len(t, desired.Webhooks, 1, "test assumes a single webhook in the asset")
	require.Len(t, desired.Webhooks[0].Rules, 1, "test assumes a single rule in the asset")
	require.Nil(t, desired.Webhooks[0].Rules[0].Scope, "test assumes the asset leaves scope unset")

	for _, tc := range []struct {
		name         string
		mutate       func(*admissionregistrationv1.ValidatingWebhookConfiguration)
		wantModified bool
	}{
		{"identical object", func(*admissionregistrationv1.ValidatingWebhookConfiguration) {}, false},
		{"API-injected/defaulted fields are not drift", func(w *admissionregistrationv1.ValidatingWebhookConfiguration) {
			w.Webhooks[0].ClientConfig.CABundle = []byte("injected CA")
			w.Webhooks[0].ClientConfig.Service.Port = ptr.To(int32(443))
			w.Webhooks[0].Rules[0].Scope = ptr.To(admissionregistrationv1.AllScopes)
			w.Webhooks[0].MatchPolicy = ptr.To(admissionregistrationv1.Equivalent)
			w.Webhooks[0].NamespaceSelector = &metav1.LabelSelector{}
			w.Webhooks[0].ObjectSelector = &metav1.LabelSelector{}
		}, false},
		{"managed label tampered", func(w *admissionregistrationv1.ValidatingWebhookConfiguration) { w.Labels["app"] = "tampered" }, true},
		{"managed label missing", func(w *admissionregistrationv1.ValidatingWebhookConfiguration) {
			delete(w.Labels, common.ManagedResourceLabelKey)
		}, true},
		{"webhook added", func(w *admissionregistrationv1.ValidatingWebhookConfiguration) {
			w.Webhooks = append(w.Webhooks, *w.Webhooks[0].DeepCopy())
		}, true},
		{"webhook removed", func(w *admissionregistrationv1.ValidatingWebhookConfiguration) { w.Webhooks = nil }, true},
		{"webhook name changed", func(w *admissionregistrationv1.ValidatingWebhookConfiguration) { w.Webhooks[0].Name = "other" }, true},
		{"rule removed", func(w *admissionregistrationv1.ValidatingWebhookConfiguration) { w.Webhooks[0].Rules = nil }, true},
		{"rule apiGroups changed", func(w *admissionregistrationv1.ValidatingWebhookConfiguration) {
			w.Webhooks[0].Rules[0].APIGroups = []string{"other.example"}
		}, true},
		{"rule resources changed", func(w *admissionregistrationv1.ValidatingWebhookConfiguration) {
			w.Webhooks[0].Rules[0].Resources = []string{"other"}
		}, true},
		{"rule operations changed", func(w *admissionregistrationv1.ValidatingWebhookConfiguration) {
			w.Webhooks[0].Rules[0].Operations = []admissionregistrationv1.OperationType{admissionregistrationv1.Delete}
		}, true},
		{"admissionReviewVersions changed", func(w *admissionregistrationv1.ValidatingWebhookConfiguration) {
			w.Webhooks[0].AdmissionReviewVersions = []string{"v2"}
		}, true},
		{"failurePolicy changed", func(w *admissionregistrationv1.ValidatingWebhookConfiguration) {
			w.Webhooks[0].FailurePolicy = ptr.To(admissionregistrationv1.Ignore)
		}, true},
		{"sideEffects changed", func(w *admissionregistrationv1.ValidatingWebhookConfiguration) {
			w.Webhooks[0].SideEffects = ptr.To(admissionregistrationv1.SideEffectClassSome)
		}, true},
		{"timeoutSeconds changed", func(w *admissionregistrationv1.ValidatingWebhookConfiguration) {
			w.Webhooks[0].TimeoutSeconds = ptr.To(int32(30))
		}, true},
		{"clientConfig.URL added", func(w *admissionregistrationv1.ValidatingWebhookConfiguration) {
			w.Webhooks[0].ClientConfig.URL = ptr.To("https://example.test/webhook")
		}, true},
		{"clientConfig.Service removed", func(w *admissionregistrationv1.ValidatingWebhookConfiguration) {
			w.Webhooks[0].ClientConfig.Service = nil
		}, true},
		{"clientConfig.Service.Name changed", func(w *admissionregistrationv1.ValidatingWebhookConfiguration) {
			w.Webhooks[0].ClientConfig.Service.Name = "other"
		}, true},
		{"clientConfig.Service.Namespace changed", func(w *admissionregistrationv1.ValidatingWebhookConfiguration) {
			w.Webhooks[0].ClientConfig.Service.Namespace = "other"
		}, true},
		{"clientConfig.Service.Path changed", func(w *admissionregistrationv1.ValidatingWebhookConfiguration) {
			w.Webhooks[0].ClientConfig.Service.Path = ptr.To("/other")
		}, true},
		{"clientConfig.Service.Port set to a non-default value", func(w *admissionregistrationv1.ValidatingWebhookConfiguration) {
			w.Webhooks[0].ClientConfig.Service.Port = ptr.To(int32(8443))
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			existing := desired.DeepCopy()
			tc.mutate(existing)
			assert.Equal(t, tc.wantModified, webhookModified(desired, existing))
		})
	}

	t.Run("explicit non-default scope mismatch is drift", func(t *testing.T) {
		explicitScope := desired.DeepCopy()
		explicitScope.Webhooks[0].Rules[0].Scope = ptr.To(admissionregistrationv1.NamespacedScope)
		existing := explicitScope.DeepCopy()
		existing.Webhooks[0].Rules[0].Scope = ptr.To(admissionregistrationv1.AllScopes)
		assert.True(t, webhookModified(explicitScope, existing),
			"when the asset explicitly sets a scope, a differing existing scope must be treated as drift")
	})
}
