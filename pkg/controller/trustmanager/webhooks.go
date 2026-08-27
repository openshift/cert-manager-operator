package trustmanager

import (
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func (r *Reconciler) createOrApplyWebhooks(trustManager *v1alpha1.TrustManager, resourceLabels map[string]string, trustManagerCreateRecon bool) error {
	desired := r.getWebhookObject(resourceLabels)

	webhookName := desired.GetName()
	r.log.V(4).Info("reconciling validatingwebhookconfiguration resource", "name", webhookName)
	fetched := &admissionregistrationv1.ValidatingWebhookConfiguration{}
	exist, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), fetched)
	if err != nil {
		return fromClientError(err, "failed to check %s validatingwebhookconfiguration resource already exists", webhookName)
	}

	if exist && trustManagerCreateRecon {
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s validatingwebhookconfiguration resource already exists, maybe from previous installation", webhookName)
	}
	if exist && hasObjectChanged(desired, fetched) {
		r.log.V(1).Info("validatingwebhookconfiguration has been modified, updating to desired state", "name", webhookName)
		if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
			return fromClientError(err, "failed to update %s validatingwebhookconfiguration resource", webhookName)
		}
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "validatingwebhookconfiguration resource %s reconciled back to desired state", webhookName)
	} else {
		r.log.V(4).Info("validatingwebhookconfiguration resource already exists and is in expected state", "name", webhookName)
	}
	if !exist {
		if err := r.Create(r.ctx, desired); err != nil {
			return fromClientError(err, "failed to create %s validatingwebhookconfiguration resource", webhookName)
		}
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "validatingwebhookconfiguration resource %s created", webhookName)
	}

	return nil
}

func (r *Reconciler) getWebhookObject(resourceLabels map[string]string) *admissionregistrationv1.ValidatingWebhookConfiguration {
	webhook := decodeValidatingWebhookConfigurationObjBytes(assets.MustAsset(webhookAssetName))
	updateResourceLabels(webhook, resourceLabels)
	return webhook
}
