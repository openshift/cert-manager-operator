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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
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

		By("enable IstioCSR addon feature by patching operator deployment")
		err = patchOperatorDeploymentWithEnvVars(ctx, loader.KubeClient, map[string]string{
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
})
