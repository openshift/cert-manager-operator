package trustmanager

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func (r *Reconciler) createOrApplyWebhookIssuers(tm *v1alpha1.TrustManager, resourceLabels map[string]string, trustManagerCreateRecon bool) error {
	desired := r.getIssuerObject(tm, resourceLabels)

	issuerName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(4).Info("reconciling webhook issuer resource", "name", issuerName)

	fetched := &certmanagerv1.Issuer{}
	exist, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), fetched)
	if err != nil {
		return FromClientError(err, "failed to check %s issuer resource already exists", issuerName)
	}

	if exist && trustManagerCreateRecon {
		r.eventRecorder.Eventf(tm, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s issuer resource already exists, maybe from previous installation", issuerName)
	}
	if exist && hasObjectChanged(desired, fetched) {
		r.log.V(1).Info("issuer has been modified, updating to desired state", "name", issuerName)
		if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to update %s issuer resource", issuerName)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "issuer resource %s reconciled back to desired state", issuerName)
	} else {
		r.log.V(4).Info("issuer resource already exists and is in expected state", "name", issuerName)
	}
	if !exist {
		if err := r.Create(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to create %s issuer resource", issuerName)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "issuer resource %s created", issuerName)
	}

	return nil
}

func (r *Reconciler) createOrApplyCertificates(tm *v1alpha1.TrustManager, resourceLabels map[string]string, trustManagerCreateRecon bool) error {
	desired := r.getCertificateObject(tm, resourceLabels)

	certName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(4).Info("reconciling certificate resource", "name", certName)

	fetched := &certmanagerv1.Certificate{}
	exist, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), fetched)
	if err != nil {
		return FromClientError(err, "failed to check %s certificate resource already exists", certName)
	}

	if exist && trustManagerCreateRecon {
		r.eventRecorder.Eventf(tm, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s certificate resource already exists, maybe from previous installation", certName)
	}
	if exist && hasObjectChanged(desired, fetched) {
		r.log.V(1).Info("certificate has been modified, updating to desired state", "name", certName)
		if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to update %s certificate resource", certName)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "certificate resource %s reconciled back to desired state", certName)
	} else {
		r.log.V(4).Info("certificate resource already exists and is in expected state", "name", certName)
	}
	if !exist {
		if err := r.Create(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to create %s certificate resource", certName)
		}
		r.eventRecorder.Eventf(tm, corev1.EventTypeNormal, "Reconciled", "certificate resource %s created", certName)
	}

	return nil
}

func (r *Reconciler) getIssuerObject(tm *v1alpha1.TrustManager, resourceLabels map[string]string) *certmanagerv1.Issuer {
	issuer := decodeIssuerObjBytes(assets.MustAsset(webhookIssuerAssetName))
	trustNamespace := tm.Spec.TrustManagerConfig.TrustNamespace
	if trustNamespace == "" {
		trustNamespace = defaultTrustNamespace
	}
	updateNamespace(issuer, trustNamespace)
	updateResourceLabels(issuer, resourceLabels)
	return issuer
}

func (r *Reconciler) getCertificateObject(tm *v1alpha1.TrustManager, resourceLabels map[string]string) *certmanagerv1.Certificate {
	certificate := decodeCertificateObjBytes(assets.MustAsset(webhookCertificateAssetName))
	trustNamespace := tm.Spec.TrustManagerConfig.TrustNamespace
	if trustNamespace == "" {
		trustNamespace = defaultTrustNamespace
	}
	updateNamespace(certificate, trustNamespace)
	updateResourceLabels(certificate, resourceLabels)

	// Update DNS names to use the correct namespace
	certificate.Spec.DNSNames = []string{
		fmt.Sprintf("trust-manager.%s.svc", trustNamespace),
	}
	certificate.Spec.CommonName = fmt.Sprintf("trust-manager.%s.svc", trustNamespace)

	return certificate
}
