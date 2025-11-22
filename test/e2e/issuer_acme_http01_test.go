//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	k8sresource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/openshift/cert-manager-operator/test/library"

	acmev1 "github.com/cert-manager/cert-manager/pkg/apis/acme/v1"
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanagermetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ACME Issuer HTTP01 solver", Ordered, func() {
	var ctx context.Context
	var cancel context.CancelFunc
	var ns *corev1.Namespace
	var appsDomain string
	var baseDomain string

	BeforeAll(func() {
		By("getting cluster base domain")
		var err error
		baseDomain, err = library.GetClusterBaseDomain(context.Background(), configClient)
		Expect(err).NotTo(HaveOccurred(), "failed to get cluster base domain")
		Expect(baseDomain).NotTo(BeEmpty(), "base domain should not be empty")
		appsDomain = "apps." + baseDomain

		By("adding required args to cert-manager controller")
		err = addOverrideArgs(certmanageroperatorclient, certmanagerControllerDeployment, []string{
			// for http-01 solver ingress
			"--acme-http01-solver-resource-limits-cpu=150m",
			"--acme-http01-solver-resource-limits-memory=200Mi",
			"--acme-http01-solver-resource-request-cpu=100m",
			"--acme-http01-solver-resource-request-memory=100Mi",
		})
		Expect(err).NotTo(HaveOccurred(), "failed to add override args to cert-manager controller")

		DeferCleanup(func(ctx context.Context) {
			By("Resetting cert-manager state")
			err := resetCertManagerState(ctx, certmanageroperatorclient, loader)
			if err != nil {
				fmt.Fprintf(GinkgoWriter, "failed to reset cert-manager state during cleanup: %v\n", err)
			}
		})
	})

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 15*time.Minute)
		DeferCleanup(cancel)

		By("waiting for operator status to become available")
		err := VerifyHealthyOperatorConditions(certmanageroperatorclient.OperatorV1alpha1())
		Expect(err).NotTo(HaveOccurred(), "Operator is expected to be available")

		By("creating a test namespace")
		namespace, err := loader.CreateTestingNS("e2e-acme-http01", false)
		Expect(err).NotTo(HaveOccurred(), "failed to create test namespace")
		ns = namespace

		DeferCleanup(func() {
			loader.DeleteTestingNS(ns.Name, func() bool { return CurrentSpecReport().Failed() })
		})
	})

	Context("http-01 challenge using ingress", func() {
		var ingressHost string
		var secretName string

		BeforeEach(func() {
			clusterIssuerName := "letsencrypt-http01"
			ingressClassName := "openshift-default"
			secretName = "ingress-http01-secret"

			By("creating a cluster issuer")
			clusterIssuer := &certmanagerv1.ClusterIssuer{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterIssuerName,
				},
				Spec: certmanagerv1.IssuerSpec{
					IssuerConfig: certmanagerv1.IssuerConfig{
						ACME: &acmev1.ACMEIssuer{
							Server: letsEncryptStagingServerURL,
							PrivateKey: certmanagermetav1.SecretKeySelector{
								LocalObjectReference: certmanagermetav1.LocalObjectReference{
									Name: "letsencrypt-http01-issuer",
								},
							},
							Solvers: []acmev1.ACMEChallengeSolver{
								{
									HTTP01: &acmev1.ACMEChallengeSolverHTTP01{
										Ingress: &acmev1.ACMEChallengeSolverHTTP01Ingress{
											IngressClassName: &ingressClassName,
										},
									},
								},
							},
						},
					},
				},
			}
			_, err := certmanagerClient.CertmanagerV1().ClusterIssuers().Create(ctx, clusterIssuer, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			By("creating an hello-openshift deployment")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "acme", "deployment.yaml"), ns.Name)

			By("creating a service for the deployment hello-openshift")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "acme", "service.yaml"), ns.Name)

			By("creating an Ingress object")
			ingressHost = fmt.Sprintf("ahi-%s.%s", randomStr(3), appsDomain) // acronym for "ACME http-01 Ingress"
			pathType := networkingv1.PathTypePrefix
			ingress := &networkingv1.Ingress{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "networking.k8s.io/v1",
					Kind:       "Ingress",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ingress-http01",
					Namespace: ns.Name,
					Annotations: map[string]string{
						"cert-manager.io/cluster-issuer": clusterIssuerName,
					},
				},
				Spec: networkingv1.IngressSpec{
					IngressClassName: &ingressClassName,
					Rules: []networkingv1.IngressRule{
						{
							Host: ingressHost,
							IngressRuleValue: networkingv1.IngressRuleValue{
								HTTP: &networkingv1.HTTPIngressRuleValue{
									Paths: []networkingv1.HTTPIngressPath{
										{
											Path:     "/",
											PathType: &pathType,
											Backend:  networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "hello-openshift", Port: networkingv1.ServiceBackendPort{Number: 8080}}},
										},
									},
								},
							},
						},
					},
					TLS: []networkingv1.IngressTLS{{
						Hosts:      []string{ingressHost},
						SecretName: secretName,
					}},
				},
			}
			ingress, err = loader.KubeClient.NetworkingV1().Ingresses(ns.Name).Create(ctx, ingress, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			DeferCleanup(func() {
				certmanagerClient.CertmanagerV1().ClusterIssuers().Delete(ctx, clusterIssuerName, metav1.DeleteOptions{})
			})
		})

		It("should obtain a valid LetsEncrypt certificate", func() {
			By("checking TLS certificate contents")
			err := wait.PollUntilContextTimeout(context.TODO(), slowPollInterval, highTimeout, true, func(context.Context) (bool, error) {
				secret, err := loader.KubeClient.CoreV1().Secrets(ns.Name).Get(ctx, secretName, metav1.GetOptions{})
				tlsConfig, isvalid := library.GetTLSConfig(secret)
				if !isvalid {
					return false, nil
				}
				isHostCorrect, err := library.VerifyHostname(ingressHost, tlsConfig.Clone())
				if err != nil {
					return false, nil
				}
				isNotExpired, err := library.VerifyExpiry(ingressHost+":443", tlsConfig.Clone())
				if err != nil {
					return false, nil
				}

				return isHostCorrect && isNotExpired, nil
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should create HTTP01 solver pods with custom resource limits and requests", func() {
			By("monitoring for ACME HTTP01 solver pods with expected resource configuration")
			// These values match what's configured in BeforeAll
			expectedResources := corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("150m"),
					corev1.ResourceMemory: k8sresource.MustParse("200Mi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("100m"),
					corev1.ResourceMemory: k8sresource.MustParse("100Mi"),
				},
			}

			err := wait.PollUntilContextTimeout(ctx, fastPollInterval, lowTimeout, true, func(ctx context.Context) (bool, error) {
				pods, err := k8sClientSet.CoreV1().Pods("").List(ctx, metav1.ListOptions{
					LabelSelector: acmeSolverPodLabel,
				})
				if err != nil {
					return false, nil // Retry on transient errors
				}

				if len(pods.Items) == 0 {
					return false, nil // Keep waiting for pods to appear
				}

				// Check if any pod matches the expected resource configuration
				for _, pod := range pods.Items {
					if err := VerifyContainerResources(pod, acmeSolverContainerName, expectedResources); err == nil {
						return true, nil
					}
				}

				return false, nil // No matching pods yet, keep waiting
			})
			Expect(err).NotTo(HaveOccurred(), "should find ACME HTTP01 solver pods with expected resource configuration")

			By("waiting for certificate to get ready")
			err = waitForCertificateReadiness(ctx, secretName, ns.Name)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
