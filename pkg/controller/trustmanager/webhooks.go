package trustmanager

import (
	"fmt"
	"maps"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
)

func (r *Reconciler) createOrApplyValidatingWebhookConfiguration(trustManager *v1alpha1.TrustManager, resourceLabels, resourceAnnotations map[string]string) error {
	desired := getValidatingWebhookConfigObject(resourceLabels, resourceAnnotations)
	resourceName := desired.GetName()
	r.log.V(4).Info("reconciling validatingwebhookconfiguration resource", "name", resourceName)

	existing := &admissionregistrationv1.ValidatingWebhookConfiguration{}
	exists, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), existing)
	if err != nil {
		return common.FromClientError(err, "failed to check if validatingwebhookconfiguration %q exists", resourceName)
	}
	if exists && !common.ValidatingWebhookConfigurationModified(desired, existing, fieldOwner) {
		r.log.V(4).Info("validatingwebhookconfiguration resource exists and is in desired state", "name", resourceName)
		return nil
	}

	r.log.V(2).Info("validatingwebhookconfiguration resource has been modified, updating to desired state", "name", resourceName)
	if err := r.Patch(r.ctx, desired, client.Apply, client.FieldOwner(fieldOwner), client.ForceOwnership); err != nil {
		return common.FromClientError(err, "failed to apply validatingwebhookconfiguration %q", resourceName)
	}

	r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "validatingwebhookconfiguration resource %s applied", resourceName)
	return nil
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
// cert-manager CA injection annotation. From trust-manager v0.25.0 the webhook
// injects CA from the TLS Secret (inject-ca-from-secret) rather than the
// Certificate resource. The annotation is constructed dynamically.
func updateWebhookAnnotations(webhookConfig *admissionregistrationv1.ValidatingWebhookConfiguration, resourceAnnotations map[string]string) {
	mergedAnnotations := make(map[string]string, len(resourceAnnotations)+1)
	maps.Copy(mergedAnnotations, resourceAnnotations)
	mergedAnnotations["cert-manager.io/inject-ca-from-secret"] = fmt.Sprintf("%s/%s", operandNamespace, trustManagerTLSSecretName)
	webhookConfig.SetAnnotations(mergedAnnotations)
}
