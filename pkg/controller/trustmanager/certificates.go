package trustmanager

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func (r *Reconciler) createOrApplyIssuers(trustManager *v1alpha1.TrustManager, resourceLabels map[string]string, trustManagerCreateRecon bool) error {
	desired := r.getIssuerObject(resourceLabels)

	issuerName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(4).Info("reconciling issuer resource", "name", issuerName)
	fetched := &certmanagerv1.Issuer{}
	exist, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), fetched)
	if err != nil {
		return fromClientError(err, "failed to check %s issuer resource already exists", issuerName)
	}

	if exist && trustManagerCreateRecon {
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s issuer resource already exists, maybe from previous installation", issuerName)
	}
	if exist && hasObjectChanged(desired, fetched) {
		r.log.V(1).Info("issuer has been modified, updating to desired state", "name", issuerName)
		if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
			return fromClientError(err, "failed to update %s issuer resource", issuerName)
		}
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "issuer resource %s reconciled back to desired state", issuerName)
	} else {
		r.log.V(4).Info("issuer resource already exists and is in expected state", "name", issuerName)
	}
	if !exist {
		if err := r.Create(r.ctx, desired); err != nil {
			return fromClientError(err, "failed to create %s issuer resource", issuerName)
		}
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "issuer resource %s created", issuerName)
	}

	return nil
}

func (r *Reconciler) createOrApplyCertificates(trustManager *v1alpha1.TrustManager, resourceLabels map[string]string, trustManagerCreateRecon bool) error {
	desired := r.getCertificateObject(resourceLabels)

	certificateName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(4).Info("reconciling certificate resource", "name", certificateName)
	fetched := &certmanagerv1.Certificate{}
	exist, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), fetched)
	if err != nil {
		return fromClientError(err, "failed to check %s certificate resource already exists", certificateName)
	}

	if exist && trustManagerCreateRecon {
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s certificate resource already exists, maybe from previous installation", certificateName)
	}
	if exist && hasObjectChanged(desired, fetched) {
		r.log.V(1).Info("certificate has been modified, updating to desired state", "name", certificateName)
		if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
			return fromClientError(err, "failed to update %s certificate resource", certificateName)
		}
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "certificate resource %s reconciled back to desired state", certificateName)
	} else {
		r.log.V(4).Info("certificate resource already exists and is in expected state", "name", certificateName)
	}
	if !exist {
		if err := r.Create(r.ctx, desired); err != nil {
			return fromClientError(err, "failed to create %s certificate resource", certificateName)
		}
		r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "certificate resource %s created", certificateName)
	}

	return nil
}

func (r *Reconciler) getIssuerObject(resourceLabels map[string]string) *certmanagerv1.Issuer {
	issuer := decodeIssuerObjBytes(assets.MustAsset(issuerAssetName))
	updateNamespace(issuer, operandNamespace)
	updateResourceLabels(issuer, resourceLabels)
	return issuer
}

func (r *Reconciler) getCertificateObject(resourceLabels map[string]string) *certmanagerv1.Certificate {
	certificate := decodeCertificateObjBytes(assets.MustAsset(certificateAssetName))
	updateNamespace(certificate, operandNamespace)
	updateResourceLabels(certificate, resourceLabels)
	return certificate
}
