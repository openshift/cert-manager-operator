//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

const (
	istioCSRP0ClusterIssuerName          = "selfsigned-issuer"
	istioCSRP0CAClusterIssuerName        = "istiocsr-p0-ca-clusterissuer"
	istioCSRP0CASecretName               = "root-secret"
	istioCSRP0CACertificateName          = "my-selfsigned-ca"
	istioCSRP0RejectAnnotation           = "operator.openshift.io/istio-csr-reject-multiple-instance"
	istioCSRP0ManagedResourceLabel       = "app.kubernetes.io/name=cert-manager-istio-csr"
	istioCSRP0ManagedByLabel             = "app.kubernetes.io/managed-by"
	istioCSRP0ManagedByExpectedValue     = "cert-manager-operator"
	istioCSRP0ManagedAppLabel            = "app"
	istioCSRP0ManagedAppExpectedValue    = "cert-manager-istio-csr"
	istioCSRP0ISTIOCSRName               = "default"
	istioCSRP0GRPCServiceName            = "cert-manager-istio-csr"
	istioCSRP0GRPCServicePortName        = "web"
	istioCSRP0MissingConfigMapKeyMessage = "not found in ConfigMap"

	istioCSRP0MaistraMemberOfLabel = "maistra.io/member-of"
	istioCSRP0IstiodTLSSecretName  = "istiod-tls"

	// Non-routable ACME directory URL for negative-path tests; operator rejects ACME issuers by type only.
	istioCSRP0ACMEPlaceholderServer = "https://example.invalid/directory"

	// istioCSRP0IstiodWaitTimeout is how long OSM smoke tests wait for a ready istiod before skipping.
	istioCSRP0IstiodWaitTimeout = 15 * time.Minute
)

type istioCSRP0Config struct {
	issuerRefKind                 string
	issuerRefName                 string
	serverPort                    int32
	logLevel                      int32
	logFormat                     string
	istioControlPlaneNamespace    string
	istioDataPlaneSelector        string
	customCAConfigMapName         string
	customCAConfigMapNamespace    string
	customCAConfigMapKey          string
	controllerConfigLabels        map[string]string
	addServerBlock                bool
	addIstioDataPlaneSelector     bool
	addCustomCAConfigMap          bool
	addControllerConfigLabels     bool
}

// discoverIstiodControlPlaneNamespace returns the namespace of a ready istiod deployment, if any.
func discoverIstiodControlPlaneNamespace(ctx context.Context, clientset *kubernetes.Clientset) (string, bool, error) {
	deployments, err := clientset.AppsV1().Deployments("").List(ctx, metav1.ListOptions{
		LabelSelector: "app=istiod",
	})
	if err != nil {
		return "", false, err
	}
	for _, deployment := range deployments.Items {
		if deployment.Status.ReadyReplicas > 0 {
			return deployment.Namespace, true, nil
		}
	}

	allDeployments, err := clientset.AppsV1().Deployments("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", false, err
	}
	for _, deployment := range allDeployments.Items {
		if deployment.Name != "istiod" && !strings.HasPrefix(deployment.Name, "istiod-") {
			continue
		}
		if deployment.Status.ReadyReplicas > 0 {
			return deployment.Namespace, true, nil
		}
	}
	return "", false, nil
}

// waitForIstiodControlPlaneNamespace polls until a ready istiod deployment exists or timeout expires.
func waitForIstiodControlPlaneNamespace(ctx context.Context, clientset *kubernetes.Clientset, timeout time.Duration) (string, error) {
	var controlPlaneNamespace string
	err := wait.PollUntilContextTimeout(ctx, fastPollInterval, timeout, true, func(context.Context) (bool, error) {
		namespace, found, err := discoverIstiodControlPlaneNamespace(ctx, clientset)
		if err != nil {
			return false, err
		}
		if !found {
			return false, nil
		}
		controlPlaneNamespace = namespace
		return true, nil
	})
	if err != nil {
		return "", fmt.Errorf("istiod control plane not available after %s: %w", timeout, err)
	}
	return controlPlaneNamespace, nil
}

func generateMeshWorkloadCSR(meshNamespace, serviceAccountName string) string {
	csrTemplate := &x509.CertificateRequest{
		Subject: pkix.Name{
			Organization: []string{"OpenShift Service Mesh E2E"},
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

func copySecretToNamespace(ctx context.Context, clientset *kubernetes.Clientset, sourceNS, targetNS, secretName string) {
	source, err := clientset.CoreV1().Secrets(sourceNS).Get(ctx, secretName, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred())

	copied := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: targetNS,
		},
		Data: source.Data,
		Type: source.Type,
	}
	_, err = clientset.CoreV1().Secrets(targetNS).Create(ctx, copied, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		Expect(err).NotTo(HaveOccurred())
	}
}

var _ = Describe("Istio-CSR P0 coverage [apigroup:operator.openshift.io]", Ordered, Label("Platform:Generic", "Feature:IstioCSR"), func() {
	ctx := context.TODO()
	var clientset *kubernetes.Clientset

	ensureClusterIssuerReady := func() {
		By("ensuring self-signed ClusterIssuer exists")
		clusterIssuer := &certmanagerv1.ClusterIssuer{
			ObjectMeta: metav1.ObjectMeta{
				Name: istioCSRP0ClusterIssuerName,
			},
			Spec: certmanagerv1.IssuerSpec{
				IssuerConfig: certmanagerv1.IssuerConfig{
					SelfSigned: &certmanagerv1.SelfSignedIssuer{},
				},
			},
		}
		_, err := certmanagerClient.CertmanagerV1().ClusterIssuers().Create(ctx, clusterIssuer, metav1.CreateOptions{})
		if err != nil && !apierrors.IsAlreadyExists(err) {
			Expect(err).NotTo(HaveOccurred())
		}

		By("waiting for self-signed ClusterIssuer readiness")
		Expect(waitForClusterIssuerReadiness(ctx, istioCSRP0ClusterIssuerName)).NotTo(HaveOccurred())
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

	newIstioCSR := func(namespace string, cfg istioCSRP0Config) *unstructured.Unstructured {
		issuerRefKind := "Issuer"
		if cfg.issuerRefKind != "" {
			issuerRefKind = cfg.issuerRefKind
		}
		issuerRefName := "istio-ca"
		if cfg.issuerRefName != "" {
			issuerRefName = cfg.issuerRefName
		}
		istioNamespace := namespace
		if cfg.istioControlPlaneNamespace != "" {
			istioNamespace = cfg.istioControlPlaneNamespace
		}

		istioCSR := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "operator.openshift.io/v1alpha1",
				"kind":       "IstioCSR",
				"metadata": map[string]interface{}{
					"name":      istioCSRP0ISTIOCSRName,
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
							"namespace": istioNamespace,
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
		if cfg.addIstioDataPlaneSelector {
			istioCSRConfig["istioDataPlaneNamespaceSelector"] = cfg.istioDataPlaneSelector
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
		if cfg.addControllerConfigLabels {
			istioCSR.Object["spec"].(map[string]interface{})["controllerConfig"] = map[string]interface{}{
				"labels": cfg.controllerConfigLabels,
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

	waitForIstioCSRReady := func(namespace string) {
		By("waiting for IstioCSR to become ready")
		err := pollTillDeploymentAvailable(ctx, clientset, namespace, "cert-manager-istio-csr")
		Expect(err).NotTo(HaveOccurred())

		_, err = pollTillIstioCSRAvailable(ctx, loader, namespace, istioCSRP0ISTIOCSRName)
		Expect(err).NotTo(HaveOccurred())
	}

	getIstioCSRStatus := func(namespace string) map[string]interface{} {
		obj, err := loader.DynamicClient.Resource(istiocsrSchema).Namespace(namespace).Get(ctx, istioCSRP0ISTIOCSRName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		status, found, err := unstructured.NestedMap(obj.Object, "status")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		return status
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

	It("should reject IstioCSR with name other than default", Label("ISTIOCSR-P0-001"), func() {
		ns, err := loader.CreateTestingNS("istiocsr-invalid-name", true)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			loader.DeleteTestingNS(ns.Name, func() bool { return CurrentSpecReport().Failed() })
		})

		invalid := newIstioCSR(ns.Name, istioCSRP0Config{})
		invalid.SetName("not-default")

		_, err = loader.DynamicClient.Resource(istiocsrSchema).Namespace(ns.Name).Create(ctx, invalid, metav1.CreateOptions{})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("metadata.name"))
	})

	It("should reject processing of second IstioCSR instance across namespaces", Label("ISTIOCSR-P0-002"), func() {
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

		createIstioCSR(firstNS.Name, newIstioCSR(firstNS.Name, istioCSRP0Config{}))
		waitForIstioCSRReady(firstNS.Name)

		createIstioCSR(secondNS.Name, newIstioCSR(secondNS.Name, istioCSRP0Config{}))
		By("waiting for IstioCSR Ready=False with multiple-instance rejection message")
		Expect(waitForIstioCSRConditionMessage(ctx, loader, secondNS.Name, istioCSRP0ISTIOCSRName, v1alpha1.Ready, metav1.ConditionFalse, "multiple instances of istiocsr exists", highTimeout, slowPollInterval)).NotTo(HaveOccurred())

		obj, err := loader.DynamicClient.Resource(istiocsrSchema).Namespace(secondNS.Name).Get(ctx, istioCSRP0ISTIOCSRName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(obj.GetAnnotations()).To(HaveKey(istioCSRP0RejectAnnotation))

		Consistently(func() bool {
			_, err := clientset.AppsV1().Deployments(secondNS.Name).Get(ctx, istioCSRP0GRPCServiceName, metav1.GetOptions{})
			return apierrors.IsNotFound(err)
		}, "30s", "5s").Should(BeTrue())
	})

	It("should support ClusterIssuer for IstioCSR reconciliation", Label("ISTIOCSR-P0-003"), func() {
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
		Expect(waitForCertificateReadiness(ctx, istioCSRP0CACertificateName, ns.Name)).NotTo(HaveOccurred())

		By("copying CA secret to cert-manager namespace for CA ClusterIssuer readiness")
		copySecretToNamespace(ctx, clientset, ns.Name, operandNamespace, istioCSRP0CASecretName)
		DeferCleanup(func() {
			_ = clientset.CoreV1().Secrets(operandNamespace).Delete(ctx, istioCSRP0CASecretName, metav1.DeleteOptions{})
		})

		By("creating CA ClusterIssuer backed by root-secret")
		caClusterIssuer := &certmanagerv1.ClusterIssuer{
			ObjectMeta: metav1.ObjectMeta{
				Name: istioCSRP0CAClusterIssuerName,
			},
			Spec: certmanagerv1.IssuerSpec{
				IssuerConfig: certmanagerv1.IssuerConfig{
					CA: &certmanagerv1.CAIssuer{
						SecretName: istioCSRP0CASecretName,
					},
				},
			},
		}
		_, err = certmanagerClient.CertmanagerV1().ClusterIssuers().Create(ctx, caClusterIssuer, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			_ = certmanagerClient.CertmanagerV1().ClusterIssuers().Delete(ctx, istioCSRP0CAClusterIssuerName, metav1.DeleteOptions{})
		})
		Expect(waitForClusterIssuerReadiness(ctx, istioCSRP0CAClusterIssuerName)).NotTo(HaveOccurred())

		istioCSR := newIstioCSR(ns.Name, istioCSRP0Config{
			issuerRefKind: "ClusterIssuer",
			issuerRefName: istioCSRP0CAClusterIssuerName,
		})
		createIstioCSR(ns.Name, istioCSR)
		waitForIstioCSRReady(ns.Name)

		status := getIstioCSRStatus(ns.Name)
		Expect(status["istioCSRGRPCEndpoint"]).NotTo(BeEmpty())
	})

	It("should report degraded state for unsupported ACME issuer", Label("ISTIOCSR-P0-004"), func() {
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
						Server: istioCSRP0ACMEPlaceholderServer,
						PrivateKey: certmanagermetav1.SecretKeySelector{
							LocalObjectReference: certmanagermetav1.LocalObjectReference{Name: "acme-private-key"},
						},
					},
				},
			},
		}
		_, err = certmanagerClient.CertmanagerV1().Issuers(ns.Name).Create(ctx, acmeIssuer, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		istioCSR := newIstioCSR(ns.Name, istioCSRP0Config{
			issuerRefName: "acme-issuer",
		})
		createIstioCSR(ns.Name, istioCSR)
		By("waiting for IstioCSR Degraded=True with unsupported ACME issuer message")
		Expect(waitForIstioCSRConditionMessage(ctx, loader, ns.Name, istioCSRP0ISTIOCSRName, v1alpha1.Degraded, metav1.ConditionTrue, "unsupported ACME issuer", highTimeout, slowPollInterval)).NotTo(HaveOccurred())
	})

	It("should reconcile custom gRPC port to service and status endpoint", Label("ISTIOCSR-P0-005"), func() {
		ns, err := loader.CreateTestingNS("istiocsr-port", true)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			loader.DeleteTestingNS(ns.Name, func() bool { return CurrentSpecReport().Failed() })
		})
		createIssuerPrerequisites(ns.Name)

		istioCSR := newIstioCSR(ns.Name, istioCSRP0Config{
			addServerBlock: true,
			serverPort:     7443,
		})
		createIstioCSR(ns.Name, istioCSR)
		waitForIstioCSRReady(ns.Name)

		status := getIstioCSRStatus(ns.Name)
		endpoint := status["istioCSRGRPCEndpoint"].(string)
		Expect(endpoint).To(ContainSubstring(":7443"))

		svc, err := clientset.CoreV1().Services(ns.Name).Get(ctx, istioCSRP0GRPCServiceName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		var grpcPort int32
		for _, p := range svc.Spec.Ports {
			if p.Name == istioCSRP0GRPCServicePortName {
				grpcPort = p.Port
			}
		}
		Expect(grpcPort).To(Equal(int32(7443)))
	})

	It("should reconcile custom log arguments after deployment drift", Label("ISTIOCSR-P0-006"), func() {
		ns, err := loader.CreateTestingNS("istiocsr-log", true)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			loader.DeleteTestingNS(ns.Name, func() bool { return CurrentSpecReport().Failed() })
		})
		createIssuerPrerequisites(ns.Name)

		istioCSR := newIstioCSR(ns.Name, istioCSRP0Config{
			logLevel:  5,
			logFormat: "json",
		})
		createIstioCSR(ns.Name, istioCSR)
		waitForIstioCSRReady(ns.Name)

		deployment, err := clientset.AppsV1().Deployments(ns.Name).Get(ctx, istioCSRP0GRPCServiceName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(deployment.Spec.Template.Spec.Containers).NotTo(BeEmpty())
		Expect(deployment.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--log-level=5"))
		Expect(deployment.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--log-format=json"))

		deployment.Spec.Template.Spec.Containers[0].Args = []string{"--log-level=1", "--log-format=text"}
		_, err = clientset.AppsV1().Deployments(ns.Name).Update(ctx, deployment, metav1.UpdateOptions{})
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			current, err := clientset.AppsV1().Deployments(ns.Name).Get(ctx, istioCSRP0GRPCServiceName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(current.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--log-level=5"))
			g.Expect(current.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--log-format=json"))
		}, highTimeout, slowPollInterval).Should(Succeed())
	})

	It("should recreate ServiceAccount when deleted", Label("ISTIOCSR-P0-017"), func() {
		ns, err := loader.CreateTestingNS("istiocsr-sa", true)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			loader.DeleteTestingNS(ns.Name, func() bool { return CurrentSpecReport().Failed() })
		})
		createIssuerPrerequisites(ns.Name)

		createIstioCSR(ns.Name, newIstioCSR(ns.Name, istioCSRP0Config{}))
		waitForIstioCSRReady(ns.Name)

		status := getIstioCSRStatus(ns.Name)
		serviceAccountName := status["serviceAccount"].(string)
		Expect(clientset.CoreV1().ServiceAccounts(ns.Name).Delete(ctx, serviceAccountName, metav1.DeleteOptions{})).NotTo(HaveOccurred())
		Expect(pollTillServiceAccountAvailable(ctx, clientset, ns.Name, serviceAccountName)).NotTo(HaveOccurred())
	})

	It("should reconcile gRPC service drift", Label("ISTIOCSR-P0-018"), func() {
		ns, err := loader.CreateTestingNS("istiocsr-service", true)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			loader.DeleteTestingNS(ns.Name, func() bool { return CurrentSpecReport().Failed() })
		})
		createIssuerPrerequisites(ns.Name)

		createIstioCSR(ns.Name, newIstioCSR(ns.Name, istioCSRP0Config{}))
		waitForIstioCSRReady(ns.Name)

		status := getIstioCSRStatus(ns.Name)
		expectedPort := getGRPCPortFromEndpoint(status["istioCSRGRPCEndpoint"].(string))

		service, err := clientset.CoreV1().Services(ns.Name).Get(ctx, istioCSRP0GRPCServiceName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		for i := range service.Spec.Ports {
			if service.Spec.Ports[i].Name == istioCSRP0GRPCServicePortName {
				service.Spec.Ports[i].Port = 9443
			}
		}
		_, err = clientset.CoreV1().Services(ns.Name).Update(ctx, service, metav1.UpdateOptions{})
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			current, err := clientset.CoreV1().Services(ns.Name).Get(ctx, istioCSRP0GRPCServiceName, metav1.GetOptions{})
			g.Expect(err).NotTo(HaveOccurred())
			var grpcPort int32
			for _, p := range current.Spec.Ports {
				if p.Name == istioCSRP0GRPCServicePortName {
					grpcPort = p.Port
				}
			}
			g.Expect(grpcPort).To(Equal(expectedPort))
		}, highTimeout, slowPollInterval).Should(Succeed())
	})

	It("should recreate deleted network policy", Label("ISTIOCSR-P0-019"), func() {
		ns, err := loader.CreateTestingNS("istiocsr-netpol", true)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			loader.DeleteTestingNS(ns.Name, func() bool { return CurrentSpecReport().Failed() })
		})
		createIssuerPrerequisites(ns.Name)

		createIstioCSR(ns.Name, newIstioCSR(ns.Name, istioCSRP0Config{}))
		waitForIstioCSRReady(ns.Name)

		networkPolicies, err := clientset.NetworkingV1().NetworkPolicies(ns.Name).List(ctx, metav1.ListOptions{
			LabelSelector: istioCSRP0ManagedResourceLabel,
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

	It("should publish complete IstioCSR status contract", Label("ISTIOCSR-P0-020"), func() {
		ns, err := loader.CreateTestingNS("istiocsr-status", true)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			loader.DeleteTestingNS(ns.Name, func() bool { return CurrentSpecReport().Failed() })
		})
		createIssuerPrerequisites(ns.Name)

		createIstioCSR(ns.Name, newIstioCSR(ns.Name, istioCSRP0Config{}))
		waitForIstioCSRReady(ns.Name)

		status := getIstioCSRStatus(ns.Name)
		for _, field := range []string{"istioCSRImage", "istioCSRGRPCEndpoint", "serviceAccount", "clusterRole", "clusterRoleBinding"} {
			Expect(status[field]).NotTo(BeEmpty(), "status.%s should be populated", field)
		}

		endpoint := status["istioCSRGRPCEndpoint"].(string)
		Expect(endpoint).To(ContainSubstring(fmt.Sprintf(".%s.svc:", ns.Name)))
		Expect(getGRPCPortFromEndpoint(endpoint)).To(BeNumerically(">", 0))

		_, err = clientset.RbacV1().ClusterRoles().Get(ctx, status["clusterRole"].(string), metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		_, err = clientset.RbacV1().ClusterRoleBindings().Get(ctx, status["clusterRoleBinding"].(string), metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		_, err = clientset.CoreV1().ServiceAccounts(ns.Name).Get(ctx, status["serviceAccount"].(string), metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
	})

	It("should report degraded when referenced CA ConfigMap key is missing", Label("ISTIOCSR-P0-023"), func() {
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
		_, err = clientset.CoreV1().ConfigMaps(ns.Name).Create(ctx, cm, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		istioCSR := newIstioCSR(ns.Name, istioCSRP0Config{
			addCustomCAConfigMap:      true,
			customCAConfigMapName:     cm.Name,
			customCAConfigMapNamespace: "",
			customCAConfigMapKey:      "ca-cert.pem",
		})
		createIstioCSR(ns.Name, istioCSR)
		By("waiting for IstioCSR Degraded=True with missing ConfigMap key message")
		Expect(waitForIstioCSRConditionMessage(ctx, loader, ns.Name, istioCSRP0ISTIOCSRName, v1alpha1.Degraded, metav1.ConditionTrue, istioCSRP0MissingConfigMapKeyMessage, highTimeout, slowPollInterval)).NotTo(HaveOccurred())
	})

	It("should report degraded when referenced CA ConfigMap namespace does not exist", Label("ISTIOCSR-P0-027"), func() {
		ns, err := loader.CreateTestingNS("istiocsr-missing-ns", true)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			loader.DeleteTestingNS(ns.Name, func() bool { return CurrentSpecReport().Failed() })
		})
		createIssuerPrerequisites(ns.Name)

		istioCSR := newIstioCSR(ns.Name, istioCSRP0Config{
			addCustomCAConfigMap:       true,
			customCAConfigMapName:      "external-ca",
			customCAConfigMapNamespace: "non-existent-istiocsr-ca-ns",
			customCAConfigMapKey:       "ca.crt",
		})
		createIstioCSR(ns.Name, istioCSR)
		By("waiting for IstioCSR Degraded=True with missing CA ConfigMap namespace message")
		Expect(waitForIstioCSRConditionMessage(ctx, loader, ns.Name, istioCSRP0ISTIOCSRName, v1alpha1.Degraded, metav1.ConditionTrue, "failed to fetch CA certificate ConfigMap", highTimeout, slowPollInterval)).NotTo(HaveOccurred())
	})

	Context("OpenShift Service Mesh smoke", Label("Feature:ServiceMesh"), Ordered, func() {
		var (
			istioCPNamespace string
			crNamespace      string
			meshMemberNS     *corev1.Namespace
			nonMemberNS      *corev1.Namespace
			istioCSRStatus   v1alpha1.IstioCSRStatus
		)

		BeforeAll(func() {
			By(fmt.Sprintf("waiting up to %s for a ready istiod control plane", istioCSRP0IstiodWaitTimeout))
			cpNamespace, err := waitForIstiodControlPlaneNamespace(ctx, clientset, istioCSRP0IstiodWaitTimeout)
			if err != nil {
				Skip(fmt.Sprintf("OpenShift Service Mesh / istiod control plane not available: %v", err))
			}
			istioCPNamespace = cpNamespace

			crNS, err := loader.CreateTestingNS("istiocsr-osm", true)
			Expect(err).NotTo(HaveOccurred())
			crNamespace = crNS.Name
			DeferCleanup(func() {
				loader.DeleteTestingNS(crNamespace, func() bool { return CurrentSpecReport().Failed() })
			})

			By(fmt.Sprintf("using istiod control-plane namespace %s", istioCPNamespace))
			createIssuerPrerequisites(istioCPNamespace)

			maistraSelector := fmt.Sprintf("%s=%s", istioCSRP0MaistraMemberOfLabel, istioCPNamespace)
			createIstioCSR(crNamespace, newIstioCSR(crNamespace, istioCSRP0Config{
				istioControlPlaneNamespace: istioCPNamespace,
				addIstioDataPlaneSelector:  true,
				istioDataPlaneSelector:     maistraSelector,
			}))
			waitForIstioCSRReady(crNamespace)

			statusMap := getIstioCSRStatus(crNamespace)
			Expect(statusMap["istioCSRGRPCEndpoint"]).NotTo(BeEmpty())
			Expect(statusMap["serviceAccount"]).NotTo(BeEmpty())

			var err2 error
			istioCSRStatus, err2 = pollTillIstioCSRAvailable(ctx, loader, crNamespace, istioCSRP0ISTIOCSRName)
			Expect(err2).NotTo(HaveOccurred())

			meshMemberNS, err = loader.CreateTestingNS("osm-member", true)
			Expect(err).NotTo(HaveOccurred())
			Eventually(func(g Gomega) {
				ns, getErr := clientset.CoreV1().Namespaces().Get(ctx, meshMemberNS.Name, metav1.GetOptions{})
				g.Expect(getErr).NotTo(HaveOccurred())
				if ns.Labels == nil {
					ns.Labels = map[string]string{}
				}
				ns.Labels[istioCSRP0MaistraMemberOfLabel] = istioCPNamespace
				updated, updateErr := clientset.CoreV1().Namespaces().Update(ctx, ns, metav1.UpdateOptions{})
				g.Expect(updateErr).NotTo(HaveOccurred())
				meshMemberNS = updated
			}, lowTimeout, fastPollInterval).Should(Succeed())
			DeferCleanup(func() {
				loader.DeleteTestingNS(meshMemberNS.Name, func() bool { return CurrentSpecReport().Failed() })
			})

			nonMemberNS, err = loader.CreateTestingNS("osm-non-member", true)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() {
				loader.DeleteTestingNS(nonMemberNS.Name, func() bool { return CurrentSpecReport().Failed() })
			})
		})

		It("should create istio-ca-root-cert in OSM member namespace with Maistra selector", Label("OSM-SMOKE-TC-001"), func() {
			By("waiting for root CA ConfigMap in Maistra-labeled member namespace")
			err := pollTillConfigMapAvailable(ctx, clientset, meshMemberNS.Name, "istio-ca-root-cert")
			Expect(err).NotTo(HaveOccurred())

			cm, err := clientset.CoreV1().ConfigMaps(meshMemberNS.Name).Get(ctx, "istio-ca-root-cert", metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			Expect(cm.Data).To(HaveKey("root-cert.pem"))
			Expect(cm.Data["root-cert.pem"]).NotTo(BeEmpty())

			By("verifying root CA ConfigMap is not created in a non-member namespace")
			err = pollTillConfigMapRemains(ctx, clientset, nonMemberNS.Name, "istio-ca-root-cert", lowTimeout)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return cert-chain for mesh workload SPIFFE identity via gRPC [Skipped:Disconnected]", Label("OSM-SMOKE-TC-002"), func() {
			const (
				grpcAppName          = "grpcurl-istio-csr-osm"
				meshWorkloadSA       = "mesh-workload"
			)

			By("preparing grpcurl job in IstioCSR operand namespace with mesh workload SPIFFE URI")
			protoBytes, err := testassets.ReadFile("testdata/ca.proto")
			Expect(err).NotTo(HaveOccurred())
			protoCM := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "proto-cm-osm",
					Namespace: crNamespace,
				},
				Data: map[string]string{
					"ca.proto": string(protoBytes),
				},
			}
			_, err = clientset.CoreV1().ConfigMaps(crNamespace).Create(ctx, protoCM, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() {
				_ = clientset.CoreV1().ConfigMaps(crNamespace).Delete(ctx, protoCM.Name, metav1.DeleteOptions{})
			})

			Eventually(func(g Gomega) {
				_, err := clientset.CoreV1().Secrets(istioCPNamespace).Get(ctx, istioCSRP0IstiodTLSSecretName, metav1.GetOptions{})
				g.Expect(err).NotTo(HaveOccurred())
			}, highTimeout, slowPollInterval).Should(Succeed())

			copySecretToNamespace(ctx, clientset, istioCPNamespace, crNamespace, istioCSRP0IstiodTLSSecretName)

			err = pollTillServiceAccountAvailable(ctx, clientset, crNamespace, istioCSRStatus.ServiceAccount)
			Expect(err).NotTo(HaveOccurred())

			csr := generateMeshWorkloadCSR(meshMemberNS.Name, meshWorkloadSA)

			loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
				IstioCSRGRPCurlJobConfig{
					CertificateSigningRequest: csr,
					IstioCSRStatus:            istioCSRStatus,
					JobName:                   grpcAppName,
				},
			), filepath.Join("testdata", "istio", "grpcurl_job.yaml"), crNamespace)
			DeferCleanup(func() {
				policy := metav1.DeletePropagationBackground
				_ = clientset.BatchV1().Jobs(crNamespace).Delete(ctx, grpcAppName, metav1.DeleteOptions{PropagationPolicy: &policy})
			})

			Expect(pollTillJobCompleted(ctx, clientset, crNamespace, grpcAppName)).NotTo(HaveOccurred())

			pods, err := clientset.CoreV1().Pods(crNamespace).List(ctx, metav1.ListOptions{
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

			logStream, err := clientset.CoreV1().Pods(crNamespace).GetLogs(succeededPodName, &corev1.PodLogOptions{}).Stream(ctx)
			Expect(err).NotTo(HaveOccurred())
			defer logStream.Close()

			logData, err := io.ReadAll(logStream)
			Expect(err).NotTo(HaveOccurred())

			var entry LogEntry
			Expect(json.Unmarshal(logData, &entry)).NotTo(HaveOccurred())
			Expect(entry.CertChain).NotTo(BeEmpty())

			for _, certPEM := range entry.CertChain {
				Expect(library.ValidateCertificate(certPEM, "my-selfsigned-ca")).NotTo(HaveOccurred())
			}
		})
	})

})
