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
	// Copy managed labels and annotations onto secretTemplate. Manifest annotations
	// such as cert-manager.io/allow-direct-injection are reapplied so cainjector
	// can fill the webhook caBundle. webhookTLS.certManager does not replace this template.
	applyWebhookCertConfig(certificate, config.WebhookTLS.CertManager)
	certificate.Spec.SecretTemplate = certificateSecretTemplate(certificate.Spec.SecretTemplate, resourceLabels, resourceAnnotations)

	return certificate
}

// applyWebhookCertConfig copies webhookTLS.certManager onto the Certificate.
// Unset fields keep the operator default (self-signed Issuer) or are left for cert-manager to default.
func applyWebhookCertConfig(certificate *certmanagerv1.Certificate, certConfig v1alpha1.TrustManagerCertConfig) {
	certificate.Spec.IssuerRef = webhookCertificateIssuerRef(certConfig)
	if certConfig.CertificateDuration != nil {
		certificate.Spec.Duration = certConfig.CertificateDuration
	}
	if certConfig.CertificateRenewBefore != nil {
		certificate.Spec.RenewBefore = certConfig.CertificateRenewBefore
	}
	if certConfig.CertificateSignatureAlgorithm != "" {
		certificate.Spec.SignatureAlgorithm = certmanagerv1.SignatureAlgorithm(certConfig.CertificateSignatureAlgorithm)
	} else {
		certificate.Spec.SignatureAlgorithm = certmanagerv1.PureEd25519
	}
	overlayPrivateKeyFromCertConfig(certificate, certConfig)
}

// webhookCertificateIssuerRef is the issuer for the webhook Certificate and for
// the CertificateRequestPolicy selector. An unset issuerRef uses the operator's
// self-signed Issuer. Both must match or approver-policy never approves the request.
func webhookCertificateIssuerRef(certConfig v1alpha1.TrustManagerCertConfig) certmanagermetav1.IssuerReference {
	if certConfig.IssuerRef == nil {
		return certmanagermetav1.IssuerReference{
			Name:  trustManagerIssuerName,
			Kind:  "Issuer",
			Group: "cert-manager.io",
		}
	}
	ref := *certConfig.IssuerRef
	if ref.Kind == "" {
		ref.Kind = "Issuer"
	}
	if ref.Group == "" {
		ref.Group = "cert-manager.io"
	}
	return ref
}

// overlayPrivateKeyFromCertConfig copies configured private-key fields onto the
// Certificate decoded from bindata. Unset algorithm defaults to Ed25519 (API default).
// Unset rotation policy and size keep bindata or cert-manager defaults.
func overlayPrivateKeyFromCertConfig(certificate *certmanagerv1.Certificate, certConfig v1alpha1.TrustManagerCertConfig) {
	if certConfig.PrivateKeyRotationPolicy == "" && certConfig.PrivateKeySize == 0 && certConfig.PrivateKeyAlgorithm == "" {
		if certificate.Spec.PrivateKey == nil {
			certificate.Spec.PrivateKey = &certmanagerv1.CertificatePrivateKey{}
		}
		certificate.Spec.PrivateKey.Algorithm = certmanagerv1.Ed25519KeyAlgorithm
		return
	}
	if certificate.Spec.PrivateKey == nil {
		certificate.Spec.PrivateKey = &certmanagerv1.CertificatePrivateKey{}
	}
	if certConfig.PrivateKeyAlgorithm != "" {
		certificate.Spec.PrivateKey.Algorithm = certmanagerv1.PrivateKeyAlgorithm(certConfig.PrivateKeyAlgorithm)
	}
	if certConfig.PrivateKeyRotationPolicy != "" {
		certificate.Spec.PrivateKey.RotationPolicy = certmanagerv1.PrivateKeyRotationPolicy(certConfig.PrivateKeyRotationPolicy)
	}
	if certConfig.PrivateKeySize != 0 {
		certificate.Spec.PrivateKey.Size = int(certConfig.PrivateKeySize)
	}
}

// certificateSecretTemplate copies operator-managed labels and annotations onto
// the Certificate secretTemplate so cert-manager propagates them to the webhook
// TLS Secret. User annotations are copied first, then manifest annotations are
// re-applied so keys such as cert-manager.io/allow-direct-injection keep the
// manifest value. cainjector uses that annotation to fill the webhook caBundle.
func certificateSecretTemplate(base *certmanagerv1.CertificateSecretTemplate, resourceLabels, resourceAnnotations map[string]string) *certmanagerv1.CertificateSecretTemplate {
	template := &certmanagerv1.CertificateSecretTemplate{}
	if base != nil {
		template = base.DeepCopy()
	}
	if len(resourceLabels) > 0 {
		if template.Labels == nil {
			template.Labels = make(map[string]string, len(resourceLabels))
		}
		maps.Copy(template.Labels, resourceLabels)
	}
	if len(resourceAnnotations) > 0 {
		merged := make(map[string]string, len(template.Annotations)+len(resourceAnnotations))
		maps.Copy(merged, resourceAnnotations)
		maps.Copy(merged, template.Annotations)
		template.Annotations = merged
	}
	return template
}

// issuerModified compares only the fields we manage via SSA.
func issuerModified(desired, existing *certmanagerv1.Issuer) bool {
	return common.ManagedMetadataModified(desired, existing, fieldOwner) ||
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
	if common.ManagedMetadataModified(desired, existing, fieldOwner) {
		return true
	}
	if desired.Spec.CommonName != existing.Spec.CommonName ||
		!slices.Equal(desired.Spec.DNSNames, existing.Spec.DNSNames) ||
		desired.Spec.SecretName != existing.Spec.SecretName ||
		!ptr.Equal(desired.Spec.RevisionHistoryLimit, existing.Spec.RevisionHistoryLimit) ||
		!reflect.DeepEqual(desired.Spec.IssuerRef, existing.Spec.IssuerRef) ||
		secretTemplateModified(desired.Spec.SecretTemplate, existing.Spec.SecretTemplate, existing) ||
		certificatePrivateKeyRotationPolicy(desired.Spec.PrivateKey) != certificatePrivateKeyRotationPolicy(existing.Spec.PrivateKey) {
		return true
	}
	if certificateFieldDrift(desired.Spec.Duration != nil, ptr.Equal(desired.Spec.Duration, existing.Spec.Duration), existing, "spec", "duration") {
		return true
	}
	if certificateFieldDrift(desired.Spec.RenewBefore != nil, ptr.Equal(desired.Spec.RenewBefore, existing.Spec.RenewBefore), existing, "spec", "renewBefore") {
		return true
	}
	if certificateFieldDrift(certificatePrivateKeyAlgorithm(desired.Spec.PrivateKey) != "", certificatePrivateKeyAlgorithm(desired.Spec.PrivateKey) == certificatePrivateKeyAlgorithm(existing.Spec.PrivateKey), existing, "spec", "privateKey", "algorithm") {
		return true
	}
	if certificateFieldDrift(certificatePrivateKeySize(desired.Spec.PrivateKey) != 0, certificatePrivateKeySize(desired.Spec.PrivateKey) == certificatePrivateKeySize(existing.Spec.PrivateKey), existing, "spec", "privateKey", "size") {
		return true
	}
	if certificateFieldDrift(desired.Spec.SignatureAlgorithm != "", desired.Spec.SignatureAlgorithm == existing.Spec.SignatureAlgorithm, existing, "spec", "signatureAlgorithm") {
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

// certificatePrivateKeyRotationPolicy returns the rotation policy we manage on
// the webhook Certificate. Bindata always sets RotationPolicy, so it is compared
// directly. Algorithm and size use certificateFieldDrift so webhook defaults are
// left alone until the user sets them.
func certificatePrivateKeyRotationPolicy(pk *certmanagerv1.CertificatePrivateKey) certmanagerv1.PrivateKeyRotationPolicy {
	if pk == nil {
		return ""
	}
	return pk.RotationPolicy
}

// secretTemplateModified reports whether managed secretTemplate labels or
// annotations have drifted. Keys still requested are compared by value. A key
// that is no longer requested is drift only when trust-manager-controller owns
// it in managedFields, so the next server-side apply drops it. Keys owned by
// another manager are left alone. Nil and empty maps are treated as equivalent
// so cert-manager omitting an empty annotations field does not force a re-apply.
func secretTemplateModified(desired, existing *certmanagerv1.CertificateSecretTemplate, existingCert *certmanagerv1.Certificate) bool {
	if desired == nil && existing == nil {
		return false
	}
	var desiredLabels, desiredAnnotations, existingLabels, existingAnnotations map[string]string
	if desired != nil {
		desiredLabels = desired.Labels
		desiredAnnotations = desired.Annotations
	}
	if existing != nil {
		existingLabels = existing.Labels
		existingAnnotations = existing.Annotations
	}
	if managedMapModified(desiredLabels, existingLabels) || managedMapModified(desiredAnnotations, existingAnnotations) {
		return true
	}
	if existingCert == nil || (!mapHasKeyAbsentFrom(existingLabels, desiredLabels) && !mapHasKeyAbsentFrom(existingAnnotations, desiredAnnotations)) {
		return false
	}
	owned := controllerOwnedFields(existingCert)
	return ownedSecretTemplateKeyRemoved(owned, "labels", desiredLabels, existingLabels) ||
		ownedSecretTemplateKeyRemoved(owned, "annotations", desiredAnnotations, existingAnnotations)
}

func managedMapModified(desired, existing map[string]string) bool {
	for k, v := range desired {
		existingValue, ok := existing[k]
		if !ok || existingValue != v {
			return true
		}
	}
	return false
}

func mapHasKeyAbsentFrom(live, desired map[string]string) bool {
	for key := range live {
		if _, ok := desired[key]; !ok {
			return true
		}
	}
	return false
}

func ownedSecretTemplateKeyRemoved(owned *fieldpath.Set, mapField string, desired, live map[string]string) bool {
	for key := range live {
		if _, kept := desired[key]; kept {
			continue
		}
		if owned.Has(fieldpath.MakePathOrDie("spec", "secretTemplate", mapField, key)) {
			return true
		}
	}
	return false
}

// controllerOwnedFields returns the fields trust-manager-controller owns on the
// main resource. Status and other subresources are ignored. An unreadable
// managedFields entry is skipped.
func controllerOwnedFields(obj *certmanagerv1.Certificate) *fieldpath.Set {
	owned := fieldpath.NewSet()
	if obj == nil {
		return owned
	}
	for _, entry := range obj.GetManagedFields() {
		if entry.Manager != fieldOwner || entry.Subresource != "" || entry.FieldsV1 == nil || len(entry.FieldsV1.Raw) == 0 {
			continue
		}
		var fields fieldpath.Set
		if err := fields.FromJSON(bytes.NewReader(entry.FieldsV1.Raw)); err != nil {
			continue
		}
		owned = owned.Union(&fields)
	}
	return owned
}

func certificatePrivateKeyAlgorithm(pk *certmanagerv1.CertificatePrivateKey) certmanagerv1.PrivateKeyAlgorithm {
	if pk == nil {
		return ""
	}
	return pk.Algorithm
}

func certificatePrivateKeySize(pk *certmanagerv1.CertificatePrivateKey) int {
	if pk == nil {
		return 0
	}
	return pk.Size
}
