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

	testutils "github.com/openshift/cert-manager-operator/pkg/controller/istiocsr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/test/library"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// istioCSRProtoURL links to proto for istio-csr API spec
const istioCSRProtoURL = "https://raw.githubusercontent.com/istio/api/v1.24.1/security/v1alpha1/ca.proto"

type LogEntry struct {
	CertChain []string `json:"certChain"`
}

type IstioCSRConfig struct {
	ClusterID                       string
	IstioDataPlaneNamespaceSelector string
}

var _ = Describe("Istio-CSR", Ordered, Label("Feature:IstioCSR"), func() {
	ctx := context.TODO()
	var clientset *kubernetes.Clientset
	var httpProxy, httpsProxy, noProxy string

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

		By("increase operator log verbosity")
		err = patchSubscriptionWithEnvVars(ctx, loader, map[string]string{
			"OPERATOR_LOG_LEVEL": "5",
		})
		Expect(err).NotTo(HaveOccurred())

		By("getting cluster proxy configuration")
		httpProxy, httpsProxy, noProxy, err = getClusterProxyConfig(ctx, configClient)
		Expect(err).Should(BeNil(), "failed to get cluster proxy config")
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

		err = waitForCertificateReadiness(ctx, "my-selfsigned-ca", ns.Name)
		Expect(err).NotTo(HaveOccurred())

		By("creating istio-ca issuer")
		loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "istio_ca_issuer.yaml"), ns.Name)
		DeferCleanup(func() {
			loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "istio_ca_issuer.yaml"), ns.Name)
		})

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
		})

		It("should return cert-chain as response", func() {
			serviceAccountName := "cert-manager-istio-csr"
			grpcAppName := "grpcurl-istio-csr"

			By("creating istiocsr.operator.openshift.io resource")
			loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
				IstioCSRConfig{},
			), filepath.Join("testdata", "istio", "istio_csr_template.yaml"), ns.Name)
			defer loader.DeleteFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
				IstioCSRConfig{},
			), filepath.Join("testdata", "istio", "istio_csr_template.yaml"), ns.Name)

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
					HTTPProxy:                 httpProxy,
					HTTPSProxy:                httpsProxy,
					NoProxy:                   noProxy,
				},
			), filepath.Join("testdata", "istio", "grpcurl_job.yaml"), ns.Name)
			DeferCleanup(func() {
				policy := metav1.DeletePropagationBackground
				clientset.BatchV1().Jobs(ns.Name).Delete(ctx, grpcAppName, metav1.DeleteOptions{PropagationPolicy: &policy})
			})

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
			), filepath.Join("testdata", "istio", "istio_csr_template.yaml"), ns.Name)
			defer loader.DeleteFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
				IstioCSRConfig{
					ClusterID: clusterName,
				},
			), filepath.Join("testdata", "istio", "istio_csr_template.yaml"), ns.Name)

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
					HTTPProxy:                 httpProxy,
					HTTPSProxy:                httpsProxy,
					NoProxy:                   noProxy,
				},
			), filepath.Join("testdata", "istio", "grpcurl_job_with_cluster_id.yaml"), ns.Name)
			DeferCleanup(func() {
				policy := metav1.DeletePropagationBackground
				clientset.BatchV1().Jobs(ns.Name).Delete(ctx, grpcAppName, metav1.DeleteOptions{PropagationPolicy: &policy})
			})

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
			), filepath.Join("testdata", "istio", "istio_csr_template.yaml"), ns.Name)
			defer loader.DeleteFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
				IstioCSRConfig{
					ClusterID: clusterName,
				},
			), filepath.Join("testdata", "istio", "istio_csr_template.yaml"), ns.Name)

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
					HTTPProxy:                 httpProxy,
					HTTPSProxy:                httpsProxy,
					NoProxy:                   noProxy,
				},
			), filepath.Join("testdata", "istio", "grpcurl_job_with_cluster_id.yaml"), ns.Name)
			DeferCleanup(func() {
				policy := metav1.DeletePropagationBackground
				clientset.BatchV1().Jobs(ns.Name).Delete(ctx, grpcAppName, metav1.DeleteOptions{PropagationPolicy: &policy})
			})

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

	Context("istioDataPlaneNamespaceSelector functionality", func() {
		var testNamespaces []*corev1.Namespace

		BeforeEach(func() {
			By("creating test namespaces with different labels")
			namespacesToCreate := []*corev1.Namespace{
				{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "ca-injection-enabled-",
						Labels: map[string]string{
							"cert-manager.io/test-ca-injection": "enabled", // matches the IstioCSR resource
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "ca-injection-disabled-",
						Labels: map[string]string{
							"cert-manager.io/test-ca-injection": "disabled", // doesn't match the IstioCSR resource
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "ca-injection-unlabeled-",
						// no labels
					},
				},
			}

			for _, ns := range namespacesToCreate {
				createdNs, err := clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
				Expect(err).Should(BeNil())
				testNamespaces = append(testNamespaces, createdNs)
			}

			DeferCleanup(func() {
				for _, testNs := range testNamespaces {
					err := clientset.CoreV1().Namespaces().Delete(ctx, testNs.Name, metav1.DeleteOptions{})
					Expect(err).Should(BeNil())
				}
				testNamespaces = []*corev1.Namespace{}
			})
		})

		It("should only create istio-ca-root-cert ConfigMap in namespaces matching the selector", func() {
			By("creating istiocsr.operator.openshift.io resource with istioDataPlaneNamespaceSelector")
			loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
				IstioCSRConfig{
					IstioDataPlaneNamespaceSelector: "cert-manager.io/test-ca-injection=enabled",
				},
			), filepath.Join("testdata", "istio", "istio_csr_template.yaml"), ns.Name)
			defer loader.DeleteFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
				IstioCSRConfig{
					IstioDataPlaneNamespaceSelector: "cert-manager.io/test-ca-injection=enabled",
				},
			), filepath.Join("testdata", "istio", "istio_csr_template.yaml"), ns.Name)

			By("waiting for IstioCSR to be ready and deployment to be created")
			istioCSRStatus := waitForIstioCSRReady(ns)
			log.Printf("IstioCSR status: %+v", istioCSRStatus)

			By("verifying ConfigMap creation based on namespace selector")
			var matchingNs *corev1.Namespace
			for _, testNs := range testNamespaces {
				labelValue, hasLabel := testNs.Labels["cert-manager.io/test-ca-injection"]

				if hasLabel && labelValue == "enabled" {
					// This namespace should have the ConfigMap
					matchingNs = testNs
					err := pollTillConfigMapAvailable(ctx, clientset, testNs.Name, "istio-ca-root-cert")
					Expect(err).Should(BeNil(), fmt.Sprintf("ConfigMap should exist in namespace %s (enabled)", testNs.Name))
				} else {
					// This namespace should NOT have the ConfigMap
					var reason string
					if !hasLabel {
						reason = "unlabeled"
					} else {
						reason = fmt.Sprintf("with %s label", labelValue)
					}

					err := pollTillConfigMapRemains(ctx, clientset, testNs.Name, "istio-ca-root-cert", lowTimeout)
					Expect(err).Should(BeNil(), fmt.Sprintf("ConfigMap should NOT exist in namespace %s (%s)", testNs.Name, reason))
				}
			}
			Expect(matchingNs).NotTo(BeNil(), "Should have found at least one namespace with enabled label")

			By("verifying the ConfigMap contains the correct CA certificate data")
			cm, err := clientset.CoreV1().ConfigMaps(matchingNs.Name).Get(ctx, "istio-ca-root-cert", metav1.GetOptions{})
			Expect(err).Should(BeNil())
			Expect(cm.Data).Should(HaveKey("root-cert.pem"))
			Expect(cm.Data["root-cert.pem"]).ShouldNot(BeEmpty())

			By("verifying ConfigMap exists in exactly 1 namespace (the one with matching label)")
			configMapCount := 0
			for _, testNs := range testNamespaces {
				_, err := clientset.CoreV1().ConfigMaps(testNs.Name).Get(ctx, "istio-ca-root-cert", metav1.GetOptions{})
				if err == nil {
					configMapCount++
				}
			}
			Expect(configMapCount).Should(Equal(1), "ConfigMap should exist in exactly 1 namespace (the one with enabled label)")
		})

		It("should create istio-ca-root-cert ConfigMap in all namespaces when istioDataPlaneNamespaceSelector is not set", func() {
			By("creating istiocsr.operator.openshift.io resource without istioDataPlaneNamespaceSelector")
			loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
				IstioCSRConfig{},
			), filepath.Join("testdata", "istio", "istio_csr_template.yaml"), ns.Name)
			defer loader.DeleteFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
				IstioCSRConfig{},
			), filepath.Join("testdata", "istio", "istio_csr_template.yaml"), ns.Name)

			By("waiting for IstioCSR to be ready and deployment to be created")
			istioCSRStatus := waitForIstioCSRReady(ns)
			log.Printf("IstioCSR status: %+v", istioCSRStatus)

			By("waiting for istio-ca-root-cert ConfigMap to be created in all test namespaces")
			for _, testNs := range testNamespaces {
				err := pollTillConfigMapAvailable(ctx, clientset, testNs.Name, "istio-ca-root-cert")
				Expect(err).Should(BeNil(), fmt.Sprintf("ConfigMap should be created in namespace %s", testNs.Name))

				By(fmt.Sprintf("verifying ConfigMap content in namespace %s", testNs.Name))
				cm, err := clientset.CoreV1().ConfigMaps(testNs.Name).Get(ctx, "istio-ca-root-cert", metav1.GetOptions{})
				Expect(err).Should(BeNil())
				Expect(cm.Data).Should(HaveKey("root-cert.pem"))
				Expect(cm.Data["root-cert.pem"]).ShouldNot(BeEmpty())
			}

			By("verifying ConfigMap exists in all 3 test namespaces when no selector is used")
			configMapCount := 0
			for _, testNs := range testNamespaces {
				_, err := clientset.CoreV1().ConfigMaps(testNs.Name).Get(ctx, "istio-ca-root-cert", metav1.GetOptions{})
				if err == nil {
					configMapCount++
				}
			}
			Expect(configMapCount).Should(Equal(3), "ConfigMap should exist in all 3 test namespaces when no selector is configured")
		})
	})

	Context("with CA Certificate ConfigMap", func() {

		const (
			configMapRefName = "test-ca-certificate"
			configMapRefKey  = "ca-cert.pem"
		)

		type istioCSRTemplateData struct {
			CustomNamespace string
			ConfigMapName   string
			ConfigMapKey    string
		}

		// Helper functions for CA ConfigMap verification
		verifyConfigMapHasWatchLabel := func(namespace, configMapName string) {
			By(fmt.Sprintf("Verifying watch label on ConfigMap %s in namespace %s", configMapName, namespace))
			Eventually(func() bool {
				configMap, err := clientset.CoreV1().ConfigMaps(namespace).Get(ctx, configMapName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				_, hasWatchLabel := configMap.Labels[testutils.IstiocsrResourceWatchLabelName]
				return hasWatchLabel
			}, "1m", "5s").Should(BeTrue(), fmt.Sprintf("ConfigMap %s should have watch label", configMapName))
		}

		verifyConfigMapCopied := func(sourceNamespace, sourceConfigMapName, sourceKey, destNamespace string) {
			By(fmt.Sprintf("Verifying ConfigMap %s from namespace %s is copied to namespace %s", sourceConfigMapName, sourceNamespace, destNamespace))
			Eventually(func() bool {
				// Get the source ConfigMap data
				sourceConfigMap, err := clientset.CoreV1().ConfigMaps(sourceNamespace).Get(ctx, sourceConfigMapName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				sourceCertData := sourceConfigMap.Data[sourceKey]

				// Check if the copied ConfigMap exists and has the same data
				copiedConfigMap, err := clientset.CoreV1().ConfigMaps(destNamespace).Get(ctx, testutils.IstiocsrCAConfigMapName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				copiedCertData := copiedConfigMap.Data[testutils.IstiocsrCAKeyName]

				return sourceCertData != "" && copiedCertData != "" && copiedCertData == sourceCertData
			}, "2m", "5s").Should(BeTrue(), "CA certificate should be copied with identical content")
		}

		verifyConfigMapMountedInPod := func(namespace string) {
			By("Verifying ConfigMap is mounted correctly in the pod")
			Eventually(func() error {
				pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
					LabelSelector: "app.kubernetes.io/name=cert-manager-istio-csr",
				})
				if err != nil {
					return err
				}
				if len(pods.Items) == 0 {
					return fmt.Errorf("no istio-csr pods found")
				}

				// Get the first running pod
				var runningPod *corev1.Pod
				for i := range pods.Items {
					if pods.Items[i].Status.Phase == corev1.PodRunning {
						runningPod = &pods.Items[i]
						break
					}
				}
				if runningPod == nil {
					return fmt.Errorf("no running istio-csr pod found")
				}

				// Verify volume is configured
				volumeFound := false
				for _, vol := range runningPod.Spec.Volumes {
					if vol.Name == "root-ca" {
						if vol.ConfigMap == nil {
							return fmt.Errorf("volume root-ca is not a ConfigMap volume")
						}
						if vol.ConfigMap.Name != testutils.IstiocsrCAConfigMapName {
							return fmt.Errorf("volume root-ca references wrong ConfigMap: got %s, want %s",
								vol.ConfigMap.Name, testutils.IstiocsrCAConfigMapName)
						}
						volumeFound = true
						break
					}
				}
				if !volumeFound {
					return fmt.Errorf("volume root-ca not found in pod spec")
				}

				// Verify volume mount in container
				for _, container := range runningPod.Spec.Containers {
					if container.Name == "cert-manager-istio-csr" {
						volumeMountFound := false
						for _, vm := range container.VolumeMounts {
							if vm.Name == "root-ca" {
								if vm.MountPath != "/var/run/configmaps/istio-csr" {
									return fmt.Errorf("volume mount root-ca has wrong path: got %s, want /var/run/configmaps/istio-csr",
										vm.MountPath)
								}
								if !vm.ReadOnly {
									return fmt.Errorf("volume mount root-ca should be read-only")
								}
								volumeMountFound = true
								break
							}
						}
						if !volumeMountFound {
							return fmt.Errorf("volume mount root-ca not found in container cert-manager-istio-csr")
						}
						return nil
					}
				}
				return fmt.Errorf("container cert-manager-istio-csr not found in pod")
			}, "2m", "10s").Should(Succeed(), "ConfigMap should be correctly mounted in the pod")
		}

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

			By("Creating IstioCSR resource")
			templateData := istioCSRTemplateData{
				CustomNamespace: "", // Empty string for same namespace
				ConfigMapName:   configMapRefName,
				ConfigMapKey:    configMapRefKey,
			}
			loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(templateData), filepath.Join("testdata", "istio", "istiocsr_with_ca_certificate_template.yaml"), ns.Name)
			DeferCleanup(func() {
				loader.DeleteFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(templateData), filepath.Join("testdata", "istio", "istiocsr_with_ca_certificate_template.yaml"), ns.Name)
			})

			By("waiting for IstioCSR to be ready and deployment to be created")
			istioCSRStatus := waitForIstioCSRReady(ns)
			log.Printf("IstioCSR status: %+v", istioCSRStatus)

			// Verify that the source ConfigMap data is copied to the operator-managed ConfigMap
			verifyConfigMapCopied(caCertConfigMap.Namespace, caCertConfigMap.Name, configMapRefKey, ns.Name)
			// Verify that the source ConfigMap has a watch label to trigger reconciliation on changes
			verifyConfigMapHasWatchLabel(caCertConfigMap.Namespace, caCertConfigMap.Name)
			// Verify that the certificate is correctly mounted in the istio-csr pod
			verifyConfigMapMountedInPod(ns.Name)
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
			templateData := istioCSRTemplateData{
				CustomNamespace: "", // Empty string for same namespace
				ConfigMapName:   configMapRefName,
				ConfigMapKey:    configMapRefKey,
			}
			loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(templateData), filepath.Join("testdata", "istio", "istiocsr_with_ca_certificate_template.yaml"), ns.Name)
			DeferCleanup(func() {
				loader.DeleteFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(templateData), filepath.Join("testdata", "istio", "istiocsr_with_ca_certificate_template.yaml"), ns.Name)
			})

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
			DeferCleanup(func() {
				loader.DeleteTestingNS(customNamespace.Name, func() bool { return false })
			})

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
			templateData := istioCSRTemplateData{
				CustomNamespace: customNamespace.Name,
				ConfigMapName:   configMapRefName,
				ConfigMapKey:    configMapRefKey,
			}
			loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(templateData), filepath.Join("testdata", "istio", "istiocsr_with_ca_certificate_template.yaml"), ns.Name)
			DeferCleanup(func() {
				loader.DeleteFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(templateData), filepath.Join("testdata", "istio", "istiocsr_with_ca_certificate_template.yaml"), ns.Name)
			})

			By("waiting for IstioCSR to be ready and deployment to be created")
			istioCSRStatus := waitForIstioCSRReady(ns)
			log.Printf("IstioCSR status: %+v", istioCSRStatus)

			// Verify that the source ConfigMap data is copied from custom namespace to the operator-managed ConfigMap
			verifyConfigMapCopied(customNamespace.Name, caCertConfigMap.Name, configMapRefKey, ns.Name)
			// Verify that the source ConfigMap in custom namespace has a watch label to trigger reconciliation on changes
			verifyConfigMapHasWatchLabel(customNamespace.Name, caCertConfigMap.Name)
			// Verify that the certificate from custom namespace is correctly mounted in the istio-csr pod
			verifyConfigMapMountedInPod(ns.Name)
		})

		It("should reconcile copied ConfigMap when manually modified", func() {
			By("Creating initial CA certificate ConfigMap")
			initialCert := testutils.GenerateCertificate("Initial CA E2E", []string{"cert-manager-operator"}, func(cert *x509.Certificate) {
				cert.IsCA = true
				cert.KeyUsage |= x509.KeyUsageCertSign
			})

			caCertConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapRefName,
					Namespace: ns.Name,
				},
				Data: map[string]string{
					configMapRefKey: initialCert,
				},
			}
			_, err := clientset.CoreV1().ConfigMaps(caCertConfigMap.Namespace).Create(ctx, caCertConfigMap, metav1.CreateOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			By("Creating IstioCSR resource")
			templateData := istioCSRTemplateData{
				CustomNamespace: "",
				ConfigMapName:   configMapRefName,
				ConfigMapKey:    configMapRefKey,
			}
			loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(templateData), filepath.Join("testdata", "istio", "istiocsr_with_ca_certificate_template.yaml"), ns.Name)
			DeferCleanup(func() {
				loader.DeleteFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(templateData), filepath.Join("testdata", "istio", "istiocsr_with_ca_certificate_template.yaml"), ns.Name)
			})

			By("Waiting for IstioCSR to be ready")
			_ = waitForIstioCSRReady(ns)

			By("Verifying initial ConfigMap is copied")
			Eventually(func() bool {
				copiedConfigMap, err := clientset.CoreV1().ConfigMaps(ns.Name).Get(ctx, testutils.IstiocsrCAConfigMapName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				return copiedConfigMap.Data[testutils.IstiocsrCAKeyName] == initialCert
			}, "2m", "5s").Should(BeTrue(), "Initial CA certificate should be copied")

			By("Manually modifying the copied ConfigMap")
			tamperedCert := "-----BEGIN CERTIFICATE-----\nTAMPERED DATA\n-----END CERTIFICATE-----"
			copiedConfigMap, err := clientset.CoreV1().ConfigMaps(ns.Name).Get(ctx, testutils.IstiocsrCAConfigMapName, metav1.GetOptions{})
			Expect(err).ShouldNot(HaveOccurred())
			copiedConfigMap.Data[testutils.IstiocsrCAKeyName] = tamperedCert
			_, err = clientset.CoreV1().ConfigMaps(ns.Name).Update(ctx, copiedConfigMap, metav1.UpdateOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			By("Verifying operator automatically reconciles the copied ConfigMap back to the desired state")
			Eventually(func() bool {
				reconciledConfigMap, err := clientset.CoreV1().ConfigMaps(ns.Name).Get(ctx, testutils.IstiocsrCAConfigMapName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				// Should be reconciled back to match the source ConfigMap, not the tampered data
				return reconciledConfigMap.Data[testutils.IstiocsrCAKeyName] == initialCert
			}, "2m", "5s").Should(BeTrue(), "Copied ConfigMap should be reconciled back to match source ConfigMap")

			By("Verifying the ConfigMap volume mount is still correctly configured")
			verifyConfigMapMountedInPod(ns.Name)
		})

		It("should update mounted CA certificate when source ConfigMap is modified", func() {
			By("Creating initial CA certificate ConfigMap")
			initialCert := testutils.GenerateCertificate("Initial CA E2E", []string{"cert-manager-operator"}, func(cert *x509.Certificate) {
				cert.IsCA = true
				cert.KeyUsage |= x509.KeyUsageCertSign
			})

			caCertConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapRefName,
					Namespace: ns.Name,
				},
				Data: map[string]string{
					configMapRefKey: initialCert,
				},
			}
			_, err := clientset.CoreV1().ConfigMaps(caCertConfigMap.Namespace).Create(ctx, caCertConfigMap, metav1.CreateOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			By("Creating IstioCSR resource")
			templateData := istioCSRTemplateData{
				CustomNamespace: "",
				ConfigMapName:   configMapRefName,
				ConfigMapKey:    configMapRefKey,
			}
			loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(templateData), filepath.Join("testdata", "istio", "istiocsr_with_ca_certificate_template.yaml"), ns.Name)
			DeferCleanup(func() {
				loader.DeleteFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(templateData), filepath.Join("testdata", "istio", "istiocsr_with_ca_certificate_template.yaml"), ns.Name)
			})

			By("Waiting for IstioCSR to be ready")
			_ = waitForIstioCSRReady(ns)

			By("Verifying initial ConfigMap is copied and mounted")
			Eventually(func() bool {
				copiedConfigMap, err := clientset.CoreV1().ConfigMaps(ns.Name).Get(ctx, testutils.IstiocsrCAConfigMapName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				return copiedConfigMap.Data[testutils.IstiocsrCAKeyName] == initialCert
			}, "2m", "5s").Should(BeTrue(), "Initial CA certificate should be copied")

			// Verify that the ConfigMap volume mount is correctly configured
			verifyConfigMapMountedInPod(ns.Name)

			By("Updating the source ConfigMap with new CA certificate")
			updatedCert := testutils.GenerateCertificate("Updated CA E2E", []string{"cert-manager-operator-updated"}, func(cert *x509.Certificate) {
				cert.IsCA = true
				cert.KeyUsage |= x509.KeyUsageCertSign
			})

			sourceConfigMap, err := clientset.CoreV1().ConfigMaps(ns.Name).Get(ctx, configMapRefName, metav1.GetOptions{})
			Expect(err).ShouldNot(HaveOccurred())
			sourceConfigMap.Data[configMapRefKey] = updatedCert
			_, err = clientset.CoreV1().ConfigMaps(ns.Name).Update(ctx, sourceConfigMap, metav1.UpdateOptions{})
			Expect(err).ShouldNot(HaveOccurred())

			By("Verifying copied ConfigMap is updated with new certificate")
			Eventually(func() bool {
				copiedConfigMap, err := clientset.CoreV1().ConfigMaps(ns.Name).Get(ctx, testutils.IstiocsrCAConfigMapName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				return copiedConfigMap.Data[testutils.IstiocsrCAKeyName] == updatedCert
			}, "2m", "5s").Should(BeTrue(), "Updated CA certificate should be copied")

			// Verify that the ConfigMap volume mount is still correctly configured
			verifyConfigMapMountedInPod(ns.Name)
		})
	})
})
