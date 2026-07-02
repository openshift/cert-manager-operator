package trustmanager

import (
	"fmt"
	"maps"
	"reflect"
	"slices"

	"k8s.io/utils/ptr"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
)

func (r *Reconciler) createOrApplyValidatingWebhookConfiguration(trustManager *v1alpha1.TrustManager, resourceLabels, resourceAnnotations map[string]string) error {
	desired := getValidatingWebhookConfigObject(resourceLabels, resourceAnnotations)
	existing := &admissionregistrationv1.ValidatingWebhookConfiguration{}
	return r.reconcileResourceWithSSA(trustManager, desired, existing, "validatingwebhookconfiguration", func() bool {
		return webhookConfigModified(desired, existing)
	})
}

func getValidatingWebhookConfigObject(resourceLabels, resourceAnnotations map[string]string) *admissionregistrationv1.ValidatingWebhookConfiguration {
	webhookConfig := common.DecodeObjBytes[*admissionregistrationv1.ValidatingWebhookConfiguration](codecs, admissionregistrationv1.SchemeGroupVersion, assets.MustAsset(validatingWebhookConfigAssetName))
	common.UpdateName(webhookConfig, trustManagerWebhookConfigName)
	common.UpdateResourceLabels(webhookConfig, resourceLabels)

	updateWebhookClientConfig(webhookConfig)
	updateWebhookAnnotations(webhookConfig, resourceAnnotations)

	return webhookConfig
}

// updateWebhookClientConfig sets the webhook clientConfig service name and namespace.
func updateWebhookClientConfig(webhookConfig *admissionregistrationv1.ValidatingWebhookConfiguration) {
	for i := range webhookConfig.Webhooks {
		if webhookConfig.Webhooks[i].ClientConfig.Service != nil {
			webhookConfig.Webhooks[i].ClientConfig.Service.Name = trustManagerServiceName
			webhookConfig.Webhooks[i].ClientConfig.Service.Namespace = operandNamespace
		}
	}
}

// updateWebhookAnnotations merges user-provided annotations with the required
// cert-manager CA injection annotation. The CA injection annotation references
// the Certificate resource by namespace/name and is constructed dynamically.
func updateWebhookAnnotations(webhookConfig *admissionregistrationv1.ValidatingWebhookConfiguration, resourceAnnotations map[string]string) {
	mergedAnnotations := make(map[string]string, len(resourceAnnotations)+1)
	maps.Copy(mergedAnnotations, resourceAnnotations)
	mergedAnnotations["cert-manager.io/inject-ca-from"] = fmt.Sprintf("%s/%s", operandNamespace, trustManagerCertificateName)
	webhookConfig.SetAnnotations(mergedAnnotations)
}

// webhookConfigModified compares only the fields we manage via SSA.
// Individual webhook fields are compared explicitly because the API server
// defaults fields like matchPolicy, namespaceSelector, and objectSelector.
func webhookConfigModified(desired, existing *admissionregistrationv1.ValidatingWebhookConfiguration) bool {
	if managedMetadataModified(desired, existing) {
		return true
	}
	if len(desired.Webhooks) != len(existing.Webhooks) {
		return true
	}
	for i := range desired.Webhooks {
		if validatingWebhookModified(&desired.Webhooks[i], &existing.Webhooks[i]) {
			return true
		}
	}
	return false
}

func validatingWebhookModified(desired, existing *admissionregistrationv1.ValidatingWebhook) bool {
	if desired.Name != existing.Name {
		return true
	}
	if !reflect.DeepEqual(desired.Rules, existing.Rules) ||
		!slices.Equal(desired.AdmissionReviewVersions, existing.AdmissionReviewVersions) {
		return true
	}
	if !ptr.Equal(desired.FailurePolicy, existing.FailurePolicy) ||
		!ptr.Equal(desired.SideEffects, existing.SideEffects) ||
		!ptr.Equal(desired.TimeoutSeconds, existing.TimeoutSeconds) {
		return true
	}
	if !reflect.DeepEqual(desired.ClientConfig.Service, existing.ClientConfig.Service) {
		return true
	}
	return false
}
