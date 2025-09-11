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
	"log"
	"net/url"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	testutils "github.com/openshift/cert-manager-operator/pkg/controller/istiocsr"
	"github.com/openshift/cert-manager-operator/test/library"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// backOffLimit is the max retries for the Job
const backOffLimit int32 = 10

// istioCSRProtoURL links to proto for istio-csr API spec
const istioCSRProtoURL = "https://raw.githubusercontent.com/istio/api/v1.24.1/security/v1alpha1/ca.proto"

type LogEntry struct {
	CertChain []string `json:"certChain"`
}

type IstioCSRConfig struct {
	ClusterID string
}

var _ = Describe("Istio-CSR", Ordered, Label("TechPreview", "Feature:IstioCSR"), func() {
	ctx := context.TODO()
	var clientset *kubernetes.Clientset

	generateCSR := func() string {
		csrTemplate := &x509.CertificateRequest{
			Subject: pkix.Name{
				Organization:       []string{"My Organization"},
				OrganizationalUnit: []string{"IT Department"},
				Country:            []string{"US"},
				Locality:           []string{"Los Angeles"},
				Province:           []string{"California"},
			},
			URIs: []*url.URL{
				{Scheme: "spiffe", Host: "cluster.local", Path: "/ns/istio-system/sa/cert-manager-istio-csr"},
			},
			SignatureAlgorithm: x509.SHA256WithRSA,
		}

		csr, err := library.GenerateCSR(csrTemplate)
		Expect(err).Should(BeNil())
		return csr
	}

	waitForIstioCSRReady := func(ns *corev1.Namespace) v1alpha1.IstioCSRStatus {
		By("poll till cert-manager-istio-csr deployment is available")
		err := pollTillDeploymentAvailable(ctx, clientset, ns.Name, "cert-manager-istio-csr")
		Expect(err).Should(BeNil())

		By("poll till istiocsr object is available")
		istioCSRStatus, err := pollTillIstioCSRAvailable(ctx, loader, ns.Name, "default")
		Expect(err).Should(BeNil())

		return istioCSRStatus
	}

	BeforeAll(func() {
		var err error
		clientset, err = kubernetes.NewForConfig(cfg)
		Expect(err).Should(BeNil())

		By("enable IstioCSR addon feature by patching subscription object")
		err = patchSubscriptionWithEnvVars(ctx, loader, map[string]string{
			"UNSUPPORTED_ADDON_FEATURES": "IstioCSR=true",
			"OPERATOR_LOG_LEVEL":         "6",
		})
		Expect(err).NotTo(HaveOccurred())
	})

	var ns *corev1.Namespace

	BeforeEach(func() {
		By("waiting for operator status to become available")
		err := VerifyHealthyOperatorConditions(certmanageroperatorclient.OperatorV1alpha1())
		Expect(err).NotTo(HaveOccurred(), "Operator is expected to be available")

		By("creating a test namespace")
		namespace, err := loader.CreateTestingNS("istio-system", true)
		Expect(err).NotTo(HaveOccurred())
		ns = namespace

		DeferCleanup(func() {
			By("deleting cluster-scoped RBAC resources of the istio-csr agent")
			clientset.RbacV1().ClusterRoles().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
				LabelSelector: "app.kubernetes.io/name=cert-manager-istio-csr",
			})
			clientset.RbacV1().ClusterRoleBindings().DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
				LabelSelector: "app.kubernetes.io/name=cert-manager-istio-csr",
			})

			By("deleting the test namespace")
			loader.DeleteTestingNS(ns.Name, func() bool { return CurrentSpecReport().Failed() })
		})
	})

	Context("grpc call istio.v1.auth.IstioCertificateService/CreateCertificate to istio-csr agent", func() {
		BeforeEach(func() {
			By("creating cluster issuer")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "cluster_issuer.yaml"), ns.Name)
			DeferCleanup(func() {
				loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "cluster_issuer.yaml"), ns.Name)
			})

			By("issuing TLS certificate")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "certificate.yaml"), ns.Name)
			DeferCleanup(func() {
				loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "certificate.yaml"), ns.Name)
			})

			err := waitForCertificateReadiness(ctx, "my-selfsigned-ca", ns.Name)
			Expect(err).NotTo(HaveOccurred())

			By("fetching proto file from api")
			protoContent, err := library.FetchFileFromURL(istioCSRProtoURL)
			Expect(err).Should(BeNil())
			Expect(protoContent).NotTo(BeEmpty())

			By("creating proto config map")
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "proto-cm",
					Namespace: ns.Name,
				},
				Data: map[string]string{
					"ca.proto": protoContent,
				},
			}
			_, err = clientset.CoreV1().ConfigMaps(ns.Name).Create(ctx, configMap, metav1.CreateOptions{})
			Expect(err).Should(BeNil())
			DeferCleanup(func() {
				clientset.CoreV1().ConfigMaps(ns.Name).Delete(ctx, configMap.Name, metav1.DeleteOptions{})
			})

			By("creating istio-ca issuer")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "istio_ca_issuer.yaml"), ns.Name)
			DeferCleanup(func() {
				loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "istio_ca_issuer.yaml"), ns.Name)
			})
		})

		It("should return cert-chain as response", func() {
			serviceAccountName := "cert-manager-istio-csr"
			grpcAppName := "grpcurl-istio-csr"

			By("creating istiocsr.operator.openshift.io resource")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "istio_csr.yaml"), ns.Name)
			defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "istio_csr.yaml"), ns.Name)

			istioCSRStatus := waitForIstioCSRReady(ns)

			By("poll till the service account is available")
			err := pollTillServiceAccountAvailable(ctx, clientset, ns.Name, serviceAccountName)
			Expect(err).Should(BeNil())

			By("generate csr request")
			csr := generateCSR()

			By("creating an grpcurl job")
			loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
				IstioCSRGRPCurlJobConfig{
					CertificateSigningRequest: csr,
					IstioCSRStatus:            istioCSRStatus,
				},
			), filepath.Join("testdata", "istio", "grpcurl_job.yaml"), ns.Name)
			policy := metav1.DeletePropagationBackground
			defer clientset.BatchV1().Jobs(ns.Name).Delete(ctx, grpcAppName, metav1.DeleteOptions{PropagationPolicy: &policy})

			By("waiting for the job to be completed")
			err = pollTillJobCompleted(ctx, clientset, ns.Name, grpcAppName)
			Expect(err).Should(BeNil())

			By("fetching logs of the grpcurl job")
			pods, err := clientset.CoreV1().Pods(ns.Name).List(context.TODO(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("app=%s", grpcAppName),
			})
			Expect(err).Should(BeNil())

			By("fetching succeeded pod name")
			var succeededPodName string
			for _, pod := range pods.Items {
				if pod.Status.Phase == corev1.PodSucceeded {
					succeededPodName = pod.Name
				}
			}
			Expect(succeededPodName).ShouldNot(BeEmpty())

			req := clientset.CoreV1().Pods(ns.Name).GetLogs(succeededPodName, &corev1.PodLogOptions{})
			logs, err := req.Stream(context.TODO())
			Expect(err).Should(BeNil())
			defer logs.Close()

			logData, err := io.ReadAll(logs)
			Expect(err).Should(BeNil())

			var entry LogEntry
			err = json.Unmarshal(logData, &entry)
			Expect(err).Should(BeNil())
			Expect(entry.CertChain).ShouldNot(BeEmpty())

			By("validating each certificate")
			for _, certPEM := range entry.CertChain {
				err = library.ValidateCertificate(certPEM, "my-selfsigned-ca")
				Expect(err).Should(BeNil())
			}
		})

		It("should accept gRPC calls with correct clusterID", func() {
			grpcAppName := "grpcurl-istio-csr-cluster-id"

			By("creating istiocsr.operator.openshift.io resource with custom clusterID")
			loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
				IstioCSRConfig{
					ClusterID: clusterName,
				},
			), filepath.Join("testdata", "istio", "istio_csr_custom_cluster_id.yaml"), ns.Name)
			defer loader.DeleteFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
				IstioCSRConfig{
					ClusterID: clusterName,
				},
			), filepath.Join("testdata", "istio", "istio_csr_custom_cluster_id.yaml"), ns.Name)

			istioCSRStatus := waitForIstioCSRReady(ns)

			By("poll till the service account is available")
			err := pollTillServiceAccountAvailable(ctx, clientset, ns.Name, "cert-manager-istio-csr")
			Expect(err).Should(BeNil())

			By("generate csr request")
			csr := generateCSR()

			By("creating grpcurl job with matching clusterID")
			loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
				IstioCSRGRPCurlJobConfig{
					CertificateSigningRequest: csr,
					IstioCSRStatus:            istioCSRStatus,
					ClusterID:                 clusterName, // matches the IstioCSR resource
					JobName:                   grpcAppName,
				},
			), filepath.Join("testdata", "istio", "grpcurl_job_with_cluster_id.yaml"), ns.Name)
			policy := metav1.DeletePropagationBackground
			defer clientset.BatchV1().Jobs(ns.Name).Delete(ctx, grpcAppName, metav1.DeleteOptions{PropagationPolicy: &policy})

			By("waiting for the job to be completed")
			err = pollTillJobCompleted(ctx, clientset, ns.Name, grpcAppName)
			Expect(err).Should(BeNil())

			By("verifying successful certificate response")
			pods, err := clientset.CoreV1().Pods(ns.Name).List(context.TODO(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("app=%s", grpcAppName),
			})
			Expect(err).Should(BeNil())

			var succeededPodName string
			for _, pod := range pods.Items {
				if pod.Status.Phase == corev1.PodSucceeded {
					succeededPodName = pod.Name
				}
			}
			Expect(succeededPodName).ShouldNot(BeEmpty())

			req := clientset.CoreV1().Pods(ns.Name).GetLogs(succeededPodName, &corev1.PodLogOptions{})
			logs, err := req.Stream(context.TODO())
			Expect(err).Should(BeNil())
			defer logs.Close()

			logData, err := io.ReadAll(logs)
			Expect(err).Should(BeNil())

			var entry LogEntry
			err = json.Unmarshal(logData, &entry)
			Expect(err).Should(BeNil())
			Expect(entry.CertChain).ShouldNot(BeEmpty())
		})

		It("should reject gRPC calls with wrong clusterID", func() {
			grpcAppName := "grpcurl-istio-csr-wrong-cluster-id"

			By("creating istiocsr.operator.openshift.io resource with custom clusterID")
			loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
				IstioCSRConfig{
					ClusterID: clusterName,
				},
			), filepath.Join("testdata", "istio", "istio_csr_custom_cluster_id.yaml"), ns.Name)
			defer loader.DeleteFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
				IstioCSRConfig{
					ClusterID: clusterName,
				},
			), filepath.Join("testdata", "istio", "istio_csr_custom_cluster_id.yaml"), ns.Name)

			istioCSRStatus := waitForIstioCSRReady(ns)

			By("poll till the service account is available")
			err := pollTillServiceAccountAvailable(ctx, clientset, ns.Name, "cert-manager-istio-csr")
			Expect(err).Should(BeNil())

			By("generate csr request")
			csr := generateCSR()

			By("creating grpcurl job with wrong clusterID")
			loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
				IstioCSRGRPCurlJobConfig{
					CertificateSigningRequest: csr,
					IstioCSRStatus:            istioCSRStatus,
					ClusterID:                 "wrong-cluster-id", // doesn't match the IstioCSR resource
					JobName:                   grpcAppName,
				},
			), filepath.Join("testdata", "istio", "grpcurl_job_with_cluster_id.yaml"), ns.Name)
			policy := metav1.DeletePropagationBackground
			defer clientset.BatchV1().Jobs(ns.Name).Delete(ctx, grpcAppName, metav1.DeleteOptions{PropagationPolicy: &policy})

			By("waiting for the job to fail or timeout")
			// This job should fail because clusterID doesn't match
			err = pollTillJobFailed(ctx, clientset, ns.Name, grpcAppName)
			Expect(err).Should(BeNil())

			By("verifying job failed due to clusterID mismatch")
			pods, err := clientset.CoreV1().Pods(ns.Name).List(context.TODO(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("app=%s", grpcAppName),
			})
			Expect(err).Should(BeNil())

			// Check that the pod failed (no successful pod should exist)
			var succeededPodName string
			for _, pod := range pods.Items {
				if pod.Status.Phase == corev1.PodSucceeded {
					succeededPodName = pod.Name
				}
			}
			Expect(succeededPodName).Should(BeEmpty(), "Job should have failed due to wrong clusterID")
		})
	})

	Context("with CA Certificate ConfigMap", func() {

		const (
			configMapRefName = "test-ca-certificate"
			configMapRefKey  = "ca-cert.pem"
		)

		BeforeEach(func() {
			By("creating cluster issuer")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "cluster_issuer.yaml"), ns.Name)

			By("issuing TLS certificate")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "certificate.yaml"), ns.Name)

			err := waitForCertificateReadiness(ctx, "my-selfsigned-ca", ns.Name)
			Expect(err).NotTo(HaveOccurred())

			By("creating istio-ca issuer")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "istio_ca_issuer.yaml"), ns.Name)
		})

		AfterEach(func() {
			By("cleaning up cluster issuer")
			loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "cluster_issuer.yaml"), ns.Name)

			By("cleaning up TLS certificate")
			loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "certificate.yaml"), ns.Name)

			By("cleaning up istio-ca issuer")
			loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "istio_ca_issuer.yaml"), ns.Name)
		})

		It("should successfully use CA certificate from ConfigMap in same namespace", func() {
			By("Creating a CA certificate ConfigMap")
			caCertConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapRefName,
					Namespace: ns.Name, // same namespace as IstioCSR
				},
				Data: map[string]string{
					configMapRefKey: testutils.GenerateCertificate("Test CA E2E", []string{"cert-manager-operator"}, func(cert *x509.Certificate) {
						cert.IsCA = true
						cert.KeyUsage |= x509.KeyUsageCertSign
					}),
				},
			}
			_, err := clientset.CoreV1().ConfigMaps(caCertConfigMap.Namespace).Create(ctx, caCertConfigMap, metav1.CreateOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			// By("Verifying ConfigMap exists before creating IstioCSR")
			// _, err = clientset.CoreV1().ConfigMaps(ns.Name).Get(ctx, configMapRefName, metav1.GetOptions{})
			// Expect(err).ShouldNot(HaveOccurred(), "ConfigMap should exist before creating IstioCSR")

			By("Creating IstioCSR resource")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "istiocsr_with_ca_certificate.yaml"), ns.Name)
			defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "istiocsr_with_ca_certificate.yaml"), ns.Name)

			By("waiting for IstioCSR to be ready and deployment to be created")
			// Add some debugging before calling waitForIstioCSRReady
			// Eventually(func() error {
			// 	istiocsrClient := loader.DynamicClient.Resource(istiocsrSchema).Namespace(ns.Name)
			// 	customResource, err := istiocsrClient.Get(ctx, "default", metav1.GetOptions{})
			// 	if err != nil {
			// 		return fmt.Errorf("IstioCSR not found: %v", err)
			// 	}

			// 	status, found, err := unstructured.NestedMap(customResource.Object, "status")
			// 	if err != nil {
			// 		return fmt.Errorf("failed to extract status: %v", err)
			// 	}
			// 	if !found {
			// 		return fmt.Errorf("status not found in IstioCSR")
			// 	}

			// 	// Print status for debugging
			// 	fmt.Printf("DEBUG: IstioCSR status: %+v\n", status)
			// 	return nil
			// }, "30s", "5s").Should(Succeed(), "Should be able to get IstioCSR status for debugging")

			istioCSRStatus := waitForIstioCSRReady(ns)
			log.Printf("IstioCSR status: %+v", istioCSRStatus)

			By("Verifying that the CA certificate ConfigMap gets processed")
			// Wait for the controller to process and copy the ConfigMap
			var sourceCertData string
			Eventually(func() bool {
				// Get the source ConfigMap data
				sourceConfigMap, err := clientset.CoreV1().ConfigMaps(caCertConfigMap.Namespace).Get(ctx, caCertConfigMap.Name, metav1.GetOptions{})
				if err != nil {
					return false
				}
				sourceCertData = sourceConfigMap.Data[configMapRefKey]

				// Check if the copied ConfigMap exists and has the same data
				copiedConfigMap, err := clientset.CoreV1().ConfigMaps(ns.Name).Get(ctx, testutils.IstiocsrCAConfigMapName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				copiedCertData := copiedConfigMap.Data[testutils.IstiocsrCAKeyName]

				return sourceCertData != "" && copiedCertData != "" && copiedCertData == sourceCertData

			}, "2m", "5s").Should(BeTrue(), "CA certificate should be copied to IstioCSR namespace with identical content")

			By("Verifying watch label was added to source ConfigMap")
			Eventually(func() bool {
				sourceConfigMap, err := clientset.CoreV1().ConfigMaps(caCertConfigMap.Namespace).Get(ctx, caCertConfigMap.Name, metav1.GetOptions{})
				if err != nil {
					return false
				}
				_, hasWatchLabel := sourceConfigMap.Labels[testutils.IstiocsrResourceWatchLabelName]
				return hasWatchLabel
			}, "1m", "5s").Should(BeTrue(), "Source ConfigMap should have watch label")
		})

		It("should fail when CA certificate is not actually a CA certificate", func() {
			By("Creating a ConfigMap with a non-CA certificate")
			nonCACertConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapRefName,
					Namespace: ns.Name, // same namespace as IstioCSR
				},
				Data: map[string]string{
					configMapRefKey: testutils.GenerateCertificate("Test Non-CA E2E", []string{"cert-manager-operator"}, func(cert *x509.Certificate) {
						cert.IsCA = false
					}),
				},
			}
			_, err := clientset.CoreV1().ConfigMaps(nonCACertConfigMap.Namespace).Create(ctx, nonCACertConfigMap, metav1.CreateOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			By("Creating IstioCSR resource")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "istiocsr_with_ca_certificate.yaml"), ns.Name)
			defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "istiocsr_with_ca_certificate.yaml"), ns.Name)

			By("Verifying that IstioCSR deployment fails due to non-CA certificate")
			// The deployment should fail and not become ready due to certificate validation
			Consistently(func() error {
				_, err := pollTillIstioCSRAvailable(ctx, loader, ns.Name, "default")
				return err
			}, "30s", "5s").Should(HaveOccurred(), "IstioCSR should fail to become ready due to non-CA certificate")
		})

		It("should successfully use CA certificate from ConfigMap in custom namespace", func() {
			By("Creating a custom namespace for the source ConfigMap")
			customNamespace, err := loader.CreateTestingNS("custom-ca-ns", false)
			Expect(err).ShouldNot(HaveOccurred())
			defer loader.DeleteTestingNS(customNamespace.Name, func() bool { return false })

			By("Creating a CA certificate ConfigMap in the custom namespace")
			caCertConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapRefName,
					Namespace: customNamespace.Name,
				},
				Data: map[string]string{
					configMapRefKey: testutils.GenerateCertificate("Custom Namespace CA E2E", []string{"cert-manager-operator"}, func(cert *x509.Certificate) {
						cert.IsCA = true
						cert.KeyUsage |= x509.KeyUsageCertSign
					}),
				},
			}
			_, err = clientset.CoreV1().ConfigMaps(caCertConfigMap.Namespace).Create(ctx, caCertConfigMap, metav1.CreateOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			By("Creating IstioCSR resource with custom namespace reference")
			loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
				struct {
					CustomNamespace string
					ConfigMapName   string
				}{
					CustomNamespace: customNamespace.Name,
					ConfigMapName:   configMapRefName,
				},
			), filepath.Join("testdata", "istio", "istiocsr_with_ca_certificate_custom_namespace.yaml"), ns.Name)
			defer loader.DeleteFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
				struct {
					CustomNamespace string
					ConfigMapName   string
				}{
					CustomNamespace: customNamespace.Name,
					ConfigMapName:   configMapRefName,
				},
			), filepath.Join("testdata", "istio", "istiocsr_with_ca_certificate_custom_namespace.yaml"), ns.Name)

			By("waiting for IstioCSR to be ready and deployment to be created")
			istioCSRStatus := waitForIstioCSRReady(ns)
			log.Printf("IstioCSR status: %+v", istioCSRStatus)

			By("Verifying that the CA certificate ConfigMap gets processed from custom namespace")
			// Wait for the controller to process and copy the ConfigMap from custom namespace to IstioCSR namespace
			var sourceCertData string
			Eventually(func() bool {
				// Get the source ConfigMap data from custom namespace for comparison
				sourceConfigMap, err := clientset.CoreV1().ConfigMaps(customNamespace.Name).Get(ctx, caCertConfigMap.Name, metav1.GetOptions{})
				if err != nil {
					return false
				}
				sourceCertData = sourceConfigMap.Data[configMapRefKey]

				// Check if the copied ConfigMap exists in IstioCSR namespace and has the same data
				copiedConfigMap, err := clientset.CoreV1().ConfigMaps(ns.Name).Get(ctx, testutils.IstiocsrCAConfigMapName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				copiedCertData := copiedConfigMap.Data[testutils.IstiocsrCAKeyName]
				return sourceCertData != "" && copiedCertData != "" && copiedCertData == sourceCertData
			}, "2m", "5s").Should(BeTrue(), "CA certificate should be copied from custom namespace to IstioCSR namespace with identical content")

			By("Verifying watch label was added to source ConfigMap in custom namespace")
			Eventually(func() bool {
				sourceConfigMap, err := clientset.CoreV1().ConfigMaps(customNamespace.Name).Get(ctx, caCertConfigMap.Name, metav1.GetOptions{})
				if err != nil {
					return false
				}
				_, hasWatchLabel := sourceConfigMap.Labels[testutils.IstiocsrResourceWatchLabelName]
				return hasWatchLabel
			}, "1m", "5s").Should(BeTrue(), "Source ConfigMap in custom namespace should have watch label")
		})
	})
})
