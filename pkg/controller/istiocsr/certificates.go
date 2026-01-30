package istiocsr

import (
	"errors"
	"fmt"
	"maps"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanagermetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

var (
	errCertificateParamsNotCompliant = errors.New("certificate parameters PrivateKeySize and PrivateKeyAlgorithm do not comply")
)

func (r *Reconciler) createOrApplyCertificates(istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string, istioCSRCreateRecon bool) error {
	desired, err := r.getCertificateObject(istiocsr, resourceLabels)
	if err != nil {
		return fmt.Errorf("failed to generate certificate resource for creation in %s: %w", istiocsr.GetNamespace(), err)
	}

	certificateName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(logVerbosityLevelDebug).Info("reconciling certificate resource", "name", certificateName)
	fetched := &certmanagerv1.Certificate{}
	exist, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), fetched)
	if err != nil {
		return FromClientError(err, "failed to check %s certificate resource already exists", certificateName)
	}

	if exist && istioCSRCreateRecon {
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s certificate resource already exists, maybe from previous installation", certificateName)
	}
	if exist && hasObjectChanged(desired, fetched) {
		r.log.V(1).Info("certificate has been modified, updating to desired state", "name", certificateName)
		if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to update %s certificate resource", certificateName)
		}
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeNormal, "Reconciled", "certificate resource %s reconciled back to desired state", certificateName)
	} else {
		r.log.V(logVerbosityLevelDebug).Info("certificate resource already exists and is in expected state", "name", certificateName)
	}
	if !exist {
		if err := r.Create(r.ctx, desired); err != nil {
			return FromClientError(err, "failed to create %s certificate resource", certificateName)
		}
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeNormal, "Reconciled", "certificate resource %s created", certificateName)
	}

	return nil
}

func (r *Reconciler) getCertificateObject(istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string) (*certmanagerv1.Certificate, error) {
	certificate := decodeCertificateObjBytes(assets.MustAsset(certificateAssetName))

	updateNamespace(certificate, istiocsr.Spec.IstioCSRConfig.Istio.Namespace)
	// add custom label for identification on the object created in different namespace.
	labels := make(map[string]string, len(resourceLabels)+1)
	maps.Copy(labels, resourceLabels)
	labels[istiocsrNamespaceMappingLabelName] = istiocsr.GetNamespace()
	certificate.SetLabels(labels)

	if err := updateCertificateParams(istiocsr, certificate); err != nil {
		return nil, NewIrrecoverableError(err, "failed to update certificate resource for %s/%s istiocsr deployment", istiocsr.GetNamespace(), istiocsr.GetName())
	}

	return certificate, nil
}

func updateCertificateParams(istiocsr *v1alpha1.IstioCSR, certificate *certmanagerv1.Certificate) error {
	updateCertificateCommonName(istiocsr, certificate)
	updateCertificateDNSNames(istiocsr, certificate)
	updateCertificateURIs(istiocsr, certificate)
	updateCertificateDuration(istiocsr, certificate)
	updateCertificateRenewBefore(istiocsr, certificate)
	if err := updateCertificatePrivateKey(istiocsr, certificate); err != nil {
		return err
	}
	updateCertificateIssuerRef(istiocsr, certificate)
	return nil
}

func updateCertificateCommonName(istiocsr *v1alpha1.IstioCSR, certificate *certmanagerv1.Certificate) {
	certificate.Spec.CommonName = istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.CommonName
	if certificate.Spec.CommonName == "" {
		certificate.Spec.CommonName = fmt.Sprintf(istiodCertificateCommonNameFmt, istiocsr.Spec.IstioCSRConfig.Istio.Namespace)
	}
}

func updateCertificateDNSNames(istiocsr *v1alpha1.IstioCSR, certificate *certmanagerv1.Certificate) {
	dnsNames := make([]string, len(istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.CertificateDNSNames))
	copy(dnsNames, istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.CertificateDNSNames)

	// Also need to add a DNS SAN for every requested revision, except for default revision, which will not be
	// included in the DNS name.
	for _, revision := range istiocsr.Spec.IstioCSRConfig.Istio.Revisions {
		if revision == "" {
			continue
		}
		if revision == "default" {
			name := fmt.Sprintf(istiodCertificateDefaultDNSName, istiocsr.Spec.IstioCSRConfig.Istio.Namespace)
			dnsNames = append(dnsNames, name)
			continue
		}
		name := fmt.Sprintf(istiodCertificateRevisionBasedDNSName, revision, istiocsr.Spec.IstioCSRConfig.Istio.Namespace)
		dnsNames = append(dnsNames, name)
	}
	certificate.Spec.DNSNames = dnsNames
}

func updateCertificateURIs(istiocsr *v1alpha1.IstioCSR, certificate *certmanagerv1.Certificate) {
	certificate.Spec.URIs = []string{
		fmt.Sprintf(istiodCertificateSpiffeURIFmt, istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.TrustDomain, istiocsr.Spec.IstioCSRConfig.Istio.Namespace),
	}
}

func updateCertificateDuration(istiocsr *v1alpha1.IstioCSR, certificate *certmanagerv1.Certificate) {
	certificate.Spec.Duration = istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.CertificateDuration
	if certificate.Spec.Duration == nil {
		certificate.Spec.Duration = &metav1.Duration{Duration: DefaultCertificateDuration}
	}
}

func updateCertificateRenewBefore(istiocsr *v1alpha1.IstioCSR, certificate *certmanagerv1.Certificate) {
	certificate.Spec.RenewBefore = istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.CertificateRenewBefore
	if certificate.Spec.RenewBefore == nil {
		certificate.Spec.RenewBefore = &metav1.Duration{Duration: DefaultCertificateRenewBeforeDuration}
	}
}

func updateCertificatePrivateKey(istiocsr *v1alpha1.IstioCSR, certificate *certmanagerv1.Certificate) error {
	certificate.Spec.PrivateKey.Algorithm = certmanagerv1.PrivateKeyAlgorithm(istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.PrivateKeyAlgorithm)
	if certificate.Spec.PrivateKey.Algorithm == "" {
		certificate.Spec.PrivateKey.Algorithm = DefaultPrivateKeyAlgorithm
	}

	certificate.Spec.PrivateKey.Size = int(istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.PrivateKeySize)
	if certificate.Spec.PrivateKey.Size == 0 {
		setDefaultPrivateKeySize(certificate)
	}

	if !isPrivateKeySizeCompliant(certificate) {
		return errCertificateParamsNotCompliant
	}
	return nil
}

func setDefaultPrivateKeySize(certificate *certmanagerv1.Certificate) {
	if certificate.Spec.PrivateKey.Algorithm == certmanagerv1.RSAKeyAlgorithm {
		certificate.Spec.PrivateKey.Size = DefaultRSAPrivateKeySize
	}
	if certificate.Spec.PrivateKey.Algorithm == certmanagerv1.ECDSAKeyAlgorithm {
		certificate.Spec.PrivateKey.Size = DefaultECDSA384PrivateKeySize
	}
}

func isPrivateKeySizeCompliant(certificate *certmanagerv1.Certificate) bool {
	if certificate.Spec.PrivateKey.Algorithm == certmanagerv1.RSAKeyAlgorithm {
		return certificate.Spec.PrivateKey.Size >= DefaultRSAPrivateKeySize
	}
	if certificate.Spec.PrivateKey.Algorithm == certmanagerv1.ECDSAKeyAlgorithm {
		return certificate.Spec.PrivateKey.Size == DefaultECDSA256PrivateKeySize || certificate.Spec.PrivateKey.Size == DefaultECDSA384PrivateKeySize
	}
	return true
}

func updateCertificateIssuerRef(istiocsr *v1alpha1.IstioCSR, certificate *certmanagerv1.Certificate) {
	certificate.Spec.IssuerRef = certmanagermetav1.ObjectReference{
		Kind:  istiocsr.Spec.IstioCSRConfig.CertManager.IssuerRef.Kind,
		Group: istiocsr.Spec.IstioCSRConfig.CertManager.IssuerRef.Group,
		Name:  istiocsr.Spec.IstioCSRConfig.CertManager.IssuerRef.Name,
	}
}
