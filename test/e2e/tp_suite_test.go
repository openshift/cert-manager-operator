//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	v1 "github.com/cert-manager/cert-manager/pkg/apis/acme/v1"
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanagermetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/util/retry"

	configv1 "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/openshift/cert-manager-operator/test/library"
)

func TestTechPreviewSuite(t *testing.T) {
	RegisterFailHandler(Fail)

	suiteConfig, reportConfig := GinkgoConfiguration()
	suiteConfig.LabelFilter = "TechPreview"

	testDir := getTestDir()
	reportConfig.JSONReport = filepath.Join(testDir, "tp-report.json")
	reportConfig.JUnitReport = filepath.Join(testDir, "tp-junit.xml")
	reportConfig.NoColor = true
	reportConfig.VeryVerbose = true

	RunSpecs(t, "OpenShift TechPreview Suite", suiteConfig, reportConfig)
}

var _ = Describe("Route ExternalCertificateRef", Ordered, Label("TechPreview"), func() {
	var ctx context.Context
	var appsDomain string
	var baseDomain string

	BeforeAll(func() {
		By("creating Kube clients")
		ctx = context.Background()
		var err error
		baseDomain, err = library.GetClusterBaseDomain(ctx, configClient)
		Expect(err).NotTo(HaveOccurred())
		Expect(baseDomain).NotTo(BeEmpty())
		appsDomain = "apps." + baseDomain

		By("adding override args for dns-01 private zone passthrough")
		err = addOverrideArgs(certmanageroperatorclient, certmanagerControllerDeployment, []string{
			"--dns01-recursive-nameservers-only",
			"--dns01-recursive-nameservers=8.8.8.8:53,1.1.1.1:53",
		})
		Expect(err).NotTo(HaveOccurred())

		DeferCleanup(func() {
			By("resetting cert-manager state")
			err = resetCertManagerState(ctx, certmanageroperatorclient, loader)
			Expect(err).NotTo(HaveOccurred())
		})

		fgClient := configClient.FeatureGates()
		fg, err := fgClient.Get(ctx, "cluster", metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		routeExternalCertificateRef := false

		if fg.Spec.CustomNoUpgrade != nil {
			for _, enabledFeat := range fg.Spec.CustomNoUpgrade.Enabled {
				if enabledFeat == configv1.FeatureGateName("RouteExternalCertificate") {
					routeExternalCertificateRef = true
					break
				}
			}
		}

		if !routeExternalCertificateRef {
			err = retry.OnError(retry.DefaultRetry, func(err error) bool {
				return true
			}, func() error {
				// Get the secret
				fg, err := fgClient.Get(ctx, "cluster", metav1.GetOptions{})
				if err != nil {
					return err
				}

				fg.Spec.FeatureSet = configv1.CustomNoUpgrade
				fg.Spec.CustomNoUpgrade = &configv1.CustomFeatureGates{
					Enabled: []configv1.FeatureGateName{"RouteExternalCertificate"},
				}

				// Apply the updated secret
				_, updateErr := fgClient.Update(ctx, fg, metav1.UpdateOptions{})
				if updateErr != nil {
					return updateErr
				}

				return nil
			})
			Expect(err).NotTo(HaveOccurred())
		}
	})

	Context("dns-01 challenge using explicit credentials", func() {
		It("should obtain a valid LetsEncrypt certificate", func() {

			By("creating a test namespace")
			ns, err := loader.CreateTestingNS("e2e-route-ref-le")
			Expect(err).NotTo(HaveOccurred())
			defer loader.DeleteTestingNS(ns.Name)

			By("obtaining GCP credentials from kube-system namespace")
			gcpCredsSecret, err := loader.KubeClient.CoreV1().Secrets("kube-system").Get(ctx, "gcp-credentials", metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())

			secretKey := "service_account.json"
			svcAcct := gcpCredsSecret.Data[secretKey]

			By("copying GCP service account to test namespace")
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "gcp-credentials",
					Namespace: ns.Name,
				},
				Data: map[string][]byte{
					secretKey: svcAcct,
				},
			}
			_, err = loader.KubeClient.CoreV1().Secrets(ns.Name).Create(ctx, secret, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("getting GCP project from Infrastructure object")
			infra, err := configClient.Infrastructures().Get(ctx, "cluster", metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())

			gcpProject := infra.Status.PlatformStatus.GCP.ProjectID
			Expect(gcpProject).NotTo(Equal(""))

			By("creating new certificate Issuer")
			issuerName := "letsencrypt-dns01"
			issuer := &certmanagerv1.Issuer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      issuerName,
					Namespace: ns.Name,
				},
				Spec: certmanagerv1.IssuerSpec{
					IssuerConfig: certmanagerv1.IssuerConfig{
						ACME: &v1.ACMEIssuer{
							Server: "https://acme-v02.api.letsencrypt.org/directory", // using Production LetsEncrypt server
							PrivateKey: certmanagermetav1.SecretKeySelector{
								LocalObjectReference: certmanagermetav1.LocalObjectReference{
									Name: "letsencrypt-dns01-issuer",
								},
							},
							Solvers: []v1.ACMEChallengeSolver{
								{
									DNS01: &v1.ACMEChallengeSolverDNS01{
										CloudDNS: &v1.ACMEIssuerDNS01ProviderCloudDNS{
											ServiceAccount: &certmanagermetav1.SecretKeySelector{
												LocalObjectReference: certmanagermetav1.LocalObjectReference{
													Name: secret.Name,
												},
												Key: secretKey,
											},
											Project: gcpProject,
										},
									},
								},
							},
						},
					},
				},
			}
			_, err = certmanagerClient.CertmanagerV1().Issuers(ns.Name).Create(ctx, issuer, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			defer certmanagerClient.CertmanagerV1().Issuers(ns.Name).Delete(ctx, issuerName, metav1.DeleteOptions{})

			By("creating new certificate")
			certDomain := "hello." + appsDomain
			certName := "letsencrypt-cert"
			cert := &certmanagerv1.Certificate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      certName,
					Namespace: ns.Name,
				},
				Spec: certmanagerv1.CertificateSpec{
					IsCA:       false,
					CommonName: certDomain,
					SecretName: certName,
					DNSNames:   []string{certDomain},
					IssuerRef: certmanagermetav1.ObjectReference{
						Name: issuerName,
						Kind: "Issuer",
					},
				},
			}

			_, err = certmanagerClient.CertmanagerV1().Certificates(ns.Name).Create(ctx, cert, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			defer certmanagerClient.CertmanagerV1().Certificates(ns.Name).Delete(ctx, certName, metav1.DeleteOptions{})

			By("creating the rbac for ingress to read the certificate secret")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "route", "role.yaml"), ns.Name)
			defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "route", "role.yaml"), ns.Name)
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "route", "rolebinding.yaml"), ns.Name)
			defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "route", "rolebinding.yaml"), ns.Name)

			By("waiting for certificate to get ready")
			err = waitForCertificateReadiness(ctx, certName, ns.Name)
			Expect(err).NotTo(HaveOccurred())

			By("checking for certificate validity from secret contents")
			err = verifyCertificate(ctx, certName, ns.Name, certDomain)
			Expect(err).NotTo(HaveOccurred())

			By("creating a sample app deployment")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "acme", "deployment.yaml"), ns.Name)
			defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "acme", "deployment.yaml"), ns.Name)

			By("creating the service exposing the deployment")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "acme", "service.yaml"), ns.Name)
			defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "acme", "service.yaml"), ns.Name)

			By("creating a route that exposes the service externally")
			route := &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{
					Name: "hello-app",
				},
				Spec: routev1.RouteSpec{
					Host: certDomain,
					To: routev1.RouteTargetReference{
						Kind: "Service",
						Name: "hello-openshift",
					},
					Port: &routev1.RoutePort{
						TargetPort: intstr.FromString("8080-tcp"),
					},
					TLS: &routev1.TLSConfig{
						Termination: routev1.TLSTerminationEdge,
						ExternalCertificate: &routev1.LocalObjectReference{
							Name: certName,
						},
					},
				},
			}
			_, err = routeClient.Routes(ns.Name).Create(ctx, route, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("sleeping for some time")
			time.Sleep(10 * time.Second)

			By("performing HTTPS GET request on exposed route")
			err = httpsGetCall(fmt.Sprintf("https://%s/", certDomain))
			Expect(err).NotTo(HaveOccurred())
		})
	})
})

func httpsGetCall(url string) error {
	// Create a new HTTP client
	client := &http.Client{}

	// Create a new GET request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}

	// Send the request
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Check if the status code is 200 OK
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http status code %v", resp.StatusCode)
	}

	return nil
}

func TestHTTPS(t *testing.T) {
	err := httpsGetCall("https://expired.badssl.com/")
	if !strings.Contains(err.Error(), "failed to verify certificate") {
		t.Fatalf("expired SSL test failed, this should pass under all circumstance")
	}
}
