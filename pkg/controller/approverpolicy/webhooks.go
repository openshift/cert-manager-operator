package approverpolicy

import (
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/utils/ptr"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func getWebhookObject(instance *v1alpha1.ApproverPolicyManager) *admissionregistrationv1.ValidatingWebhookConfiguration {
	obj := common.DecodeObjBytes[*admissionregistrationv1.ValidatingWebhookConfiguration](codecs, admissionregistrationv1.SchemeGroupVersion, assets.MustAsset(assetName("validatingwebhookconfiguration", operandName)))
	prepareResource(instance, obj)
	return obj
}

func webhookModified(desired, existing *admissionregistrationv1.ValidatingWebhookConfiguration) bool {
	if managedMetadataModified(desired, existing) || len(desired.Webhooks) != len(existing.Webhooks) {
		return true
	}
	for i := range desired.Webhooks {
		a, b := &desired.Webhooks[i], &existing.Webhooks[i]
		rules := a.DeepCopy().Rules
		for j := range rules {
			if j < len(b.Rules) && rules[j].Scope == nil {
				// Scope is not set in the asset; the API defaults it to "*".
				rules[j].Scope = b.Rules[j].Scope
			}
		}
		if a.Name != b.Name ||
			!equality.Semantic.DeepEqual(rules, b.Rules) ||
			!equality.Semantic.DeepEqual(a.AdmissionReviewVersions, b.AdmissionReviewVersions) ||
			!ptr.Equal(a.FailurePolicy, b.FailurePolicy) || !ptr.Equal(a.SideEffects, b.SideEffects) ||
			!ptr.Equal(a.TimeoutSeconds, b.TimeoutSeconds) || !ptr.Equal(a.ClientConfig.URL, b.ClientConfig.URL) {
			return true
		}
		// caBundle is injected by cainjector. Service.port defaults to 443.
		if a.ClientConfig.Service == nil || b.ClientConfig.Service == nil {
			if a.ClientConfig.Service != b.ClientConfig.Service {
				return true
			}
		} else {
			x, y := a.ClientConfig.Service, b.ClientConfig.Service
			if x.Name != y.Name || x.Namespace != y.Namespace || !ptr.Equal(x.Path, y.Path) ||
				ptr.Deref(x.Port, 443) != ptr.Deref(y.Port, 443) {
				return true
			}
		}
	}
	return false
}
