package trustmanager

import (
	"bytes"
	"fmt"
	"maps"
	"reflect"
	"slices"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/structured-merge-diff/v6/fieldpath"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanagermetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

// createOrApplyIssuer reconciles the self-signed Issuer used for trust-manager's webhook TLS.
func (r *Reconciler) createOrApplyIssuer(trustManager *v1alpha1.TrustManager, resourceLabels, resourceAnnotations map[string]string) error {
	desired := getIssuerObject(resourceLabels, resourceAnnotations)
	resourceName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(4).Info("reconciling issuer resource", "name", resourceName)

	existing := &certmanagerv1.Issuer{}
	exists, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), existing)
	if err != nil {
		return common.FromClientError(err, "failed to check if issuer %q exists", resourceName)
	}
	if exists && !issuerModified(desired, existing) {
		r.log.V(4).Info("issuer resource exists and is in desired state", "name", resourceName)
		return nil
	}

	r.log.V(2).Info("issuer resource has been modified, updating to desired state", "name", resourceName)
	if err := r.Patch(r.ctx, desired, client.Apply, client.FieldOwner(fieldOwner), client.ForceOwnership); err != nil {
		return common.FromClientError(err, "failed to apply issuer %q", resourceName)
	}

	r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "issuer resource %s applied", resourceName)
	return nil
}

func getIssuerObject(resourceLabels, resourceAnnotations map[string]string) *certmanagerv1.Issuer {
	issuer := common.DecodeObjBytes[*certmanagerv1.Issuer](codecs, certmanagerv1.SchemeGroupVersion, assets.MustAsset(issuerAssetName))
	common.UpdateName(issuer, trustManagerIssuerName)
	common.UpdateNamespace(issuer, operandNamespace)
	common.UpdateResourceLabels(issuer, resourceLabels)
	updateResourceAnnotations(issuer, resourceAnnotations)
	return issuer
}

// createOrApplyCertificate reconciles the Certificate used for trust-manager's webhook TLS.
func (r *Reconciler) createOrApplyCertificate(trustManager *v1alpha1.TrustManager, resourceLabels, resourceAnnotations map[string]string) error {
	desired := getCertificateObject(trustManager.Spec.TrustManagerConfig, resourceLabels, resourceAnnotations)
	resourceName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(4).Info("reconciling certificate resource", "name", resourceName)

	existing := &certmanagerv1.Certificate{}
	exists, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), existing)
	if err != nil {
		return common.FromClientError(err, "failed to check if certificate %q exists", resourceName)
	}
	if exists && !certificateModified(desired, existing) {
		r.log.V(4).Info("certificate resource exists and is in desired state", "name", resourceName)
		return nil
	}

	r.log.V(2).Info("certificate resource has been modified, updating to desired state", "name", resourceName)
	if err := r.Patch(r.ctx, desired, client.Apply, client.FieldOwner(fieldOwner), client.ForceOwnership); err != nil {
		return common.FromClientError(err, "failed to apply certificate %q", resourceName)
	}

	r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "certificate resource %s applied", resourceName)
	return nil
}

func getCertificateObject(config v1alpha1.TrustManagerConfig, resourceLabels, resourceAnnotations map[string]string) *certmanagerv1.Certificate {
	certificate := common.DecodeObjBytes[*certmanagerv1.Certificate](codecs, certmanagerv1.SchemeGroupVersion, assets.MustAsset(certificateAssetName))
	common.UpdateName(certificate, trustManagerCertificateName)
	common.UpdateNamespace(certificate, operandNamespace)
	common.UpdateResourceLabels(certificate, resourceLabels)
	updateResourceAnnotations(certificate, resourceAnnotations)

	dnsName := fmt.Sprintf("%s.%s.svc", trustManagerServiceName, operandNamespace)
	certificate.Spec.CommonName = dnsName
	certificate.Spec.DNSNames = []string{dnsName}
	certificate.Spec.SecretName = trustManagerTLSSecretName
	applyWebhookCertConfig(certificate, config.WebhookTLS.CertManager, resourceLabels, resourceAnnotations)

	return certificate
}

// applyWebhookCertConfig copies webhookTLS.certManager onto the Certificate.
// Unset fields keep the operator default (self-signed Issuer) or are left for cert-manager to default.
func applyWebhookCertConfig(certificate *certmanagerv1.Certificate, certConfig v1alpha1.TrustManagerCertConfig, resourceLabels, resourceAnnotations map[string]string) {
	certificate.Spec.IssuerRef = certmanagermetav1.ObjectReference{
		Name:  trustManagerIssuerName,
		Kind:  "Issuer",
		Group: "cert-manager.io",
	}
	if certConfig.IssuerRef != nil {
		certificate.Spec.IssuerRef = *certConfig.IssuerRef
	}
	if certConfig.CertificateDuration != nil {
		certificate.Spec.Duration = certConfig.CertificateDuration
	}
	if certConfig.CertificateRenewBefore != nil {
		certificate.Spec.RenewBefore = certConfig.CertificateRenewBefore
	}
	if certConfig.CertificateSignatureAlgorithm != "" {
		certificate.Spec.SignatureAlgorithm = certmanagerv1.SignatureAlgorithm(certConfig.CertificateSignatureAlgorithm)
	}
	if privateKey := privateKeyFromCertConfig(certConfig); privateKey != nil {
		certificate.Spec.PrivateKey = privateKey
	}
	if certConfig.PropagateMetadataToSecret == v1alpha1.Enabled {
		certificate.Spec.SecretTemplate = &certmanagerv1.CertificateSecretTemplate{
			Labels:      maps.Clone(resourceLabels),
			Annotations: maps.Clone(resourceAnnotations),
		}
	}
}

func privateKeyFromCertConfig(certConfig v1alpha1.TrustManagerCertConfig) *certmanagerv1.CertificatePrivateKey {
	if certConfig.PrivateKeyAlgorithm == "" && certConfig.PrivateKeyRotationPolicy == "" && certConfig.PrivateKeySize == 0 {
		return nil
	}
	privateKey := &certmanagerv1.CertificatePrivateKey{}
	if certConfig.PrivateKeyAlgorithm != "" {
		privateKey.Algorithm = certmanagerv1.PrivateKeyAlgorithm(certConfig.PrivateKeyAlgorithm)
	}
	if certConfig.PrivateKeyRotationPolicy != "" {
		privateKey.RotationPolicy = certmanagerv1.PrivateKeyRotationPolicy(certConfig.PrivateKeyRotationPolicy)
	}
	if certConfig.PrivateKeySize != 0 {
		privateKey.Size = int(certConfig.PrivateKeySize)
	}
	return privateKey
}

// issuerModified compares only the fields we manage via SSA.
func issuerModified(desired, existing *certmanagerv1.Issuer) bool {
	return managedMetadataModified(desired, existing) ||
		!reflect.DeepEqual(desired.Spec, existing.Spec)
}

// certificateModified compares only the fields we manage via SSA.
// We compare individual spec fields rather than the full Spec because
// cert-manager's webhook may default fields we don't set.
// A field webhookTLS.certManager sets is drift when it differs. A field it
// leaves unset is applied away only while trust-manager-controller still owns
// it, so server-side apply drops that field and cert-manager can restore its
// default. A webhook-defaulted value is left alone. IssuerRef is always set,
// either from certManager.issuerRef or the operator's self-signed Issuer.
func certificateModified(desired, existing *certmanagerv1.Certificate) bool {
	if managedMetadataModified(desired, existing) {
		return true
	}
	if desired.Spec.CommonName != existing.Spec.CommonName ||
		!slices.Equal(desired.Spec.DNSNames, existing.Spec.DNSNames) ||
		desired.Spec.SecretName != existing.Spec.SecretName ||
		!ptr.Equal(desired.Spec.RevisionHistoryLimit, existing.Spec.RevisionHistoryLimit) ||
		!reflect.DeepEqual(desired.Spec.IssuerRef, existing.Spec.IssuerRef) {
		return true
	}
	if certificateFieldDrift(desired.Spec.Duration != nil, ptr.Equal(desired.Spec.Duration, existing.Spec.Duration), existing, "spec", "duration") {
		return true
	}
	if certificateFieldDrift(desired.Spec.RenewBefore != nil, ptr.Equal(desired.Spec.RenewBefore, existing.Spec.RenewBefore), existing, "spec", "renewBefore") {
		return true
	}
	if certificateFieldDrift(desired.Spec.PrivateKey != nil, reflect.DeepEqual(desired.Spec.PrivateKey, existing.Spec.PrivateKey), existing, "spec", "privateKey") {
		return true
	}
	if certificateFieldDrift(desired.Spec.SignatureAlgorithm != "", desired.Spec.SignatureAlgorithm == existing.Spec.SignatureAlgorithm, existing, "spec", "signatureAlgorithm") {
		return true
	}
	if certificateFieldDrift(desired.Spec.SecretTemplate != nil, reflect.DeepEqual(desired.Spec.SecretTemplate, existing.Spec.SecretTemplate), existing, "spec", "secretTemplate") {
		return true
	}
	return false
}

// certificateFieldDrift reports whether a Certificate field should be applied.
// A field the desired object sets is drift when it differs. A field the desired
// object leaves unset is drift only while trust-manager-controller still owns it,
// so server-side apply can drop it and cert-manager can restore its default.
func certificateFieldDrift(desiredSet, equal bool, existing *certmanagerv1.Certificate, parts ...string) bool {
	if desiredSet {
		return !equal
	}
	return !equal && controllerOwnsField(existing, parts...)
}

// controllerOwnsField reports whether trust-manager-controller's server-side apply
// managed fields include path or one of its children.
func controllerOwnsField(cert *certmanagerv1.Certificate, parts ...string) bool {
	pathElems := make([]any, len(parts))
	for i, part := range parts {
		pathElems[i] = part
	}
	path := fieldpath.MakePathOrDie(pathElems...)
	for _, entry := range cert.GetManagedFields() {
		if entry.Manager != fieldOwner || entry.Subresource != "" || entry.FieldsV1 == nil || len(entry.FieldsV1.Raw) == 0 {
			continue
		}
		var fields fieldpath.Set
		if err := fields.FromJSON(bytes.NewReader(entry.FieldsV1.Raw)); err != nil {
			continue
		}
		if fields.Has(path) {
			return true
		}
		subset := &fields
		for _, part := range parts {
			name := part
			subset = subset.WithPrefix(fieldpath.PathElement{FieldName: &name})
		}
		if subset != nil && !subset.Empty() {
			return true
		}
	}
	return false
}
