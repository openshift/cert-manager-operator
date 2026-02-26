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
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

var errCertificateParamsNonCompliant = errors.New("certificate parameters PrivateKeySize and PrivateKeyAlgorithm do not comply")

func (r *Reconciler) createOrApplyCertificates(istiocsr *v1alpha1.IstioCSR, resourceLabels map[string]string, istioCSRCreateRecon bool) error {
	desired, err := r.getCertificateObject(istiocsr, resourceLabels)
	if err != nil {
		return fmt.Errorf("failed to generate certificate resource for creation in %s: %w", istiocsr.GetNamespace(), err)
	}

	certificateName := fmt.Sprintf("%s/%s", desired.GetNamespace(), desired.GetName())
	r.log.V(4).Info("reconciling certificate resource", "name", certificateName)
	fetched := &certmanagerv1.Certificate{}
	exist, err := r.Exists(r.ctx, client.ObjectKeyFromObject(desired), fetched)
	if err != nil {
		return common.FromClientError(err, "failed to check %s certificate resource already exists", certificateName)
	}

	if exist && istioCSRCreateRecon {
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeWarning, "ResourceAlreadyExists", "%s certificate resource already exists, maybe from previous installation", certificateName)
	}
	if exist && hasObjectChanged(desired, fetched) {
		r.log.V(1).Info("certificate has been modified, updating to desired state", "name", certificateName)
		if err := r.UpdateWithRetry(r.ctx, desired); err != nil {
			return common.FromClientError(err, "failed to update %s certificate resource", certificateName)
		}
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeNormal, "Reconciled", "certificate resource %s reconciled back to desired state", certificateName)
	} else {
		r.log.V(4).Info("certificate resource already exists and is in expected state", "name", certificateName)
	}
	if !exist {
		if err := r.Create(r.ctx, desired); err != nil {
			return common.FromClientError(err, "failed to create %s certificate resource", certificateName)
		}
		r.eventRecorder.Eventf(istiocsr, corev1.EventTypeNormal, "Reconciled", "certificate resource %s created", certificateName)
	}

	return nil
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

// buildRevisionDNSNames generates DNS SAN entries for every requested revision.
// The default revision uses a simpler DNS name format, while named revisions
// include the revision name in the DNS entry.
func buildRevisionDNSNames(revisions []string, namespace string) []string {
	dnsNames := make([]string, 0, len(revisions))
	for _, revision := range revisions {
		if revision == "" {
			continue
		}
		if revision == "default" {
			dnsNames = append(dnsNames, fmt.Sprintf(istiodCertificateDefaultDNSName, namespace))
			continue
		}
		dnsNames = append(dnsNames, fmt.Sprintf(istiodCertificateRevisionBasedDNSName, revision, namespace))
	}
	return dnsNames
}

func resolvePrivateKeyConfig(algorithm certmanagerv1.PrivateKeyAlgorithm, size int) (certmanagerv1.PrivateKeyAlgorithm, int, error) {
	if algorithm == "" {
		algorithm = DefaultPrivateKeyAlgorithm
	}
	if size == 0 {
		if algorithm == certmanagerv1.RSAKeyAlgorithm {
			size = DefaultRSAPrivateKeySize
		}
		if algorithm == certmanagerv1.ECDSAKeyAlgorithm {
			size = DefaultECDSA384PrivateKeySize
		}
	}
	if (algorithm == certmanagerv1.RSAKeyAlgorithm && size < DefaultRSAPrivateKeySize) ||
		(algorithm == certmanagerv1.ECDSAKeyAlgorithm && size != DefaultECDSA256PrivateKeySize && size != DefaultECDSA384PrivateKeySize) {
		return "", 0, errCertificateParamsNonCompliant
	}
	return algorithm, size, nil
}

func updateCertificateParams(istiocsr *v1alpha1.IstioCSR, certificate *certmanagerv1.Certificate) error {
	certificate.Spec.CommonName = istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.CommonName
	if istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.CommonName == "" {
		certificate.Spec.CommonName = fmt.Sprintf(istiodCertificateCommonNameFmt, istiocsr.Spec.IstioCSRConfig.Istio.Namespace)
	}

	dnsNames := make([]string, 0, len(istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.CertificateDNSNames)+len(istiocsr.Spec.IstioCSRConfig.Istio.Revisions))
	dnsNames = append(dnsNames, istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.CertificateDNSNames...)
	dnsNames = append(dnsNames, buildRevisionDNSNames(istiocsr.Spec.IstioCSRConfig.Istio.Revisions, istiocsr.Spec.IstioCSRConfig.Istio.Namespace)...)
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

	algorithm, size, err := resolvePrivateKeyConfig(
		certmanagerv1.PrivateKeyAlgorithm(istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.PrivateKeyAlgorithm),
		int(istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.PrivateKeySize),
	)
	if err != nil {
		return err
	}
	certificate.Spec.PrivateKey.Algorithm = algorithm
	certificate.Spec.PrivateKey.Size = size

	certificate.Spec.IssuerRef = certmanagermetav1.ObjectReference{
		Kind:  istiocsr.Spec.IstioCSRConfig.CertManager.IssuerRef.Kind,
		Group: istiocsr.Spec.IstioCSRConfig.CertManager.IssuerRef.Group,
		Name:  istiocsr.Spec.IstioCSRConfig.CertManager.IssuerRef.Name,
	}
	return nil
}
