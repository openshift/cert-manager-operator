package istiocsr

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanagermetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func (r *Reconciler) createOrApplyCertificates(istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string, istioCSRCreateRecon bool) error {
	desired, err := r.getCertificateObject(istiocsr, resourceLabels)
	if err != nil {
		return fmt.Errorf("failed to generate certificate resource for creation in %s: %w", istiocsr.GetNamespace(), err)
	}

	certificateName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(1).Info("reconciling certificate resource", "name", certificateName)
	fetched := &certmanagerv1.Certificate{}
	key := types.NamespacedName{
		Name:      desired.GetName(),
		Namespace: desired.GetNamespace(),
	}
	exist, err := r.Exists(r.ctx, key, fetched)
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
		r.log.V(1).Info("certificate resource already exists and is in expected state", "name", certificateName)
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
	if istiocsr.Spec.IstioCSRConfig == nil {
		return nil, fmt.Errorf("not creating certificate resource, %s/%s spec.IstioCSRConfig is empty", istiocsr.GetNamespace(), istiocsr.GetName())
	}

	certificate := decodeCertificateObjBytes(assets.MustAsset(certificateAssetName))

	updateNamespace(certificate, istiocsr.Spec.IstioCSRConfig.Istio.Namespace)
	// add custom label for identification on the object created in different namespace.
	labels := make(map[string]string, len(resourceLabels)+1)
	for k, v := range resourceLabels {
		labels[k] = v
	}
	labels[istiocsrNamespaceMappingLabelName] = istiocsr.GetNamespace()
	certificate.SetLabels(labels)

	if err := updateCertificateParams(istiocsr, certificate); err != nil {
		return nil, NewIrrecoverableError(err, "failed to update certificate resource for %s/%s istiocsr deployment", istiocsr.GetNamespace(), istiocsr.GetName())
	}

	return certificate, nil
}

func updateCertificateParams(istiocsr *v1alpha1.IstioCSR, certificate *certmanagerv1.Certificate) error {
	certificate.Spec.CommonName = istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.CommonName
	if istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.CommonName == "" {
		certificate.Spec.CommonName = fmt.Sprintf(istiodCertificateCommonNameFmt, istiocsr.Spec.IstioCSRConfig.Istio.Namespace)
	}

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

	certificate.Spec.URIs = []string{
		fmt.Sprintf(istiodCertificateSpiffeURIFmt, istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.TrustDomain, istiocsr.Spec.IstioCSRConfig.Istio.Namespace),
	}

	certificate.Spec.Duration = istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.CertificateDuration
	if certificate.Spec.Duration == nil {
		certificate.Spec.Duration = &metav1.Duration{Duration: DefaultCertificateDuration}
	}

	certificate.Spec.RenewBefore = istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.CertificateRenewBefore
	if certificate.Spec.RenewBefore == nil {
		certificate.Spec.Duration = &metav1.Duration{Duration: DefaultCertificateRenewBeforeDuration}
	}

	certificate.Spec.PrivateKey.Algorithm = certmanagerv1.PrivateKeyAlgorithm(istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.SignatureAlgorithm)
	if certificate.Spec.PrivateKey.Algorithm == "" {
		certificate.Spec.PrivateKey.Algorithm = DefaultSignatureAlgorithm
	}

	certificate.Spec.PrivateKey.Size = istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.PrivateKeySize
	if certificate.Spec.PrivateKey.Size == 0 {
		certificate.Spec.PrivateKey.Size = DefaultRSAPrivateKeySize
	}
	if (certificate.Spec.PrivateKey.Algorithm == certmanagerv1.RSAKeyAlgorithm && certificate.Spec.PrivateKey.Size < DefaultRSAPrivateKeySize) ||
		(certificate.Spec.PrivateKey.Algorithm == certmanagerv1.ECDSAKeyAlgorithm && certificate.Spec.PrivateKey.Size != DefaultECDSA256PrivateKeySize && certificate.Spec.PrivateKey.Size != DefaultECDSA384PrivateKeySize) {
		return fmt.Errorf("certificate parameters PrivateKeySize(%d) and SignatureAlgorithm(%s) do not comply", certificate.Spec.PrivateKey.Size, certificate.Spec.PrivateKey.Algorithm)
	}

	certificate.Spec.IssuerRef = certmanagermetav1.ObjectReference{
		Kind:  istiocsr.Spec.IstioCSRConfig.CertManager.IssuerRef.Kind,
		Group: istiocsr.Spec.IstioCSRConfig.CertManager.IssuerRef.Group,
		Name:  istiocsr.Spec.IstioCSRConfig.CertManager.IssuerRef.Name,
	}
	return nil
}
