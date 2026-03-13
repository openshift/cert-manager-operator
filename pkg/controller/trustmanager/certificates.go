package trustmanager

import (
	"fmt"
	"reflect"
	"slices"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

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
		r.log.V(4).Info("issuer already matches desired state, skipping apply", "name", resourceName)
		return nil
	}

	if err := r.Patch(r.ctx, desired, client.Apply, client.FieldOwner(fieldOwner), client.ForceOwnership); err != nil {
		return common.FromClientError(err, "failed to apply issuer %q", resourceName)
	}

	r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "issuer resource %s applied", resourceName)
	r.log.V(2).Info("applied issuer", "name", resourceName)
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
	desired := getCertificateObject(resourceLabels, resourceAnnotations)
	resourceName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(4).Info("reconciling certificate resource", "name", resourceName)

	existing := &certmanagerv1.Certificate{}
	exists, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), existing)
	if err != nil {
		return common.FromClientError(err, "failed to check if certificate %q exists", resourceName)
	}
	if exists && !certificateModified(desired, existing) {
		r.log.V(4).Info("certificate already matches desired state, skipping apply", "name", resourceName)
		return nil
	}

	if err := r.Patch(r.ctx, desired, client.Apply, client.FieldOwner(fieldOwner), client.ForceOwnership); err != nil {
		return common.FromClientError(err, "failed to apply certificate %q", resourceName)
	}

	r.eventRecorder.Eventf(trustManager, corev1.EventTypeNormal, "Reconciled", "certificate resource %s applied", resourceName)
	r.log.V(2).Info("applied certificate", "name", resourceName)
	return nil
}

func getCertificateObject(resourceLabels, resourceAnnotations map[string]string) *certmanagerv1.Certificate {
	certificate := common.DecodeObjBytes[*certmanagerv1.Certificate](codecs, certmanagerv1.SchemeGroupVersion, assets.MustAsset(certificateAssetName))
	common.UpdateName(certificate, trustManagerCertificateName)
	common.UpdateNamespace(certificate, operandNamespace)
	common.UpdateResourceLabels(certificate, resourceLabels)
	updateResourceAnnotations(certificate, resourceAnnotations)

	dnsName := fmt.Sprintf("%s.%s.svc", trustManagerServiceName, operandNamespace)
	certificate.Spec.CommonName = dnsName
	certificate.Spec.DNSNames = []string{dnsName}
	certificate.Spec.SecretName = trustManagerTLSSecretName
	certificate.Spec.IssuerRef = certmanagermetav1.ObjectReference{
		Name:  trustManagerIssuerName,
		Kind:  "Issuer",
		Group: "cert-manager.io",
	}

	return certificate
}

// issuerModified compares only the fields we manage via SSA.
func issuerModified(desired, existing *certmanagerv1.Issuer) bool {
	return managedMetadataModified(desired, existing) ||
		!reflect.DeepEqual(desired.Spec, existing.Spec)
}

// certificateModified compares only the fields we manage via SSA.
// We compare individual spec fields rather than the full Spec because
// cert-manager's webhook may default fields we don't set (e.g. Duration).
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
	return false
}
