package trustmanager

import (
	"fmt"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func (r *Reconciler) createOrApplyValidatingWebhookConfiguration(tm *v1alpha1.TrustManager, resourceLabels map[string]string, trustManagerCreateRecon bool) error {
	desired := r.getValidatingWebhookConfigObject(tm, resourceLabels)

	resourceName := desired.GetName()
	r.log.V(4).Info("reconciling validating webhook configuration resource", "name", resourceName)

	fetched := &admissionregistrationv1.ValidatingWebhookConfiguration{}
	exist, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), fetched)
	if err != nil {
		return FromClientError(err, "failed to check %s validating webhook configuration already exists", resourceName)
	}

	if exist && trustManagerCreateRecon {
		r.eventRecorder.Eventf(tm, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s validating webhook configuration resource already exists, maybe from previous installation", resourceName)
	}
	if exist && hasObjectChanged(desired, fetched) {
		r.log.V(1).Info("validating webhook configuration has been modified, updating to desired state", "name", resourceName)
		if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to update %s validating webhook configuration resource", resourceName)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "validating webhook configuration resource %s reconciled back to desired state", resourceName)
	} else {
		r.log.V(4).Info("validating webhook configuration resource already exists and is in expected state", "name", resourceName)
	}
	if !exist {
		if err := r.Create(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to create %s validating webhook configuration resource", resourceName)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "validating webhook configuration resource %s created", resourceName)
	}

	return nil
}

func (r *Reconciler) getValidatingWebhookConfigObject(tm *v1alpha1.TrustManager, resourceLabels map[string]string) *admissionregistrationv1.ValidatingWebhookConfiguration {
	vwc := decodeValidatingWebhookConfigObjBytes(assets.MustAsset(validatingWebhookConfigAssetName))
	updateResourceLabels(vwc, resourceLabels)

	trustNamespace := tm.Spec.TrustManagerConfig.TrustNamespace
	if trustNamespace == "" {
		trustNamespace = defaultTrustNamespace
	}

	// Update the webhook's service namespace reference
	for i := range vwc.Webhooks {
		if vwc.Webhooks[i].ClientConfig.Service != nil {
			vwc.Webhooks[i].ClientConfig.Service.Namespace = trustNamespace
		}
	}

	// Update inject-ca-from annotation to use correct namespace
	annotations := vwc.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations["cert-manager.io/inject-ca-from"] = fmt.Sprintf("%s/trust-manager", trustNamespace)
	vwc.SetAnnotations(annotations)

	return vwc
}
