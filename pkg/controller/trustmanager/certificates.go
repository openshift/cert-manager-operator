package trustmanager

import (
	"fmt"
	"reflect"
	"slices"

	"k8s.io/utils/ptr"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanagermetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func (r *Reconciler) createOrApplyIssuer(trustManager *v1alpha1.TrustManager, resourceLabels, resourceAnnotations map[string]string) error {
	desired := getIssuerObject(resourceLabels, resourceAnnotations)
	return common.ApplyResource(r.ctx, r.CtrlClient, r.log, r.eventRecorder, trustManager, desired, &certmanagerv1.Issuer{}, fieldOwner, issuerModified)
}

func getIssuerObject(resourceLabels, resourceAnnotations map[string]string) *certmanagerv1.Issuer {
	issuer := common.DecodeObjBytes[*certmanagerv1.Issuer](codecs, certmanagerv1.SchemeGroupVersion, assets.MustAsset(issuerAssetName))
	common.UpdateName(issuer, trustManagerIssuerName)
	common.UpdateNamespace(issuer, operandNamespace)
	common.UpdateResourceLabels(issuer, resourceLabels)
	updateResourceAnnotations(issuer, resourceAnnotations)
	return issuer
}

func (r *Reconciler) createOrApplyCertificate(trustManager *v1alpha1.TrustManager, resourceLabels, resourceAnnotations map[string]string) error {
	desired := getCertificateObject(resourceLabels, resourceAnnotations)
	return common.ApplyResource(r.ctx, r.CtrlClient, r.log, r.eventRecorder, trustManager, desired, &certmanagerv1.Certificate{}, fieldOwner, certificateModified)
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

func issuerModified(desired, existing *certmanagerv1.Issuer) bool {
	return managedMetadataModified(desired, existing) ||
		!reflect.DeepEqual(desired.Spec, existing.Spec)
}

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
