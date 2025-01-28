//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"k8s.io/utils/ptr"
	"path/filepath"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/openshift/cert-manager-operator/test/library"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type LogEntry struct {
	CertChain []string `json:"certChain"`
}

var _ = Describe("Istio-CSR", Ordered, Label("TechPreview", "Feature:IstioCSR"), func() {
	ctx := context.Background()
	var clientset *kubernetes.Clientset
	var dynamicClient *dynamic.DynamicClient

	BeforeAll(func() {
		var err error
		clientset, err = kubernetes.NewForConfig(cfg)
		Expect(err).Should(BeNil())

		dynamicClient, err = dynamic.NewForConfig(cfg)
		Expect(err).Should(BeNil())
	})

	var ns *corev1.Namespace

	BeforeEach(func() {
		By("waiting for operator status to become available")
		err := verifyOperatorStatusCondition(certmanageroperatorclient, []string{
			certManagerControllerDeploymentControllerName,
			certManagerWebhookDeploymentControllerName,
			certManagerCAInjectorDeploymentControllerName,
		}, validOperatorStatusConditions)
		Expect(err).NotTo(HaveOccurred(), "Operator is expected to be available")

		By("creating a test namespace")
		namespace, err := loader.CreateTestingNS("istio-system", true)
		Expect(err).NotTo(HaveOccurred())
		ns = namespace

		DeferCleanup(func() {
			loader.DeleteTestingNS(ns.Name, func() bool { return CurrentSpecReport().Failed() })
		})
	})

	Context("grpc call istio.v1.auth.IstioCertificateService/CreateCertificate to istio-csr agent", func() {
		It("should return cert-chain as response", func() {
			serviceAccountName := "cert-manager-istio-csr"
			grpcAppName := "grpcurl"

			By("creating cluster issuer")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "cluster_issuer.yaml"), ns.Name)
			defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "cluster_issuer.yaml"), ns.Name)

			By("issuing TLS certificate")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "certificate.yaml"), ns.Name)
			defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "certificate.yaml"), ns.Name)

			By("fetching proto file from api")
			istioURL := "https://raw.githubusercontent.com/istio/api/v1.24.1/security/v1alpha1/ca.proto"
			protoContent, err := library.FetchFileFromURL(istioURL)
			Expect(err).Should(BeNil())

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
			defer clientset.CoreV1().ConfigMaps(ns.Name).Delete(ctx, configMap.Name, metav1.DeleteOptions{})

			By("creating istio-ca issuer")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "istio-ca-issuer.yaml"), ns.Name)
			defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "istio-ca-issuer.yaml"), ns.Name)

			By("creating istiocsr.operator.openshift.io resource")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "istio-csr.yaml"), ns.Name)
			defer loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "istio-csr.yaml"), ns.Name)

			By("poll till cert-manager-istio-csr is available")
			err = pollTillDeploymentAvailable(ctx, clientset, ns.Name, "cert-manager-istio-csr")
			Expect(err).Should(BeNil())

			istioCSRGRPCEndpoint, err := pollTillIstioCSRAvailable(ctx, dynamicClient, ns.Name, "default")
			Expect(err).Should(BeNil())

			By("poll till the service account is available")
			err = pollTillServiceAccountAvailable(ctx, clientset, ns.Name, serviceAccountName)
			Expect(err).Should(BeNil())

			By("generate csr request")
			csr, err := library.GenerateCSR()
			Expect(err).Should(BeNil())

			By("creating an grpcurl job")
			job := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name: "grpcurl-job",
				},
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Name: grpcAppName,
							Labels: map[string]string{
								"app": grpcAppName,
							},
						},
						Spec: corev1.PodSpec{
							ServiceAccountName:           serviceAccountName,
							AutomountServiceAccountToken: ptr.To(false),
							RestartPolicy:                corev1.RestartPolicyOnFailure,
							Containers: []corev1.Container{
								{
									Name:  grpcAppName,
									Image: "registry.redhat.io/rhel9/go-toolset",
									Command: []string{
										"/bin/sh",
										"-c",
									},
									Env: []corev1.EnvVar{
										{
											Name:  "GOCACHE",
											Value: "/tmp/go-cache",
										},
									},
									Args: []string{
										"GOCACHE=/tmp/go-cache && " +
											"export GOPATH=/tmp/go && " +
											"go install github.com/fullstorydev/grpcurl/cmd/grpcurl@v1.9.2 >/dev/null 2>&1 && " +
											"TOKEN=$(cat /var/run/secrets/istio-ca/token) && " +
											"/tmp/go/bin/grpcurl " +
											"-import-path /proto " +
											"-proto /proto/ca.proto " +
											"-H \"Authorization: Bearer $TOKEN\" " +
											fmt.Sprintf("-d '{\"csr\": \"%s\", \"validity_duration\": 3600}' ", csr) +
											"-cacert /etc/root-secret/ca.crt " +
											"-key /etc/root-secret/tls.key " +
											"-cert /etc/root-secret/tls.crt " +
											fmt.Sprintf("%s istio.v1.auth.IstioCertificateService/CreateCertificate", istioCSRGRPCEndpoint),
									},
									VolumeMounts: []corev1.VolumeMount{
										{Name: "root-secret", MountPath: "/etc/root-secret"},
										{Name: "proto", MountPath: "/proto"},
										{Name: "service-token", MountPath: "/var/run/secrets/istio-ca"},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: "service-token",
									VolumeSource: corev1.VolumeSource{
										Projected: &corev1.ProjectedVolumeSource{
											DefaultMode: ptr.To(int32(420)),
											Sources: []corev1.VolumeProjection{
												{
													ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
														Audience:          "istio-ca",
														ExpirationSeconds: ptr.To(int64(3600)),
														Path:              "token",
													},
												},
											},
										},
									},
								},
								{
									Name: "root-secret",
									VolumeSource: corev1.VolumeSource{
										Secret: &corev1.SecretVolumeSource{
											SecretName: "istiod-tls",
										},
									},
								},
								{
									Name: "proto",
									VolumeSource: corev1.VolumeSource{
										ConfigMap: &corev1.ConfigMapVolumeSource{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "proto-cm",
											},
										},
									},
								},
							},
						},
					},
				},
			}
			_, err = clientset.BatchV1().Jobs(ns.Name).Create(context.TODO(), job, metav1.CreateOptions{})
			Expect(err).Should(BeNil())
			defer clientset.BatchV1().Jobs(ns.Name).Delete(ctx, job.Name, metav1.DeleteOptions{})

			By("waiting for the job to be completed")
			err = pollTillJobCompleted(ctx, clientset, ns.Name, "grpcurl-job")
			Expect(err).Should(BeNil())

			By("fetching logs of the grpcurl job")
			pods, err := clientset.CoreV1().Pods(ns.Name).List(context.TODO(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("app=%s", grpcAppName),
			})
			Expect(err).Should(BeNil())

			By("reading logs of the grpcurl job")
			for _, pod := range pods.Items {
				req := clientset.CoreV1().Pods(ns.Name).GetLogs(pod.Name, &corev1.PodLogOptions{})
				logs, err := req.Stream(context.TODO())
				Expect(err).Should(BeNil())

				defer logs.Close()

				logData, err := io.ReadAll(logs)
				Expect(err).Should(BeNil())

				var entry LogEntry
				err = json.Unmarshal(logData, &entry)
				Expect(err).Should(BeNil())

				By("validating each certificate")
				for _, certPEM := range entry.CertChain {
					err = library.ValidateCertificate(certPEM, "my-selfsigned-ca")
					Expect(err).Should(BeNil())
				}

			}
		})
	})
})
