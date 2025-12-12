//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	routev1 "github.com/openshift/api/route/v1"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanagermetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Self-signed Issuer", Ordered, func() {
	var ctx context.Context
	var cancel context.CancelFunc
	var ns *corev1.Namespace

	const (
		rootSecretName = "root-secret"
		caIssuerName   = "my-ca-issuer"
	)

	// countReadyCertificates counts the number of ready certificates
	countReadyCertificates := func(certNames []string) int {
		readyCount := 0
		for _, name := range certNames {
			cert, err := certmanagerClient.CertmanagerV1().Certificates(ns.Name).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				GinkgoLogr.Error(err, "Error getting certificate", "name", name)
				continue
			}
			for _, cond := range cert.Status.Conditions {
				if cond.Type == certmanagerv1.CertificateConditionReady && cond.Status == certmanagermetav1.ConditionTrue {
					readyCount++
					break
				}
			}
		}
		return readyCount
	}

	BeforeAll(func() {
		var err error
		ctx = context.Background()

		By("creating a test namespace")
		ns, err = loader.CreateTestingNS("e2e-selfsigned-ca", false)
		Expect(err).NotTo(HaveOccurred(), "failed to create test namespace")
		DeferCleanup(func() {
			By("cleaning up test namespace")
			loader.DeleteTestingNS(ns.Name, func() bool { return CurrentSpecReport().Failed() })
		})

		By("creating a self-signed ClusterIssuer")
		clusterIssuerName := "selfsigned-clusterissuer"
		clusterIssuer := &certmanagerv1.ClusterIssuer{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterIssuerName,
			},
			Spec: certmanagerv1.IssuerSpec{
				IssuerConfig: certmanagerv1.IssuerConfig{
					SelfSigned: &certmanagerv1.SelfSignedIssuer{},
				},
			},
		}
		_, err = certmanagerClient.CertmanagerV1().ClusterIssuers().Create(ctx, clusterIssuer, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to create self-signed ClusterIssuer")
		DeferCleanup(func(ctx context.Context) {
			if err := certmanagerClient.CertmanagerV1().ClusterIssuers().Delete(ctx, clusterIssuerName, metav1.DeleteOptions{}); err != nil {
				fmt.Fprintf(GinkgoWriter, "failed to delete ClusterIssuer during cleanup: %v\n", err)
			}
		})

		By("waiting for self-signed ClusterIssuer to become ready")
		err = waitForClusterIssuerReadiness(ctx, clusterIssuerName)
		Expect(err).NotTo(HaveOccurred(), "timeout waiting for ClusterIssuer to become ready")

		By("creating a certificate using the self-signed ClusterIssuer")
		certName := "my-selfsigned-ca"
		cert := &certmanagerv1.Certificate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      certName,
				Namespace: ns.Name,
			},
			Spec: certmanagerv1.CertificateSpec{
				IsCA:       true,
				CommonName: certName,
				SecretName: rootSecretName,
				PrivateKey: &certmanagerv1.CertificatePrivateKey{
					Algorithm: certmanagerv1.ECDSAKeyAlgorithm,
					Size:      256,
				},
				IssuerRef: certmanagermetav1.ObjectReference{
					Name: clusterIssuerName,
					Kind: "ClusterIssuer",
				},
			},
		}
		_, err = certmanagerClient.CertmanagerV1().Certificates(ns.Name).Create(ctx, cert, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to create certificate")

		By("waiting for certificate to get ready")
		err = waitForCertificateReadiness(ctx, certName, ns.Name)
		Expect(err).NotTo(HaveOccurred(), "timeout waiting for certificate to become ready")

		By("checking for certificate validity from secret contents")
		err = verifyCertificate(ctx, cert.Spec.SecretName, ns.Name, cert.Spec.CommonName)
		Expect(err).NotTo(HaveOccurred(), "certificate verification failed")

		By("creating CA issuer")
		issuer := &certmanagerv1.Issuer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      caIssuerName,
				Namespace: ns.Name,
			},
			Spec: certmanagerv1.IssuerSpec{
				IssuerConfig: certmanagerv1.IssuerConfig{
					CA: &certmanagerv1.CAIssuer{
						SecretName: rootSecretName,
					},
				},
			},
		}
		_, err = certmanagerClient.CertmanagerV1().Issuers(ns.Name).Create(ctx, issuer, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to create CA issuer")

		By("waiting for CA Issuer to become ready")
		err = waitForIssuerReadiness(ctx, caIssuerName, ns.Name)
		Expect(err).NotTo(HaveOccurred(), "timeout waiting for issuer to become ready")
	})

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), highTimeout)
		DeferCleanup(cancel)

		By("waiting for operator status to become available")
		err := VerifyHealthyOperatorConditions(certmanageroperatorclient.OperatorV1alpha1())
		Expect(err).NotTo(HaveOccurred(), "Operator is expected to be available")
	})

	Context("with CA issued certificate", func() {
		It("should obtain a certificate using CA", func() {

			By("creating a certificate using the CA Issuer")
			certName := "my-ca-issued-cert"
			cert := &certmanagerv1.Certificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      certName,
					Namespace: ns.Name,
				},
				Spec: certmanagerv1.CertificateSpec{
					DNSNames:   []string{"sample-ca-issued-cert"},
					SecretName: certName,
					IssuerRef: certmanagermetav1.ObjectReference{
						Name: caIssuerName,
						Kind: "Issuer",
					},
				},
			}
			_, err := certmanagerClient.CertmanagerV1().Certificates(ns.Name).Create(ctx, cert, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create certificate")

			By("waiting for certificate to get ready")
			err = waitForCertificateReadiness(ctx, certName, ns.Name)
			Expect(err).NotTo(HaveOccurred(), "timeout waiting for certificate to become ready")

			By("checking for certificate validity from secret contents")
			err = verifyCertificate(ctx, cert.Spec.SecretName, ns.Name, cert.Spec.DNSNames[0])
			Expect(err).NotTo(HaveOccurred(), "certificate verification failed")
		})

		It("should not delete the TLS secret by default when its Certificate CR is deleted", Label("TechPreview"), func() {

			By("creating a certificate using the CA Issuer")
			certName := "test-secret-retention-cert"
			cert := &certmanagerv1.Certificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      certName,
					Namespace: ns.Name,
				},
				Spec: certmanagerv1.CertificateSpec{
					CommonName: certName,
					SecretName: certName,
					IssuerRef: certmanagermetav1.ObjectReference{
						Name: caIssuerName,
						Kind: "Issuer",
					},
				},
			}
			_, err := certmanagerClient.CertmanagerV1().Certificates(ns.Name).Create(ctx, cert, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create certificate")

			By("waiting for certificate to get ready")
			err = waitForCertificateReadiness(ctx, certName, ns.Name)
			Expect(err).NotTo(HaveOccurred(), "timeout waiting for certificate to become ready")

			By("deleting the issued certificate")
			err = certmanagerClient.CertmanagerV1().Certificates(ns.Name).Delete(ctx, certName, metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to delete certificate")

			By("verifying the TLS secret is not deleted")
			Consistently(func() error {
				_, err := loader.KubeClient.CoreV1().Secrets(ns.Name).Get(ctx, certName, metav1.GetOptions{})
				return err
			}, 30*time.Second, 5*time.Second).Should(Succeed(), "TLS secret should not be deleted when Certificate is deleted")
		})

		It("should obtain another certificate using CA and renew it", func() {

			By("creating a certificate using the CA Issuer")
			certName := "renewable-ca-issued-cert"
			cert := &certmanagerv1.Certificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      certName,
					Namespace: ns.Name,
				},
				Spec: certmanagerv1.CertificateSpec{
					IsCA:        false,
					DNSNames:    []string{"sample-renewable-cert"},
					SecretName:  certName,
					Duration:    &metav1.Duration{Duration: time.Hour},
					RenewBefore: &metav1.Duration{Duration: time.Minute * 59}, // essentially becomes a renewal loop of 1min
					IssuerRef: certmanagermetav1.ObjectReference{
						Name: caIssuerName,
						Kind: "Issuer",
					},
				},
			}
			cert, err := certmanagerClient.CertmanagerV1().Certificates(ns.Name).Create(ctx, cert, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create certificate")

			By("waiting for certificate to get ready")
			err = waitForCertificateReadiness(ctx, certName, ns.Name)
			Expect(err).NotTo(HaveOccurred(), "timeout waiting for certificate to become ready")

			By("verifying certificate was renewed at least once")
			err = verifyCertificateRenewed(ctx, cert.Spec.SecretName, ns.Name, time.Minute+slowPollInterval) // using wait period of (1min+jitter)=65s
			Expect(err).NotTo(HaveOccurred(), "certificate was not renewed")
		})

		It("should prevent flood of re-issuance attempts when certificates have duplicate secretName", Label("TechPreview"), func() {

			By("creating 3 certificates with the same secretName")
			duplicateSecretName := "secret-duplicate"
			certNames := []string{"duplicate-cert-1", "duplicate-cert-2", "duplicate-cert-3"}
			for _, name := range certNames {
				cert := &certmanagerv1.Certificate{
					ObjectMeta: metav1.ObjectMeta{
						Name:      name,
						Namespace: ns.Name,
					},
					Spec: certmanagerv1.CertificateSpec{
						CommonName: name,
						DNSNames:   []string{"svc.cluster.local"},
						Usages:     []certmanagerv1.KeyUsage{certmanagerv1.UsageServerAuth},
						SecretName: duplicateSecretName,
						IssuerRef: certmanagermetav1.ObjectReference{
							Kind: "Issuer",
							Name: caIssuerName,
						},
					},
				}
				_, err := certmanagerClient.CertmanagerV1().Certificates(ns.Name).Create(ctx, cert, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred(), "failed to create certificate "+name)
			}

			By("checking consistently that only 0 or 1 CertificateRequest is created")
			Consistently(func() bool {
				requestNum := 0
				certRequests, err := certmanagerClient.CertmanagerV1().CertificateRequests(ns.Name).List(ctx, metav1.ListOptions{})
				if err != nil {
					GinkgoLogr.Error(err, "Error getting certificaterequest")
					return false
				}
				for _, name := range certNames {
					for _, cr := range certRequests.Items {
						if cr.Annotations["cert-manager.io/certificate-name"] == name {
							requestNum++
						}
					}
				}
				return requestNum <= 1
			}, 10*time.Second, 1*time.Second).Should(BeTrue(), "expect only 0 or 1 CertificateRequest to be created")

			By("checking consistently that at most 1 Certificate is Ready")
			Consistently(func() bool {
				readyCount := countReadyCertificates(certNames)
				return readyCount <= 1
			}, 10*time.Second, 1*time.Second).Should(BeTrue(), "expect at most 1 Certificate to be Ready")

			By("updating Certificates to ensure all have unique secretName")
			for i, name := range certNames {
				cert, err := certmanagerClient.CertmanagerV1().Certificates(ns.Name).Get(ctx, name, metav1.GetOptions{})
				Expect(err).NotTo(HaveOccurred(), "failed to get certificate "+name)

				cert.Spec.SecretName = fmt.Sprintf("secret-%d", i)
				_, err = certmanagerClient.CertmanagerV1().Certificates(ns.Name).Update(ctx, cert, metav1.UpdateOptions{})
				Expect(err).NotTo(HaveOccurred(), "failed to update certificate "+name)
			}

			By("verifying all Certificates become Ready")
			Eventually(func() bool {
				readyCount := countReadyCertificates(certNames)
				return readyCount == len(certNames)
			}, lowTimeout, fastPollInterval).Should(BeTrue(), "expect all Certificates to be Ready")
		})

		It("should be able to manage Route external TLS secret", Label("TechPreview"), func() {

			By("deploying hello-openshift application")
			appName := "hello-openshift"
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "acme", "deployment.yaml"), ns.Name)
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "acme", "service.yaml"), ns.Name)

			By("waiting for hello-openshift deployment to be ready")
			err := pollTillDeploymentAvailable(ctx, k8sClientSet, ns.Name, appName)
			Expect(err).NotTo(HaveOccurred(), "timeout waiting for deployment to become available")

			By("getting cluster ingress domain")
			ingress, err := configClient.Ingresses().Get(ctx, "cluster", metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to get cluster ingress")
			ingressDomain := ingress.Spec.Domain
			Expect(ingressDomain).NotTo(BeEmpty(), "cluster ingress domain should not be empty")

			By("constructing hostname for the route")
			hostname := fmt.Sprintf("route-test-%s.%s", randomStr(4), ingressDomain)

			By("creating an edge Route")
			routeName := "my-edge-route"
			route := &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name:      routeName,
					Namespace: ns.Name,
				},
				Spec: routev1.RouteSpec{
					Host: hostname,
					To: routev1.RouteTargetReference{
						Kind: "Service",
						Name: appName,
					},
					TLS: &routev1.TLSConfig{
						Termination: routev1.TLSTerminationEdge,
					},
				},
			}
			_, err = routeClient.Routes(ns.Name).Create(ctx, route, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create route")

			By("creating a certificate for the route")
			certName := "test-route-external-cert"
			cert := &certmanagerv1.Certificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      certName,
					Namespace: ns.Name,
				},
				Spec: certmanagerv1.CertificateSpec{
					DNSNames:    []string{hostname},
					SecretName:  certName,
					Duration:    &metav1.Duration{Duration: time.Hour},
					RenewBefore: &metav1.Duration{Duration: time.Minute * 58}, // essentially becomes a renewal loop of 2min
					IssuerRef: certmanagermetav1.ObjectReference{
						Name: caIssuerName,
						Kind: "Issuer",
					},
				},
			}
			_, err = certmanagerClient.CertmanagerV1().Certificates(ns.Name).Create(ctx, cert, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create certificate")

			By("waiting for certificate to become ready")
			err = waitForCertificateReadiness(ctx, certName, ns.Name)
			Expect(err).NotTo(HaveOccurred(), "timeout waiting for certificate to become ready")

			By("creating RBAC to allow router service account to read the secret")
			role := &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "secret-reader",
					Namespace: ns.Name,
				},
				Rules: []rbacv1.PolicyRule{
					{
						APIGroups:     []string{""},
						Resources:     []string{"secrets"},
						Verbs:         []string{"get", "list", "watch"},
						ResourceNames: []string{cert.Spec.SecretName},
					},
				},
			}
			_, err = k8sClientSet.RbacV1().Roles(ns.Name).Create(ctx, role, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create RBAC role")

			roleBinding := &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "secret-reader-binding",
					Namespace: ns.Name,
				},
				Subjects: []rbacv1.Subject{
					{
						Kind:      "ServiceAccount",
						Name:      "router",
						Namespace: "openshift-ingress",
					},
				},
				RoleRef: rbacv1.RoleRef{
					Kind:     "Role",
					Name:     "secret-reader",
					APIGroup: "rbac.authorization.k8s.io",
				},
			}
			_, err = k8sClientSet.RbacV1().RoleBindings(ns.Name).Create(ctx, roleBinding, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create RBAC role binding")

			By("patching the route with externalCertificate reference")
			route, err = routeClient.Routes(ns.Name).Get(ctx, routeName, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to get route")

			route.Spec.TLS.ExternalCertificate = &routev1.LocalObjectReference{
				Name: cert.Spec.SecretName,
			}

			_, err = routeClient.Routes(ns.Name).Update(ctx, route, metav1.UpdateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to update route")

			By("getting the certificate from the secret to use as CA for verification")
			certSecret, err := k8sClientSet.CoreV1().Secrets(ns.Name).Get(ctx, cert.Spec.SecretName, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to get certificate secret")
			caCert := certSecret.Data["ca.crt"]
			Expect(caCert).NotTo(BeEmpty(), "CA certificate should not be empty")

			By("waiting for route to serve the certificate and respond to HTTPS requests")
			Eventually(func() error {
				return httpsGetCallWithCA(fmt.Sprintf("https://%s/", hostname), caCert)
			}, lowTimeout, fastPollInterval).Should(Succeed(), "route should eventually serve HTTPS traffic with the certificate")
		})
	})
})
