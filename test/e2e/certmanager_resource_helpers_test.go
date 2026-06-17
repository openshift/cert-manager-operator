//go:build e2e
// +build e2e

package e2e

import (
	"context"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanagerclientset "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func ensureClusterIssuer(ctx context.Context, client certmanagerclientset.Interface, issuer *certmanagerv1.ClusterIssuer) error {
	_, err := client.CertmanagerV1().ClusterIssuers().Create(ctx, issuer, metav1.CreateOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return err
	}

	existing, getErr := client.CertmanagerV1().ClusterIssuers().Get(ctx, issuer.Name, metav1.GetOptions{})
	if getErr != nil {
		return getErr
	}

	issuer.ResourceVersion = existing.ResourceVersion
	_, err = client.CertmanagerV1().ClusterIssuers().Update(ctx, issuer, metav1.UpdateOptions{})
	return err
}

func ensureIssuer(ctx context.Context, client certmanagerclientset.Interface, issuer *certmanagerv1.Issuer) error {
	_, err := client.CertmanagerV1().Issuers(issuer.Namespace).Create(ctx, issuer, metav1.CreateOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return err
	}

	existing, getErr := client.CertmanagerV1().Issuers(issuer.Namespace).Get(ctx, issuer.Name, metav1.GetOptions{})
	if getErr != nil {
		return getErr
	}

	issuer.ResourceVersion = existing.ResourceVersion
	_, err = client.CertmanagerV1().Issuers(issuer.Namespace).Update(ctx, issuer, metav1.UpdateOptions{})
	return err
}

func ensureCertificate(ctx context.Context, client certmanagerclientset.Interface, certificate *certmanagerv1.Certificate) error {
	_, err := client.CertmanagerV1().Certificates(certificate.Namespace).Create(ctx, certificate, metav1.CreateOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return err
	}

	existing, getErr := client.CertmanagerV1().Certificates(certificate.Namespace).Get(ctx, certificate.Name, metav1.GetOptions{})
	if getErr != nil {
		return getErr
	}

	certificate.ResourceVersion = existing.ResourceVersion
	_, err = client.CertmanagerV1().Certificates(certificate.Namespace).Update(ctx, certificate, metav1.UpdateOptions{})
	return err
}
