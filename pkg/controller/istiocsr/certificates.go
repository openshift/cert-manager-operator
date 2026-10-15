package istiocsr

import (
	"fmt"
	"maps"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanagermetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func (r *Reconciler) createOrApplyCertificates(istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string) error {
	desired, err := r.getCertificateObject(istiocsr, resourceLabels)
	if err != nil {
		return fmt.Errorf("failed to generate certificate resource for creation in %s: %w", istiocsr.GetNamespace(), err)
	}

	return common.ApplyResource(r.ctx, r.CtrlClient, r.log, r.eventRecorder, istiocsr, desired, &certmanagerv1.Certificate{}, fieldOwner,
		func(d, e *certmanagerv1.Certificate) bool { return hasObjectChanged(d, e) },
	)
}

func (r *Reconciler) getCertificateObject(istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string) (*certmanagerv1.Certificate, error) {
	certificate := decodeCertificateObjBytes(assets.MustAsset(certificateAssetName))

	common.UpdateNamespace(certificate, istiocsr.Spec.IstioCSRConfig.Istio.Namespace)
	// add custom label for identification on the object created in different namespace.
	labels := make(map[string]string, len(resourceLabels)+1)
	maps.Copy(labels, resourceLabels)
	labels[istiocsrNamespaceMappingLabelName] = istiocsr.GetNamespace()
	certificate.SetLabels(labels)

	if err := updateCertificateParams(istiocsr, certificate); err != nil {
		return nil, common.NewIrrecoverableError(err, "failed to update certificate resource for %s/%s istiocsr deployment", istiocsr.GetNamespace(), istiocsr.GetName())
	}

	return certificate, nil
}

func updateCertificateParams(istiocsr *v1alpha1.IstioCSR, certificate *certmanagerv1.Certificate) error {
	certificate.Spec.CommonName = istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.CommonName
	if istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.CommonName == "" {
		certificate.Spec.CommonName = fmt.Sprintf(istiodCertificateCommonNameFmt, istiocsr.Spec.IstioCSRConfig.Istio.Namespace)
	}

	dnsNames := make([]string, 0, len(istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.CertificateDNSNames)+len(istiocsr.Spec.IstioCSRConfig.Istio.Revisions))
	dnsNames = append(dnsNames, istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.CertificateDNSNames...)

	// Also need to add a DNS SAN for every requested revision, except for default revision, which will not be
	// included in the DNS name.
	for _, revision := range istiocsr.Spec.IstioCSRConfig.Istio.Revisions {
		if revision == "" {
			continue
		}
		if revision == defaultIstioRevision {
			name := fmt.Sprintf(istiodCertificateCommonNameFmt, istiocsr.Spec.IstioCSRConfig.Istio.Namespace)
			dnsNames = append(dnsNames, name)
			continue
		}
		name := fmt.Sprintf(istiodCertificateRevisionBasedDNSName, revision, istiocsr.Spec.IstioCSRConfig.Istio.Namespace)
		dnsNames = append(dnsNames, name)
	}
	certificate.Spec.DNSNames = dnsNames

	certificate.Spec.URIs = []string{
		fmt.Sprintf(istiodCertificateSpiffeURIFmt, istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.TrustDomain, istiocsr.Spec.IstioCSRConfig.Istio.Namespace),
	}

	certificate.Spec.Duration = istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.CertificateDuration
	if certificate.Spec.Duration == nil {
		certificate.Spec.Duration = &metav1.Duration{Duration: DefaultCertificateDuration}
	}

	certificate.Spec.RenewBefore = istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.CertificateRenewBefore
	if certificate.Spec.RenewBefore == nil {
		certificate.Spec.RenewBefore = &metav1.Duration{Duration: DefaultCertificateRenewBeforeDuration}
	}

	certificate.Spec.PrivateKey.Algorithm = certmanagerv1.PrivateKeyAlgorithm(istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.PrivateKeyAlgorithm)
	if certificate.Spec.PrivateKey.Algorithm == "" {
		certificate.Spec.PrivateKey.Algorithm = DefaultPrivateKeyAlgorithm
	}

	certificate.Spec.PrivateKey.Size = int(istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.PrivateKeySize)
	if certificate.Spec.PrivateKey.Size == 0 {
		if certificate.Spec.PrivateKey.Algorithm == certmanagerv1.RSAKeyAlgorithm {
			certificate.Spec.PrivateKey.Size = DefaultRSAPrivateKeySize
		}
		if certificate.Spec.PrivateKey.Algorithm == certmanagerv1.ECDSAKeyAlgorithm {
			certificate.Spec.PrivateKey.Size = DefaultECDSA384PrivateKeySize
		}
	}
	if (certificate.Spec.PrivateKey.Algorithm == certmanagerv1.RSAKeyAlgorithm && certificate.Spec.PrivateKey.Size < DefaultRSAPrivateKeySize) ||
		(certificate.Spec.PrivateKey.Algorithm == certmanagerv1.ECDSAKeyAlgorithm && certificate.Spec.PrivateKey.Size != DefaultECDSA256PrivateKeySize && certificate.Spec.PrivateKey.Size != DefaultECDSA384PrivateKeySize) {
		return fmt.Errorf("certificate parameters PrivateKeySize and PrivateKeyAlgorithm do not comply")
	}

	certificate.Spec.IssuerRef = certmanagermetav1.IssuerReference{
		Kind:  istiocsr.Spec.IstioCSRConfig.CertManager.IssuerRef.Kind,
		Group: istiocsr.Spec.IstioCSRConfig.CertManager.IssuerRef.Group,
		Name:  istiocsr.Spec.IstioCSRConfig.CertManager.IssuerRef.Name,
	}
	return nil
}
