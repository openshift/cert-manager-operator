//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	acmev1 "github.com/cert-manager/cert-manager/pkg/apis/acme/v1"
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanagermetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/test/library"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"
)

const (
	istioCSRClusterIssuerName          = "selfsigned-issuer"
	istioCSRCAClusterIssuerName        = "istiocsr-ca-clusterissuer"
	istioCSRCASecretName               = "root-secret"
	istioCSRCACertificateName          = "my-selfsigned-ca"
	istioCSRRejectAnnotation           = "operator.openshift.io/istio-csr-reject-multiple-instance"
	istioCSRManagedResourceLabel       = "app.kubernetes.io/name=cert-manager-istio-csr"
	istioCSRResourceName               = "default"
	istioCSRGRPCServiceName            = "cert-manager-istio-csr"
	istioCSRGRPCServicePortName        = "web"
	istioCSRMissingConfigMapKeyMessage = "not found in ConfigMap"

	istioCSRIstiodTLSSecretName = "istiod-tls"

	// Non-routable ACME directory URL for negative-path tests; operator rejects ACME issuers by type only.
	istioCSRACMEPlaceholderServer = "https://example.invalid/directory"
)

type istioCSRBuildConfig struct {
	issuerRefKind              string
	issuerRefName              string
	serverPort                 int32
	logLevel                   int32
	logFormat                  string
	customCAConfigMapName      string
	customCAConfigMapNamespace string
	customCAConfigMapKey       string
	addServerBlock             bool
	addCustomCAConfigMap       bool
}

func generateMeshWorkloadCSR(meshNamespace, serviceAccountName string) string {
	csrTemplate := &x509.CertificateRequest{
		Subject: pkix.Name{
			Organization: []string{"OpenShift cert-manager IstioCSR E2E"},
		},
		URIs: []*url.URL{
			{
				Scheme: "spiffe",
				Host:   "cluster.local",
				Path:   fmt.Sprintf("/ns/%s/sa/%s", meshNamespace, serviceAccountName),
			},
		},
		SignatureAlgorithm: x509.SHA256WithRSA,
	}
	csr, err := library.GenerateCSR(csrTemplate)
	Expect(err).NotTo(HaveOccurred())
	return csr
}

func copySecretToNamespace(ctx context.Context, clientset *kubernetes.Clientset, sourceNS, targetNS, secretName string) error {
	source, err := clientset.CoreV1().Secrets(sourceNS).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get secret %s/%s: %w", sourceNS, secretName, err)
	}

	copied := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: targetNS,
		},
		Data: source.Data,
		Type: source.Type,
	}
	if err := library.UpsertSecret(ctx, clientset, copied); err != nil {
		return fmt.Errorf("upsert secret %s/%s: %w", targetNS, secretName, err)
	}
	return nil
}

var _ = Describe("Istio-CSR operand coverage [apigroup:operator.openshift.io]", Ordered, Label("Platform:Generic", "Feature:IstioCSR"), func() {
	ctx := context.TODO()
	var clientset *kubernetes.Clientset

	ensureClusterIssuerReady := func() {
		By("ensuring self-signed ClusterIssuer exists")
		clusterIssuer := &certmanagerv1.ClusterIssuer{
			ObjectMeta: metav1.ObjectMeta{
				Name: istioCSRClusterIssuerName,
			},
			Spec: certmanagerv1.IssuerSpec{
				IssuerConfig: certmanagerv1.IssuerConfig{
					SelfSigned: &certmanagerv1.SelfSignedIssuer{},
				},
			},
		}
		Expect(ensureClusterIssuer(ctx, certmanagerClient, clusterIssuer)).NotTo(HaveOccurred())

		By("waiting for self-signed ClusterIssuer readiness")
		Expect(waitForClusterIssuerReadiness(ctx, istioCSRClusterIssuerName)).NotTo(HaveOccurred())
	}

	createIssuerPrerequisites := func(namespace string) {
		By("creating a CA certificate in test namespace")
		loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "certificate.yaml"), namespace)

		By("waiting for CA certificate readiness")
		Expect(waitForCertificateReadiness(ctx, "my-selfsigned-ca", namespace)).NotTo(HaveOccurred())

		By("creating Istio CA issuer")
		loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "istio_ca_issuer.yaml"), namespace)

		DeferCleanup(func() {
			loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "istio_ca_issuer.yaml"), namespace)
			loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "certificate.yaml"), namespace)
		})
	}

	newIstioCSR := func(namespace string, cfg istioCSRBuildConfig) *unstructured.Unstructured {
		issuerRefKind := "Issuer"
		if cfg.issuerRefKind != "" {
			issuerRefKind = cfg.issuerRefKind
		}
		issuerRefName := "istio-ca"
		if cfg.issuerRefName != "" {
			issuerRefName = cfg.issuerRefName
		}

		istioCSR := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "operator.openshift.io/v1alpha1",
				"kind":       "IstioCSR",
				"metadata": map[string]interface{}{
					"name":      istioCSRResourceName,
					"namespace": namespace,
				},
				"spec": map[string]interface{}{
					"istioCSRConfig": map[string]interface{}{
						"certManager": map[string]interface{}{
							"issuerRef": map[string]interface{}{
								"name":  issuerRefName,
								"kind":  issuerRefKind,
								"group": "cert-manager.io",
							},
						},
						"istiodTLSConfig": map[string]interface{}{
							"trustDomain": "cluster.local",
						},
						"istio": map[string]interface{}{
							"namespace": namespace,
						},
					},
				},
			},
		}

		istioCSRConfig := istioCSR.Object["spec"].(map[string]interface{})["istioCSRConfig"].(map[string]interface{})
		if cfg.logLevel > 0 {
			istioCSRConfig["logLevel"] = cfg.logLevel
		}
		if cfg.logFormat != "" {
			istioCSRConfig["logFormat"] = cfg.logFormat
		}
		if cfg.addServerBlock {
			istioCSRConfig["server"] = map[string]interface{}{
				"port": cfg.serverPort,
			}
		}
		if cfg.addCustomCAConfigMap {
			istioCSRConfig["certManager"].(map[string]interface{})["istioCACertificate"] = map[string]interface{}{
				"name": cfg.customCAConfigMapName,
				"key":  cfg.customCAConfigMapKey,
			}
			if cfg.customCAConfigMapNamespace != "" {
				istioCSRConfig["certManager"].(map[string]interface{})["istioCACertificate"].(map[string]interface{})["namespace"] = cfg.customCAConfigMapNamespace
			}
		}

		return istioCSR
	}

	createIstioCSR := func(namespace string, istioCSR *unstructured.Unstructured) {
		_, err := loader.DynamicClient.Resource(istiocsrSchema).Namespace(namespace).Create(ctx, istioCSR, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			err := loader.DynamicClient.Resource(istiocsrSchema).Namespace(namespace).Delete(ctx, istioCSR.GetName(), metav1.DeleteOptions{})
			if err != nil && !apierrors.IsNotFound(err) {
				Expect(err).NotTo(HaveOccurred())
			}
		})
	}

	getGRPCPortFromEndpoint := func(endpoint string) int32 {
		lastColon := strings.LastIndex(endpoint, ":")
		Expect(lastColon).To(BeNumerically(">", 0))
		portValue := endpoint[lastColon+1:]
		port, err := strconv.ParseInt(portValue, 10, 32)
		Expect(err).NotTo(HaveOccurred())
		return int32(port)
	}

	BeforeAll(func() {
		var err error
		clientset, err = kubernetes.NewForConfig(cfg)
		Expect(err).NotTo(HaveOccurred())
		ensureClusterIssuerReady()
	})

	It("should reject IstioCSR with name other than default", Label("ISTIOCSR-001"), func() {
		ns, err := loader.CreateTestingNS("istiocsr-invalid-name", true)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			loader.DeleteTestingNS(ns.Name, func() bool { return CurrentSpecReport().Failed() })
		})

		invalid := newIstioCSR(ns.Name, istioCSRBuildConfig{})
		invalid.SetName("not-default")

		_, err = loader.DynamicClient.Resource(istiocsrSchema).Namespace(ns.Name).Create(ctx, invalid, metav1.CreateOptions{})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("metadata.name"))
	})

	It("should reject processing of second IstioCSR instance across namespaces", Label("ISTIOCSR-002"), func() {
		firstNS, err := loader.CreateTestingNS("istiocsr-first", true)
		Expect(err).NotTo(HaveOccurred())
		secondNS, err := loader.CreateTestingNS("istiocsr-second", true)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			loader.DeleteTestingNS(secondNS.Name, func() bool { return CurrentSpecReport().Failed() })
			loader.DeleteTestingNS(firstNS.Name, func() bool { return CurrentSpecReport().Failed() })
		})

		createIssuerPrerequisites(firstNS.Name)
		createIssuerPrerequisites(secondNS.Name)

		createIstioCSR(firstNS.Name, newIstioCSR(firstNS.Name, istioCSRBuildConfig{}))
		expectIstioCSROperandReady(ctx, clientset, loader, firstNS.Name)

		createIstioCSR(secondNS.Name, newIstioCSR(secondNS.Name, istioCSRBuildConfig{}))
		By("waiting for IstioCSR Ready=False with multiple-instance rejection message")
		Expect(waitForIstioCSRConditionMessage(ctx, loader, secondNS.Name, istioCSRResourceName, v1alpha1.Ready, metav1.ConditionFalse, "multiple instances of istiocsr exists", highTimeout, slowPollInterval)).NotTo(HaveOccurred())

		obj, err := loader.DynamicClient.Resource(istiocsrSchema).Namespace(secondNS.Name).Get(ctx, istioCSRResourceName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(obj.GetAnnotations()).To(HaveKey(istioCSRRejectAnnotation))

		Consistently(func() bool {
			_, err := clientset.AppsV1().Deployments(secondNS.Name).Get(ctx, istioCSRGRPCServiceName, metav1.GetOptions{})
			return apierrors.IsNotFound(err)
		}, "30s", "5s").Should(BeTrue())
	})

	It("should support ClusterIssuer for IstioCSR reconciliation", Label("ISTIOCSR-003"), func() {
		ns, err := loader.CreateTestingNS("istiocsr-clusterissuer", true)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			loader.DeleteTestingNS(ns.Name, func() bool { return CurrentSpecReport().Failed() })
		})

		By("creating CA certificate in test namespace using self-signed ClusterIssuer")
		loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "certificate.yaml"), ns.Name)
		DeferCleanup(func() {
			loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "certificate.yaml"), ns.Name)
		})
		Expect(waitForCertificateReadiness(ctx, istioCSRCACertificateName, ns.Name)).NotTo(HaveOccurred())

		By("copying CA secret to cert-manager namespace for CA ClusterIssuer readiness")
		Expect(copySecretToNamespace(ctx, clientset, ns.Name, operandNamespace, istioCSRCASecretName)).NotTo(HaveOccurred())
		DeferCleanup(func() {
			_ = clientset.CoreV1().Secrets(operandNamespace).Delete(ctx, istioCSRCASecretName, metav1.DeleteOptions{})
		})

		By("creating CA ClusterIssuer backed by root-secret")
		caClusterIssuer := &certmanagerv1.ClusterIssuer{
			ObjectMeta: metav1.ObjectMeta{
				Name: istioCSRCAClusterIssuerName,
			},
			Spec: certmanagerv1.IssuerSpec{
				IssuerConfig: certmanagerv1.IssuerConfig{
					CA: &certmanagerv1.CAIssuer{
						SecretName: istioCSRCASecretName,
					},
				},
			},
		}
		_, err = certmanagerClient.CertmanagerV1().ClusterIssuers().Create(ctx, caClusterIssuer, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			_ = certmanagerClient.CertmanagerV1().ClusterIssuers().Delete(ctx, istioCSRCAClusterIssuerName, metav1.DeleteOptions{})
		})
		Expect(waitForClusterIssuerReadiness(ctx, istioCSRCAClusterIssuerName)).NotTo(HaveOccurred())

		istioCSR := newIstioCSR(ns.Name, istioCSRBuildConfig{
			issuerRefKind: "ClusterIssuer",
			issuerRefName: istioCSRCAClusterIssuerName,
		})
		createIstioCSR(ns.Name, istioCSR)
		status := expectIstioCSROperandReady(ctx, clientset, loader, ns.Name)
		Expect(status.IstioCSRGRPCEndpoint).NotTo(BeEmpty())
	})

	It("should report degraded state for unsupported ACME issuer", Label("ISTIOCSR-004"), func() {
		ns, err := loader.CreateTestingNS("istiocsr-acme", true)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			loader.DeleteTestingNS(ns.Name, func() bool { return CurrentSpecReport().Failed() })
		})

		acmeIssuer := &certmanagerv1.Issuer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "acme-issuer",
				Namespace: ns.Name,
			},
			Spec: certmanagerv1.IssuerSpec{
				IssuerConfig: certmanagerv1.IssuerConfig{
					ACME: &acmev1.ACMEIssuer{
						Email:  "istiocsr@example.com",
						Server: istioCSRACMEPlaceholderServer,
						PrivateKey: certmanagermetav1.SecretKeySelector{
							LocalObjectReference: certmanagermetav1.LocalObjectReference{Name: "acme-private-key"},
						},
					},
				},
			},
		}
		_, err = certmanagerClient.CertmanagerV1().Issuers(ns.Name).Create(ctx, acmeIssuer, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		istioCSR := newIstioCSR(ns.Name, istioCSRBuildConfig{
			issuerRefName: "acme-issuer",
		})
		createIstioCSR(ns.Name, istioCSR)
		By("waiting for IstioCSR Degraded=True with unsupported ACME issuer message")
		Expect(waitForIstioCSRConditionMessage(ctx, loader, ns.Name, istioCSRResourceName, v1alpha1.Degraded, metav1.ConditionTrue, "unsupported ACME issuer", highTimeout, slowPollInterval)).NotTo(HaveOccurred())
	})

	It("should reconcile custom gRPC port to service and status endpoint", Label("ISTIOCSR-005"), func() {
		ns, err := loader.CreateTestingNS("istiocsr-port", true)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			loader.DeleteTestingNS(ns.Name, func() bool { return CurrentSpecReport().Failed() })
		})
		createIssuerPrerequisites(ns.Name)

		istioCSR := newIstioCSR(ns.Name, istioCSRBuildConfig{
			addServerBlock: true,
			serverPort:     7443,
		})
		createIstioCSR(ns.Name, istioCSR)
		status := expectIstioCSROperandReady(ctx, clientset, loader, ns.Name)
		Expect(status.IstioCSRGRPCEndpoint).To(ContainSubstring(":7443"))

		svc, err := clientset.CoreV1().Services(ns.Name).Get(ctx, istioCSRGRPCServiceName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		var grpcPort int32
		for _, p := range svc.Spec.Ports {
			if p.Name == istioCSRGRPCServicePortName {
				grpcPort = p.Port
			}
		}
		Expect(grpcPort).To(Equal(int32(7443)))
	})

	It("should reconcile custom log arguments after deployment drift", Label("ISTIOCSR-006"), func() {
		ns, err := loader.CreateTestingNS("istiocsr-log", true)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			loader.DeleteTestingNS(ns.Name, func() bool { return CurrentSpecReport().Failed() })
		})
		createIssuerPrerequisites(ns.Name)

		istioCSR := newIstioCSR(ns.Name, istioCSRBuildConfig{
			logLevel:  5,
			logFormat: "json",
		})
		createIstioCSR(ns.Name, istioCSR)
		expectIstioCSROperandReady(ctx, clientset, loader, ns.Name)

		deployment, err := clientset.AppsV1().Deployments(ns.Name).Get(ctx, istioCSRGRPCServiceName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(deployment.Spec.Template.Spec.Containers).NotTo(BeEmpty())
		Expect(deployment.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--log-level=5"))
		Expect(deployment.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--log-format=json"))

		deployment.Spec.Template.Spec.Containers[0].Args = []string{"--log-level=1", "--log-format=text"}
		_, err = clientset.AppsV1().Deployments(ns.Name).Update(ctx, deployment, metav1.UpdateOptions{})
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			current, err := clientset.AppsV1().Deployments(ns.Name).Get(ctx, istioCSRGRPCServiceName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(current.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--log-level=5"))
			g.Expect(current.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--log-format=json"))
		}, highTimeout, slowPollInterval).Should(Succeed())
	})

	It("should recreate ServiceAccount when deleted", Label("ISTIOCSR-017"), func() {
		ns, err := loader.CreateTestingNS("istiocsr-sa", true)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			loader.DeleteTestingNS(ns.Name, func() bool { return CurrentSpecReport().Failed() })
		})
		createIssuerPrerequisites(ns.Name)

		createIstioCSR(ns.Name, newIstioCSR(ns.Name, istioCSRBuildConfig{}))
		status := expectIstioCSROperandReady(ctx, clientset, loader, ns.Name)
		Expect(clientset.CoreV1().ServiceAccounts(ns.Name).Delete(ctx, status.ServiceAccount, metav1.DeleteOptions{})).NotTo(HaveOccurred())
		Expect(pollTillServiceAccountAvailable(ctx, clientset, ns.Name, status.ServiceAccount)).NotTo(HaveOccurred())
	})

	It("should reconcile gRPC service drift", Label("ISTIOCSR-018"), func() {
		ns, err := loader.CreateTestingNS("istiocsr-service", true)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			loader.DeleteTestingNS(ns.Name, func() bool { return CurrentSpecReport().Failed() })
		})
		createIssuerPrerequisites(ns.Name)

		createIstioCSR(ns.Name, newIstioCSR(ns.Name, istioCSRBuildConfig{}))
		status := expectIstioCSROperandReady(ctx, clientset, loader, ns.Name)
		expectedPort := getGRPCPortFromEndpoint(status.IstioCSRGRPCEndpoint)

		service, err := clientset.CoreV1().Services(ns.Name).Get(ctx, istioCSRGRPCServiceName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		for i := range service.Spec.Ports {
			if service.Spec.Ports[i].Name == istioCSRGRPCServicePortName {
				service.Spec.Ports[i].Port = 9443
			}
		}
		_, err = clientset.CoreV1().Services(ns.Name).Update(ctx, service, metav1.UpdateOptions{})
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			current, err := clientset.CoreV1().Services(ns.Name).Get(ctx, istioCSRGRPCServiceName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			var grpcPort int32
			for _, p := range current.Spec.Ports {
				if p.Name == istioCSRGRPCServicePortName {
					grpcPort = p.Port
				}
			}
			g.Expect(grpcPort).To(Equal(expectedPort))
		}, highTimeout, slowPollInterval).Should(Succeed())
	})

	It("should recreate deleted network policy", Label("ISTIOCSR-019"), func() {
		ns, err := loader.CreateTestingNS("istiocsr-netpol", true)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			loader.DeleteTestingNS(ns.Name, func() bool { return CurrentSpecReport().Failed() })
		})
		createIssuerPrerequisites(ns.Name)

		createIstioCSR(ns.Name, newIstioCSR(ns.Name, istioCSRBuildConfig{}))
		expectIstioCSROperandReady(ctx, clientset, loader, ns.Name)

		networkPolicies, err := clientset.NetworkingV1().NetworkPolicies(ns.Name).List(ctx, metav1.ListOptions{
			LabelSelector: istioCSRManagedResourceLabel,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(networkPolicies.Items).NotTo(BeEmpty())

		targetPolicy := networkPolicies.Items[0].Name
		Expect(clientset.NetworkingV1().NetworkPolicies(ns.Name).Delete(ctx, targetPolicy, metav1.DeleteOptions{})).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			_, err := clientset.NetworkingV1().NetworkPolicies(ns.Name).Get(ctx, targetPolicy, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
		}, highTimeout, slowPollInterval).Should(Succeed())
	})

	It("should publish complete IstioCSR status contract", Label("ISTIOCSR-020"), func() {
		ns, err := loader.CreateTestingNS("istiocsr-status", true)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			loader.DeleteTestingNS(ns.Name, func() bool { return CurrentSpecReport().Failed() })
		})
		createIssuerPrerequisites(ns.Name)

		createIstioCSR(ns.Name, newIstioCSR(ns.Name, istioCSRBuildConfig{}))
		status := expectIstioCSROperandReady(ctx, clientset, loader, ns.Name)
		Expect(status.IstioCSRImage).NotTo(BeEmpty(), "status.istioCSRImage should be populated")
		Expect(status.IstioCSRGRPCEndpoint).NotTo(BeEmpty(), "status.istioCSRGRPCEndpoint should be populated")
		Expect(status.ServiceAccount).NotTo(BeEmpty(), "status.serviceAccount should be populated")
		Expect(status.ClusterRole).NotTo(BeEmpty(), "status.clusterRole should be populated")
		Expect(status.ClusterRoleBinding).NotTo(BeEmpty(), "status.clusterRoleBinding should be populated")

		Expect(status.IstioCSRGRPCEndpoint).To(ContainSubstring(fmt.Sprintf(".%s.svc:", ns.Name)))
		Expect(getGRPCPortFromEndpoint(status.IstioCSRGRPCEndpoint)).To(BeNumerically(">", 0))

		_, err = clientset.RbacV1().ClusterRoles().Get(ctx, status.ClusterRole, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		_, err = clientset.RbacV1().ClusterRoleBindings().Get(ctx, status.ClusterRoleBinding, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		_, err = clientset.CoreV1().ServiceAccounts(ns.Name).Get(ctx, status.ServiceAccount, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
	})

	It("should report degraded when referenced CA ConfigMap key is missing", Label("ISTIOCSR-023"), func() {
		ns, err := loader.CreateTestingNS("istiocsr-missing-key", true)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			loader.DeleteTestingNS(ns.Name, func() bool { return CurrentSpecReport().Failed() })
		})
		createIssuerPrerequisites(ns.Name)

		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "missing-key-ca",
				Namespace: ns.Name,
			},
			Data: map[string]string{
				"other-key.pem": "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----",
			},
		}
		Expect(library.UpsertConfigMap(ctx, clientset, cm)).NotTo(HaveOccurred())

		istioCSR := newIstioCSR(ns.Name, istioCSRBuildConfig{
			addCustomCAConfigMap:      true,
			customCAConfigMapName:     cm.Name,
			customCAConfigMapNamespace: "",
			customCAConfigMapKey:      "ca-cert.pem",
		})
		createIstioCSR(ns.Name, istioCSR)
		By("waiting for IstioCSR Degraded=True with missing ConfigMap key message")
		Expect(waitForIstioCSRConditionMessage(ctx, loader, ns.Name, istioCSRResourceName, v1alpha1.Degraded, metav1.ConditionTrue, istioCSRMissingConfigMapKeyMessage, highTimeout, slowPollInterval)).NotTo(HaveOccurred())
	})

	It("should report degraded when referenced CA ConfigMap namespace does not exist", Label("ISTIOCSR-027"), func() {
		ns, err := loader.CreateTestingNS("istiocsr-missing-ns", true)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			loader.DeleteTestingNS(ns.Name, func() bool { return CurrentSpecReport().Failed() })
		})
		createIssuerPrerequisites(ns.Name)

		istioCSR := newIstioCSR(ns.Name, istioCSRBuildConfig{
			addCustomCAConfigMap:       true,
			customCAConfigMapName:      "external-ca",
			customCAConfigMapNamespace: "non-existent-istiocsr-ca-ns",
			customCAConfigMapKey:       "ca.crt",
		})
		createIstioCSR(ns.Name, istioCSR)
		By("waiting for IstioCSR Degraded=True with missing CA ConfigMap namespace message")
		Expect(waitForIstioCSRConditionMessage(ctx, loader, ns.Name, istioCSRResourceName, v1alpha1.Degraded, metav1.ConditionTrue, "failed to fetch CA certificate ConfigMap", highTimeout, slowPollInterval)).NotTo(HaveOccurred())
	})

	Context("OpenShift Service Mesh smoke", Label("Feature:IstioCSR-ServiceMesh"), Ordered, func() {
		var (
			istioCPNamespace string
			clusterID        string
			meshMemberNS     *corev1.Namespace
			meshPeerNS       *corev1.Namespace
			nonMemberNS      *corev1.Namespace
			istioCSRStatus   v1alpha1.IstioCSRStatus
		)

		BeforeAll(func() {
			clusterID = deriveClusterID(cfg)

			DeferCleanup(func() {
				_ = cleanupOSSMIstioCSROperand(ctx, loader)
			})

			By("creating istio-csr issuer chain for OSSM v3 smoke")
			err := ensureOSSMIssuerChain(ctx, clientset, certmanagerClient)
			if err != nil {
				Skip(fmt.Sprintf("istio-csr issuer prerequisites not available: %v", err))
			}

			By("creating IstioCSR operand in istio-csr namespace")
			err = ensureOSSMIstioCSROperand(ctx, loader, clusterID)
			Expect(err).NotTo(HaveOccurred())
			istioCSRStatus = expectIstioCSROperandReady(ctx, clientset, loader, ossmIstioCSRNamespace)
			Expect(istioCSRStatus.IstioCSRGRPCEndpoint).NotTo(BeEmpty())
			Expect(istioCSRStatus.ServiceAccount).NotTo(BeEmpty())

			cpNamespace, err := ensureServiceMeshForSmoke(ctx, cfg, loader, clientset, istioCSRStatus.IstioCSRGRPCEndpoint, clusterID)
			if err != nil {
				Skip(fmt.Sprintf("OpenShift Service Mesh v3 not available: %v", err))
			}
			istioCPNamespace = cpNamespace

			meshMemberNS, err = loader.CreateTestingNS("osm-apps-1", true)
			Expect(err).NotTo(HaveOccurred())
			Expect(labelNamespaceForIstioInjection(ctx, clientset, meshMemberNS.Name)).NotTo(HaveOccurred())
			DeferCleanup(func() {
				loader.DeleteTestingNS(meshMemberNS.Name, func() bool { return CurrentSpecReport().Failed() })
			})

			meshPeerNS, err = loader.CreateTestingNS("osm-apps-2", true)
			Expect(err).NotTo(HaveOccurred())
			Expect(labelNamespaceForIstioInjection(ctx, clientset, meshPeerNS.Name)).NotTo(HaveOccurred())
			DeferCleanup(func() {
				loader.DeleteTestingNS(meshPeerNS.Name, func() bool { return CurrentSpecReport().Failed() })
			})

			nonMemberNS, err = loader.CreateTestingNS("osm-non-member", true)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() {
				loader.DeleteTestingNS(nonMemberNS.Name, func() bool { return CurrentSpecReport().Failed() })
			})
		})

		It("should create istio-ca-root-cert in istio-injection=enabled namespace", Label("OSM-SMOKE-TC-001"), func() {
			By("waiting for root CA ConfigMap in istio-injection=enabled member namespace")
			err := pollTillConfigMapAvailable(ctx, clientset, meshMemberNS.Name, "istio-ca-root-cert")
			Expect(err).NotTo(HaveOccurred())

			cm, err := clientset.CoreV1().ConfigMaps(meshMemberNS.Name).Get(ctx, "istio-ca-root-cert", metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(cm.Data).To(HaveKey("root-cert.pem"))
			Expect(cm.Data["root-cert.pem"]).NotTo(BeEmpty())

			By("verifying root CA ConfigMap is not created in a non-injected namespace")
			err = pollTillConfigMapRemains(ctx, clientset, nonMemberNS.Name, "istio-ca-root-cert", lowTimeout)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return cert-chain for mesh workload SPIFFE identity via gRPC [Skipped:Disconnected]", Label("OSM-SMOKE-TC-002"), func() {
			const (
				grpcAppName    = "grpcurl-istio-csr-osm"
				meshWorkloadSA = "mesh-workload"
			)

			By("creating mesh workload service account in injected namespace")
			_, err := clientset.CoreV1().ServiceAccounts(meshMemberNS.Name).Create(ctx, &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      meshWorkloadSA,
					Namespace: meshMemberNS.Name,
				},
			}, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() {
				_ = clientset.CoreV1().ServiceAccounts(meshMemberNS.Name).Delete(ctx, meshWorkloadSA, metav1.DeleteOptions{})
			})

			By("preparing grpcurl job in injected namespace with matching mesh workload SPIFFE URI")
			protoBytes, err := testassets.ReadFile("testdata/ca.proto")
			Expect(err).NotTo(HaveOccurred())
			protoCM := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "proto-cm-osm",
					Namespace: meshMemberNS.Name,
				},
				Data: map[string]string{
					"ca.proto": string(protoBytes),
				},
			}
			Expect(library.UpsertConfigMap(ctx, clientset, protoCM)).NotTo(HaveOccurred())
			DeferCleanup(func() {
				_ = clientset.CoreV1().ConfigMaps(meshMemberNS.Name).Delete(ctx, protoCM.Name, metav1.DeleteOptions{})
			})

			Eventually(func(g Gomega) {
				_, err := clientset.CoreV1().Secrets(istioCPNamespace).Get(ctx, istioCSRIstiodTLSSecretName, metav1.GetOptions{})
				g.Expect(err).NotTo(HaveOccurred())
			}, highTimeout, slowPollInterval).Should(Succeed())

			Expect(copySecretToNamespace(ctx, clientset, istioCPNamespace, meshMemberNS.Name, istioCSRIstiodTLSSecretName)).NotTo(HaveOccurred())

			err = pollTillServiceAccountAvailable(ctx, clientset, meshMemberNS.Name, meshWorkloadSA)
			Expect(err).NotTo(HaveOccurred())

			csr := generateMeshWorkloadCSR(meshMemberNS.Name, meshWorkloadSA)

			loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
				IstioCSRGRPCurlJobConfig{
					CertificateSigningRequest: csr,
					IstioCSRStatus:            istioCSRStatus,
					JobName:                   grpcAppName,
					ProtoConfigMapName:        protoCM.Name,
					ServiceAccountName:        meshWorkloadSA,
				},
			), filepath.Join("testdata", "istio", "grpcurl_job.yaml"), meshMemberNS.Name)
			DeferCleanup(func() {
				policy := metav1.DeletePropagationBackground
				_ = clientset.BatchV1().Jobs(meshMemberNS.Name).Delete(ctx, grpcAppName, metav1.DeleteOptions{PropagationPolicy: &policy})
			})

			Expect(pollTillJobCompleted(ctx, clientset, meshMemberNS.Name, grpcAppName)).NotTo(HaveOccurred())

			pods, err := clientset.CoreV1().Pods(meshMemberNS.Name).List(ctx, metav1.ListOptions{
				LabelSelector: fmt.Sprintf("app=%s", grpcAppName),
			})
			Expect(err).NotTo(HaveOccurred())

			var succeededPodName string
			for _, pod := range pods.Items {
				if pod.Status.Phase == corev1.PodSucceeded {
					succeededPodName = pod.Name
				}
			}
			Expect(succeededPodName).NotTo(BeEmpty())

			logStream, err := clientset.CoreV1().Pods(meshMemberNS.Name).GetLogs(succeededPodName, &corev1.PodLogOptions{}).Stream(ctx)
			Expect(err).NotTo(HaveOccurred())
			defer logStream.Close()

			logData, err := io.ReadAll(logStream)
			Expect(err).NotTo(HaveOccurred())

			entry, err := parseGRPCurlLogEntry(logData)
			Expect(err).NotTo(HaveOccurred())
			Expect(entry.CertChain).NotTo(BeEmpty())

			for _, certPEM := range entry.CertChain {
				Expect(certPEM).NotTo(BeEmpty())
			}
		})

		It("should create istio-csr CertificateRequests when mesh workloads start", Label("OSM-SMOKE-TC-003"), func() {
			By("deploying sample mesh workloads in injected namespaces")
			deployMeshSampleWorkloads(ctx, loader, meshMemberNS.Name)
			deployMeshSampleWorkloads(ctx, loader, meshPeerNS.Name)

			By("waiting for injected sleep and httpbin pods to become ready")
			Expect(waitForInjectedDeploymentReady(ctx, clientset, meshMemberNS.Name, "sleep")).NotTo(HaveOccurred())
			Expect(waitForInjectedDeploymentReady(ctx, clientset, meshMemberNS.Name, "httpbin")).NotTo(HaveOccurred())
			Expect(waitForInjectedDeploymentReady(ctx, clientset, meshPeerNS.Name, "sleep")).NotTo(HaveOccurred())
			Expect(waitForInjectedDeploymentReady(ctx, clientset, meshPeerNS.Name, "httpbin")).NotTo(HaveOccurred())

			By("waiting for istio-csr CertificateRequests in istio-system")
			Expect(waitForCertificateRequestsFromIstioCSR(ctx, istioCPNamespace, 1)).NotTo(HaveOccurred())
		})

		It("should allow cross-namespace mesh traffic between injected namespaces", Label("OSM-SMOKE-TC-004"), func() {
			By("ensuring sample workloads are present in both injected namespaces")
			if _, err := clientset.AppsV1().Deployments(meshMemberNS.Name).Get(ctx, "sleep", metav1.GetOptions{}); err != nil {
				deployMeshSampleWorkloads(ctx, loader, meshMemberNS.Name)
			}
			if _, err := clientset.AppsV1().Deployments(meshPeerNS.Name).Get(ctx, "httpbin", metav1.GetOptions{}); err != nil {
				deployMeshSampleWorkloads(ctx, loader, meshPeerNS.Name)
			}
			Expect(waitForInjectedDeploymentReady(ctx, clientset, meshMemberNS.Name, "sleep")).NotTo(HaveOccurred())
			Expect(waitForInjectedDeploymentReady(ctx, clientset, meshPeerNS.Name, "httpbin")).NotTo(HaveOccurred())

			By("curling peer httpbin service from sleep pod across namespaces")
			Eventually(func(g Gomega) {
				sleepPod, err := getRunningPodName(ctx, clientset, meshMemberNS.Name, "app=sleep")
				g.Expect(err).NotTo(HaveOccurred())

				output, err := execInPod(ctx, cfg, clientset, meshMemberNS.Name, sleepPod, "sleep",
					"curl", "-sIL", fmt.Sprintf("http://httpbin.%s.svc.cluster.local:8000/status/200", meshPeerNS.Name))
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(output).To(ContainSubstring("HTTP/1.1 200"))
				g.Expect(output).To(ContainSubstring("server: envoy"))
			}, highTimeout, slowPollInterval).Should(Succeed())
		})
	})

})
