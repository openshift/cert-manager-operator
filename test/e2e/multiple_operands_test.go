//go:build e2e
// +build e2e

// CM-786 IstioCSR Controller qualification (doc section "IstioCSR Controller specific tests"
// through "Record about cert-manager Operator"): multi-operand install plus sanity and
// qualification steps 5–14 that are automatable in e2e.
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
	"strings"
	"sync"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	trustapi "github.com/cert-manager/trust-manager/pkg/apis/trust/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/sync/errgroup"
	"sigs.k8s.io/yaml"

	testutils "github.com/openshift/cert-manager-operator/pkg/controller/istiocsr"
	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/test/library"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
)

const (
	istioCSRDeploymentName     = "cert-manager-istio-csr"
	istioCSRServiceName        = "cert-manager-istio-csr"
	istioCSRServiceAccountName = "cert-manager-istio-csr"

	bothOperandsFeatureGates = "IstioCSR=true,TrustManager=true"

	cm786QualificationLabelKey   = "cm-786-qualification"
	cm786QualificationLabelValue = "true"

	cm10TestDNSName = "multi-operand.cm10.test.example"
	cm12TestDNSName = "multi-operand.cm12.invalid.example"

	qualificationBundleName     = "cm786-ca-bundle-secret"
	qualificationBundleSourceCM = "cm786-bundle-source"
	qualificationBundleSourceKey = "ca-bundle.crt"
	qualificationBundleTargetKey = "ca-bundle.crt"
)

type operandDeployment struct {
	namespace string
	name      string
}

var coreOperandDeployments = []operandDeployment{
	{operandNamespace, certmanagerControllerDeployment},
	{operandNamespace, certmanagerWebhookDeployment},
	{operandNamespace, certmanagerCAinjectorDeployment},
	{operandNamespace, trustManagerDeploymentName},
}

var _ = Describe("Multiple operands CM-786 qualification", Ordered, Label("Platform:Generic", "Feature:MultipleOperands", "TechPreview", "Skipped:MicroShift"), func() {
	var (
		ctx = context.Background()

		clientset                        *kubernetes.Clientset
		originalUnsupportedAddonFeatures string
		originalOperatorLogLevel         string
		istioNS                          *corev1.Namespace
		qualificationBundlePEM           string
	)

	BeforeAll(func() {
		var err error
		clientset, err = kubernetes.NewForConfig(cfg)
		Expect(err).NotTo(HaveOccurred())

		if isMicroShiftCluster(ctx, clientset) {
			Skip("MicroShift: OLM not available")
		}

		trustManagerRemoveStaleOperandServiceAccount(ctx, clientset)

		originalUnsupportedAddonFeatures, err = getSubscriptionEnvVar(ctx, loader, "UNSUPPORTED_ADDON_FEATURES")
		Expect(err).NotTo(HaveOccurred())

		originalOperatorLogLevel, err = getSubscriptionEnvVar(ctx, loader, "OPERATOR_LOG_LEVEL")
		Expect(err).NotTo(HaveOccurred())

		By("CM-786 step 8: enable IstioCSR and TrustManager feature gates")
		err = patchSubscriptionWithEnvVars(ctx, loader, map[string]string{
			"UNSUPPORTED_ADDON_FEATURES": bothOperandsFeatureGates,
			"OPERATOR_LOG_LEVEL":         "4",
		})
		Expect(err).NotTo(HaveOccurred())

		err = waitForDeploymentEnvVarAndRollout(ctx, operatorNamespace, operatorDeploymentName,
			"UNSUPPORTED_ADDON_FEATURES", bothOperandsFeatureGates, highTimeout)
		Expect(err).NotTo(HaveOccurred())

		caTweak := func(cert *x509.Certificate) {
			cert.IsCA = true
			cert.KeyUsage |= x509.KeyUsageCertSign
		}
		qualificationBundlePEM = testutils.GenerateCertificate("cm786-bundle-ca", []string{"cert-manager-operator-e2e"}, caTweak)
	})

	AfterAll(func() {
		expectDeleteClean(trustManagerClient().Delete(ctx, "cluster", metav1.DeleteOptions{}), "TrustManager CR cluster")
		deleteTrustManagerDefaultCAPackageConfigMap(ctx)
		if istioNS != nil {
			expectDeleteClean(
				certmanageroperatorclient.OperatorV1alpha1().IstioCSRs(istioNS.Name).Delete(ctx, istioCSRResourceName, metav1.DeleteOptions{}),
				fmt.Sprintf("IstioCSR %s/%s", istioNS.Name, istioCSRResourceName),
			)
		}

		err := patchSubscriptionWithEnvVars(ctx, loader, map[string]string{
			"UNSUPPORTED_ADDON_FEATURES": originalUnsupportedAddonFeatures,
			"OPERATOR_LOG_LEVEL":         originalOperatorLogLevel,
		})
		Expect(err).NotTo(HaveOccurred())

		if originalUnsupportedAddonFeatures == "" {
			err = waitForDeploymentEnvVarRemovedAndRollout(ctx, operatorNamespace, operatorDeploymentName, "UNSUPPORTED_ADDON_FEATURES", lowTimeout)
		} else {
			err = waitForDeploymentEnvVarAndRollout(ctx, operatorNamespace, operatorDeploymentName,
				"UNSUPPORTED_ADDON_FEATURES", originalUnsupportedAddonFeatures, lowTimeout)
		}
		Expect(err).NotTo(HaveOccurred())
	})

	Context("with operands installed concurrently", Ordered, func() {
		BeforeAll(func() {
			var err error

			istioNS, err = loader.CreateTestingNS("multi-operand-istio", true)
			Expect(err).NotTo(HaveOccurred())

			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "cluster_issuer.yaml"), istioNS.Name)
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "certificate.yaml"), istioNS.Name)
			err = waitForCertificateReadiness(ctx, "my-selfsigned-ca", istioNS.Name)
			Expect(err).NotTo(HaveOccurred())
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "istio_ca_issuer.yaml"), istioNS.Name)

			ensureMultiOperandCRsAbsent(ctx, istioNS.Name)

			istioCSR, err := loadIstioCSRFromTemplate(istioNS.Name, IstioCSRConfig{})
			Expect(err).NotTo(HaveOccurred())

			By("CM-786 steps 5/10: concurrent TrustManager and IstioCSR CR creation")
			installOperandsConcurrently(ctx, qualificationTrustManagerCR().Build(), istioCSR)

			waitForOperandsReadyConcurrently(ctx, clientset, istioNS.Name)

			err = VerifyHealthyOperatorConditions(certmanageroperatorclient.OperatorV1alpha1())
			Expect(err).NotTo(HaveOccurred())
		})

		AfterAll(func() {
			if istioNS == nil {
				return
			}
			expectDeleteClean(bundleClient.Delete(ctx, &trustapi.Bundle{ObjectMeta: metav1.ObjectMeta{Name: qualificationBundleName}}),
				fmt.Sprintf("Bundle %s", qualificationBundleName))
			expectDeleteClean(k8sClientSet.CoreV1().ConfigMaps(trustManagerNamespace).Delete(ctx, qualificationBundleSourceCM, metav1.DeleteOptions{}),
				fmt.Sprintf("ConfigMap %s/%s", trustManagerNamespace, qualificationBundleSourceCM))
			loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "istio", "istio_ca_issuer.yaml"), istioNS.Name)
			loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "certificate.yaml"), istioNS.Name)
			loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "self_signed", "cluster_issuer.yaml"), istioNS.Name)
			loader.DeleteTestingNS(istioNS.Name, func() bool { return CurrentSpecReport().Failed() })
		})

		It("should have TrustManager and IstioCSR CRs ready after concurrent creation", func() {
			tm, err := trustManagerClient().Get(ctx, "cluster", metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			ready := meta.FindStatusCondition(tm.Status.Conditions, v1alpha1.Ready)
			Expect(ready).NotTo(BeNil())
			Expect(ready.Status).To(Equal(metav1.ConditionTrue))
			Expect(tm.Status.TrustManagerImage).NotTo(BeEmpty())

			istioCSRStatus, err := pollTillIstioCSRAvailable(ctx, loader, istioNS.Name, istioCSRResourceName)
			Expect(err).NotTo(HaveOccurred())
			Expect(istioCSRStatus.IstioCSRGRPCEndpoint).NotTo(BeEmpty())
			Expect(istioCSRStatus.IstioCSRImage).NotTo(BeEmpty())
		})

		It("CM-01: all operand deployments have desired replicas ready", func() {
			verifyAllOperandDeploymentsReady(ctx, clientset, istioNS.Name)
		})

		It("CM-02: operand pods have no CrashLoopBackOff", func() {
			Expect(allOperandPodsHealthy(ctx, clientset, istioNS.Name)).To(Succeed())
		})

		It("CM-03: cert-manager webhook service has endpoints", func() {
			verifyWebhookServiceHasEndpoints(ctx, clientset)
		})

		It("CM-10: issues a self-signed namespaced certificate", func() {
			runCM10SelfSignedCertificateTest(ctx)
		})

		It("CM-12: certificate with bogus issuer ref stays not Ready and does not break operands", func() {
			runCM12BogusIssuerTest(ctx, clientset, istioNS.Name)
		})

		It("CM-786 step 5: IstioCSR operand resources exist with operator-managed labels", func() {
			verifyIstioCSROperandResources(ctx, clientset, istioNS.Name)
		})

		It("CM-786 step 10: TrustManager qualification configuration is applied", func() {
			verifyTrustManagerQualificationConfig(ctx, clientset)
		})

		It("CM-786 step 10: Bundle propagates ConfigMap source to Secret in selected namespaces", func() {
			runCM786BundleSecretTargetTest(ctx, qualificationBundlePEM)
		})

		It("CM-786 step 11: managed ClusterRoles are recreated after deletion", func() {
			runCM786ManagedClusterRoleRecreationTest(ctx, clientset, istioNS.Name)
		})

		It("CM-786 step 12: updating all three operator CRs keeps operands healthy", func() {
			runCM786UpdateAllOperatorCRsTest(ctx, clientset, istioNS.Name)
		})

		It("CM-786 step 13: IstioCSR API updates reconcile successfully", func() {
			runCM786IstioCSRSpecUpdateTest(ctx, istioNS.Name)
		})

		It("CM-786 step 14: IstioCSR gRPC CreateCertificate returns a cert chain", func() {
			runCM786IstioCSRGRPCCertificateTest(ctx, clientset, istioNS)
		})

		It("CM-786 steps 6-7: Service Mesh uses istio-csr when Service Mesh is installed", func() {
			runCM786ServiceMeshIstioCSRIntegrationTest(ctx, clientset, istioNS.Name)
		})
	})
})

func qualificationTrustManagerCR() *trustManagerCRBuilder {
	return newTrustManagerCR().
		WithLabels(map[string]string{"env": "trustmanager-test"}).
		WithAnnotations(map[string]string{"trustmanager.operator.openshift.io/cluster": "trustmanager-test"}).
		WithDefaultCAPackage(v1alpha1.DefaultCAPackagePolicyEnabled).
		WithFilterExpiredCertificates(v1alpha1.FilterExpiredCertificatesPolicyEnabled).
		WithSecretTargets(v1alpha1.SecretTargetsPolicyCustom, []string{"ca-bundle-secret", qualificationBundleName}).
		WithTrustNamespace(trustManagerNamespace)
}

func installOperandsConcurrently(ctx context.Context, trustManager *v1alpha1.TrustManager, istioCSR *v1alpha1.IstioCSR) {
	start := make(chan struct{})
	var wg sync.WaitGroup
	var tmErr, istioErr error

	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, tmErr = trustManagerClient().Create(ctx, trustManager, metav1.CreateOptions{})
	}()
	go func() {
		defer wg.Done()
		<-start
		_, istioErr = certmanageroperatorclient.OperatorV1alpha1().IstioCSRs(istioCSR.Namespace).Create(ctx, istioCSR, metav1.CreateOptions{})
	}()

	close(start)
	wg.Wait()

	Expect(tmErr).NotTo(HaveOccurred())
	Expect(istioErr).NotTo(HaveOccurred())
}

func waitForOperandsReadyConcurrently(ctx context.Context, clientset *kubernetes.Clientset, istioNamespace string) {
	var eg errgroup.Group
	eg.Go(func() error {
		if err := pollTillDeploymentAvailableWithTimeout(ctx, clientset, trustManagerNamespace, trustManagerDeploymentName, multiOperandReadyTimeout); err != nil {
			return fmt.Errorf("trust-manager deployment: %w", err)
		}
		_, err := pollTillTrustManagerAvailableStrictWithTimeout(ctx, trustManagerClient(), "cluster", multiOperandReadyTimeout)
		if err != nil {
			return fmt.Errorf("trust-manager CR: %w", err)
		}
		return nil
	})
	eg.Go(func() error {
		if err := pollTillDeploymentAvailableWithTimeout(ctx, clientset, istioNamespace, istioCSRDeploymentName, multiOperandReadyTimeout); err != nil {
			return fmt.Errorf("istio-csr deployment: %w", err)
		}
		_, err := pollTillIstioCSRAvailableWithTimeout(ctx, loader, istioNamespace, istioCSRResourceName, multiOperandReadyTimeout)
		if err != nil {
			return fmt.Errorf("istio-csr CR: %w", err)
		}
		return nil
	})
	err := eg.Wait()
	Expect(err).To(Succeed(), formatMultiOperandReadinessStatus(ctx, clientset, istioNamespace))
}

func formatMultiOperandReadinessStatus(ctx context.Context, clientset *kubernetes.Clientset, istioNamespace string) string {
	var b strings.Builder

	tm, tmErr := trustManagerClient().Get(ctx, "cluster", metav1.GetOptions{})
	if tmErr != nil {
		fmt.Fprintf(&b, "TrustManager CR: get error: %v; ", tmErr)
	} else {
		fmt.Fprintf(&b, "TrustManager conditions=%v image=%q; ", tm.Status.Conditions, tm.Status.TrustManagerImage)
	}

	if dep, depErr := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{}); depErr != nil {
		fmt.Fprintf(&b, "trust-manager deployment: get error: %v; ", depErr)
	} else {
		fmt.Fprintf(&b, "trust-manager deployment ready=%d/%d available=%d; ",
			dep.Status.ReadyReplicas, ptr.Deref(dep.Spec.Replicas, 1), dep.Status.AvailableReplicas)
	}

	istioCSR, istioErr := certmanageroperatorclient.OperatorV1alpha1().IstioCSRs(istioNamespace).Get(ctx, istioCSRResourceName, metav1.GetOptions{})
	if istioErr != nil {
		fmt.Fprintf(&b, "IstioCSR CR: get error: %v; ", istioErr)
	} else {
		fmt.Fprintf(&b, "IstioCSR conditions=%v grpc=%q; ", istioCSR.Status.Conditions, istioCSR.Status.IstioCSRGRPCEndpoint)
	}

	if dep, depErr := clientset.AppsV1().Deployments(istioNamespace).Get(ctx, istioCSRDeploymentName, metav1.GetOptions{}); depErr != nil {
		fmt.Fprintf(&b, "istio-csr deployment: get error: %v", depErr)
	} else {
		fmt.Fprintf(&b, "istio-csr deployment ready=%d/%d available=%d",
			dep.Status.ReadyReplicas, ptr.Deref(dep.Spec.Replicas, 1), dep.Status.AvailableReplicas)
	}

	return strings.TrimSpace(b.String())
}

func verifyAllOperandDeploymentsReady(ctx context.Context, clientset *kubernetes.Clientset, istioNamespace string) {
	deployments := append([]operandDeployment{}, coreOperandDeployments...)
	deployments = append(deployments, operandDeployment{istioNamespace, istioCSRDeploymentName})

	for _, dep := range deployments {
		dep := dep
		Eventually(func(g Gomega) {
			g.Expect(assertDeploymentReplicasReady(ctx, clientset, dep.namespace, dep.name)).To(Succeed())
		}, lowTimeout, fastPollInterval).Should(Succeed(), "deployment %s/%s", dep.namespace, dep.name)
	}
}

func assertDeploymentReplicasReady(ctx context.Context, clientset *kubernetes.Clientset, namespace, name string) error {
	deployment, err := clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	desired := int32(1)
	if deployment.Spec.Replicas != nil {
		desired = *deployment.Spec.Replicas
	}

	for _, cond := range deployment.Status.Conditions {
		if cond.Type == appsv1.DeploymentAvailable && cond.Status == corev1.ConditionTrue {
			if deployment.Status.ReadyReplicas >= desired && deployment.Status.AvailableReplicas >= desired {
				return nil
			}
		}
	}

	return fmt.Errorf("deployment %s/%s not ready: desired=%d ready=%d available=%d",
		namespace, name, desired, deployment.Status.ReadyReplicas, deployment.Status.AvailableReplicas)
}

func verifyWebhookServiceHasEndpoints(ctx context.Context, clientset *kubernetes.Clientset) {
	Eventually(func(g Gomega) {
		ep, err := clientset.CoreV1().Endpoints(operandNamespace).Get(ctx, certmanagerWebhookDeployment, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(ep.Subsets).NotTo(BeEmpty())

		var endpointCount int
		for _, subset := range ep.Subsets {
			endpointCount += len(subset.Addresses)
		}
		g.Expect(endpointCount).To(BeNumerically(">", 0))

		pods, err := clientset.CoreV1().Pods(operandNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/name=webhook,app.kubernetes.io/component=webhook",
		})
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(pods.Items).NotTo(BeEmpty())
	}, lowTimeout, fastPollInterval).Should(Succeed())
}

func runCM10SelfSignedCertificateTest(ctx context.Context) {
	ns, err := loader.CreateTestingNS("multi-operand-cm10", false)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() {
		loader.DeleteTestingNS(ns.Name, func() bool { return CurrentSpecReport().Failed() })
	})

	const (
		clusterIssuerName = "cm10-multi-operand-selfsigned"
		certName          = "cm10-multi-operand-cert"
		secretName        = "cm10-multi-operand-tls"
	)

	_, err = certmanagerClient.CertmanagerV1().ClusterIssuers().Create(ctx, &certmanagerv1.ClusterIssuer{
		ObjectMeta: metav1.ObjectMeta{Name: clusterIssuerName},
		Spec: certmanagerv1.IssuerSpec{
			IssuerConfig: certmanagerv1.IssuerConfig{SelfSigned: &certmanagerv1.SelfSignedIssuer{}},
		},
	}, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() {
		_ = certmanagerClient.CertmanagerV1().ClusterIssuers().Delete(ctx, clusterIssuerName, metav1.DeleteOptions{})
	})

	_, err = certmanagerClient.CertmanagerV1().Certificates(ns.Name).Create(ctx, &certmanagerv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{Name: certName, Namespace: ns.Name},
		Spec: certmanagerv1.CertificateSpec{
			SecretName: secretName,
			Duration:   &metav1.Duration{Duration: time.Hour},
			DNSNames:   []string{cm10TestDNSName},
			IssuerRef: cmmetav1.ObjectReference{
				Name: clusterIssuerName, Kind: "ClusterIssuer", Group: "cert-manager.io",
			},
		},
	}, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())

	err = waitForCertificateReadiness(ctx, certName, ns.Name)
	Expect(err).NotTo(HaveOccurred())

	secret, err := k8sClientSet.CoreV1().Secrets(ns.Name).Get(ctx, secretName, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred())
	Expect(secret.Data).To(HaveKey("tls.crt"))
	Expect(secret.Data).To(HaveKey("tls.key"))
}

func runCM12BogusIssuerTest(ctx context.Context, clientset *kubernetes.Clientset, istioNamespace string) {
	ns, err := loader.CreateTestingNS("multi-operand-cm12", false)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() {
		loader.DeleteTestingNS(ns.Name, func() bool { return CurrentSpecReport().Failed() })
	})

	const (
		bogusIssuerName = "cm12-nonexistent-clusterissuer"
		certName        = "cm12-bogus-issuer-cert"
		secretName      = "cm12-tls-should-not-exist"
	)

	_, err = certmanagerClient.CertmanagerV1().Certificates(ns.Name).Create(ctx, &certmanagerv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{Name: certName, Namespace: ns.Name},
		Spec: certmanagerv1.CertificateSpec{
			SecretName: secretName,
			DNSNames:   []string{cm12TestDNSName},
			IssuerRef: cmmetav1.ObjectReference{
				Name: bogusIssuerName, Kind: "ClusterIssuer", Group: "cert-manager.io",
			},
		},
	}, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())

	Eventually(func(g Gomega) {
		cert, getErr := certmanagerClient.CertmanagerV1().Certificates(ns.Name).Get(ctx, certName, metav1.GetOptions{})
		g.Expect(getErr).NotTo(HaveOccurred())
		var readyCondition *certmanagerv1.CertificateCondition
		for i := range cert.Status.Conditions {
			if cert.Status.Conditions[i].Type == certmanagerv1.CertificateConditionReady {
				readyCondition = &cert.Status.Conditions[i]
				break
			}
		}
		g.Expect(readyCondition).NotTo(BeNil())
		g.Expect(readyCondition.Status).To(Equal(cmmetav1.ConditionFalse))
		g.Expect(readyCondition.Message).NotTo(BeEmpty())
	}, lowTimeout, fastPollInterval).Should(Succeed())

	_, err = k8sClientSet.CoreV1().Secrets(ns.Name).Get(ctx, secretName, metav1.GetOptions{})
	Expect(apierrors.IsNotFound(err)).To(BeTrue())

	Expect(allOperandPodsHealthy(ctx, clientset, istioNamespace)).To(Succeed())
	Expect(VerifyHealthyOperatorConditions(certmanageroperatorclient.OperatorV1alpha1())).To(Succeed())
	verifyAllOperandDeploymentsReady(ctx, clientset, istioNamespace)
}

func verifyIstioCSROperandResources(ctx context.Context, clientset *kubernetes.Clientset, istioNamespace string) {
	Eventually(func(g Gomega) {
		sa, err := clientset.CoreV1().ServiceAccounts(istioNamespace).Get(ctx, istioCSRServiceAccountName, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		verifyOperatorManagedLabels(sa.Labels, "cert-manager-istio-csr")

		svc, err := clientset.CoreV1().Services(istioNamespace).Get(ctx, istioCSRServiceName, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		verifyOperatorManagedLabels(svc.Labels, "cert-manager-istio-csr")

		dep, err := clientset.AppsV1().Deployments(istioNamespace).Get(ctx, istioCSRDeploymentName, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		verifyOperatorManagedLabels(dep.Labels, "cert-manager-istio-csr")
	}, lowTimeout, fastPollInterval).Should(Succeed())

	crs, err := clientset.RbacV1().ClusterRoles().List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/name=cert-manager-istio-csr",
	})
	Expect(err).NotTo(HaveOccurred())
	Expect(crs.Items).NotTo(BeEmpty())
	verifyOperatorManagedLabels(crs.Items[0].Labels, "cert-manager-istio-csr")
}

func verifyTrustManagerQualificationConfig(ctx context.Context, clientset *kubernetes.Clientset) {
	Eventually(func(g Gomega) {
		_, err := k8sClientSet.CoreV1().ConfigMaps(trustManagerNamespace).Get(ctx, defaultCAPackageConfigMapName, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())

		dep, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(dep.Spec.Template.Spec.Containers).NotTo(BeEmpty())
		args := dep.Spec.Template.Spec.Containers[0].Args
		g.Expect(args).To(ContainElement("--secret-targets-enabled=true"))
		g.Expect(args).To(ContainElement("--filter-expired-certificates=true"))
		hasDefaultPackageArg := false
		for _, arg := range args {
			if strings.Contains(arg, "--default-package-location=") {
				hasDefaultPackageArg = true
				break
			}
		}
		g.Expect(hasDefaultPackageArg).To(BeTrue())
	}, lowTimeout, fastPollInterval).Should(Succeed())

	tm, err := trustManagerClient().Get(ctx, "cluster", metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred())
	Expect(tm.Spec.TrustManagerConfig.DefaultCAPackage.Policy).To(Equal(v1alpha1.DefaultCAPackagePolicyEnabled))
	Expect(tm.Spec.TrustManagerConfig.SecretTargets.Policy).To(Equal(v1alpha1.SecretTargetsPolicyCustom))
	Expect(tm.Spec.TrustManagerConfig.FilterExpiredCertificates).To(Equal(v1alpha1.FilterExpiredCertificatesPolicyEnabled))
}

func runCM786BundleSecretTargetTest(ctx context.Context, sourcePEM string) {
	targetNS, err := k8sClientSet.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "cm786-bundle-target-",
			Labels:       map[string]string{"istio-injection": "enabled"},
		},
	}, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() {
		_ = k8sClientSet.CoreV1().Namespaces().Delete(ctx, targetNS.Name, metav1.DeleteOptions{})
	})

	_, err = k8sClientSet.CoreV1().ConfigMaps(trustManagerNamespace).Create(ctx, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: qualificationBundleSourceCM},
		Data:       map[string]string{qualificationBundleSourceKey: sourcePEM},
	}, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())

	bundle := newBundle(qualificationBundleName).
		WithConfigMapSource(qualificationBundleSourceCM, qualificationBundleSourceKey).
		WithSecretTarget(qualificationBundleTargetKey).
		WithNamespaceSelector(map[string]string{"istio-injection": "enabled"}).
		Build()

	Expect(bundleClient.Create(ctx, bundle)).To(Succeed())

	err = waitForBundleCondition(ctx, bundleClient, qualificationBundleName, trustapi.BundleConditionSynced, metav1.ConditionTrue, highTimeout)
	Expect(err).NotTo(HaveOccurred())

	err = waitForSecretTarget(ctx, bundleClient, qualificationBundleName, targetNS.Name, qualificationBundleTargetKey, sourcePEM, highTimeout)
	Expect(err).NotTo(HaveOccurred())
}

func runCM786ManagedClusterRoleRecreationTest(ctx context.Context, clientset *kubernetes.Clientset, istioNamespace string) {
	verifyResourceRecreation(ctx, func() error {
		return clientset.RbacV1().ClusterRoles().Delete(ctx, trustManagerClusterRoleName, metav1.DeleteOptions{})
	}, func() error {
		_, err := clientset.RbacV1().ClusterRoles().Get(ctx, trustManagerClusterRoleName, metav1.GetOptions{})
		return err
	})

	istioCRs, err := clientset.RbacV1().ClusterRoles().List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/name=cert-manager-istio-csr",
	})
	Expect(err).NotTo(HaveOccurred())
	Expect(istioCRs.Items).NotTo(BeEmpty())
	deletedIstioCRName := istioCRs.Items[0].Name

	// IstioCSR ClusterRoles are created with GenerateName (cert-manager-istio-csr-<suffix>).
	// After deletion the controller recreates a new ClusterRole with a different suffix.
	Expect(clientset.RbacV1().ClusterRoles().Delete(ctx, deletedIstioCRName, metav1.DeleteOptions{})).To(Succeed())
	Eventually(func(g Gomega) {
		crs, listErr := clientset.RbacV1().ClusterRoles().List(ctx, metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/name=cert-manager-istio-csr",
		})
		g.Expect(listErr).NotTo(HaveOccurred())
		g.Expect(crs.Items).NotTo(BeEmpty(), "istio-csr ClusterRole was not recreated by controller")
	}, lowTimeout, fastPollInterval).Should(Succeed())

	if _, err := clientset.RbacV1().ClusterRoles().Get(ctx, "cert-manager-controller-challenges", metav1.GetOptions{}); err == nil {
		verifyResourceRecreation(ctx, func() error {
			return clientset.RbacV1().ClusterRoles().Delete(ctx, "cert-manager-controller-challenges", metav1.DeleteOptions{})
		}, func() error {
			_, getErr := clientset.RbacV1().ClusterRoles().Get(ctx, "cert-manager-controller-challenges", metav1.GetOptions{})
			return getErr
		})
	}

	Expect(VerifyHealthyOperatorConditions(certmanageroperatorclient.OperatorV1alpha1())).To(Succeed())

	tm, err := trustManagerClient().Get(ctx, "cluster", metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred())
	ready := meta.FindStatusCondition(tm.Status.Conditions, v1alpha1.Ready)
	Expect(ready).NotTo(BeNil())
	Expect(ready.Status).To(Equal(metav1.ConditionTrue))

	_, err = pollTillIstioCSRAvailable(ctx, loader, istioNamespace, istioCSRResourceName)
	Expect(err).NotTo(HaveOccurred())
}

func runCM786UpdateAllOperatorCRsTest(ctx context.Context, clientset *kubernetes.Clientset, istioNamespace string) {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cm, getErr := certmanageroperatorclient.OperatorV1alpha1().CertManagers().Get(ctx, "cluster", metav1.GetOptions{})
		if getErr != nil {
			return getErr
		}
		cm.Labels = mergeLabels(cm.Labels, map[string]string{cm786QualificationLabelKey: cm786QualificationLabelValue})
		_, updateErr := certmanageroperatorclient.OperatorV1alpha1().CertManagers().Update(ctx, cm, metav1.UpdateOptions{})
		return updateErr
	})
	Expect(err).NotTo(HaveOccurred())

	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		tm, getErr := trustManagerClient().Get(ctx, "cluster", metav1.GetOptions{})
		if getErr != nil {
			return getErr
		}
		tm.Labels = mergeLabels(tm.Labels, map[string]string{cm786QualificationLabelKey: cm786QualificationLabelValue})
		_, updateErr := trustManagerClient().Update(ctx, tm, metav1.UpdateOptions{})
		return updateErr
	})
	Expect(err).NotTo(HaveOccurred())

	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		istioCSR, getErr := certmanageroperatorclient.OperatorV1alpha1().IstioCSRs(istioNamespace).Get(ctx, istioCSRResourceName, metav1.GetOptions{})
		if getErr != nil {
			return getErr
		}
		istioCSR.Labels = mergeLabels(istioCSR.Labels, map[string]string{cm786QualificationLabelKey: cm786QualificationLabelValue})
		_, updateErr := certmanageroperatorclient.OperatorV1alpha1().IstioCSRs(istioNamespace).Update(ctx, istioCSR, metav1.UpdateOptions{})
		return updateErr
	})
	Expect(err).NotTo(HaveOccurred())

	Expect(VerifyHealthyOperatorConditions(certmanageroperatorclient.OperatorV1alpha1())).To(Succeed())
	verifyAllOperandDeploymentsReady(ctx, clientset, istioNamespace)
	waitForOperandsReadyConcurrently(ctx, clientset, istioNamespace)
}

func runCM786IstioCSRSpecUpdateTest(ctx context.Context, istioNamespace string) {
	for _, level := range []int32{3, 2, 1} {
		level := level
		err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			istioCSR, getErr := certmanageroperatorclient.OperatorV1alpha1().IstioCSRs(istioNamespace).Get(ctx, istioCSRResourceName, metav1.GetOptions{})
			if getErr != nil {
				return getErr
			}
			istioCSR.Spec.IstioCSRConfig.LogLevel = level
			istioCSR.Labels = mergeLabels(istioCSR.Labels, map[string]string{"test": fmt.Sprintf("cm786-log-%d", level)})
			_, updateErr := certmanageroperatorclient.OperatorV1alpha1().IstioCSRs(istioNamespace).Update(ctx, istioCSR, metav1.UpdateOptions{})
			return updateErr
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = pollTillIstioCSRAvailable(ctx, loader, istioNamespace, istioCSRResourceName)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			dep, getErr := k8sClientSet.AppsV1().Deployments(istioNamespace).Get(ctx, istioCSRDeploymentName, metav1.GetOptions{})
			g.Expect(getErr).NotTo(HaveOccurred())
			g.Expect(dep.Spec.Template.Spec.Containers).NotTo(BeEmpty())
			g.Expect(dep.Spec.Template.Spec.Containers[0].Args).To(ContainElement(fmt.Sprintf("--log-level=%d", level)))
		}, lowTimeout, fastPollInterval).Should(Succeed())
	}
}

func runCM786IstioCSRGRPCCertificateTest(ctx context.Context, clientset *kubernetes.Clientset, ns *corev1.Namespace) {
	const grpcAppName = "grpcurl-istio-csr"

	istioCSRStatus, err := pollTillIstioCSRAvailable(ctx, loader, ns.Name, istioCSRResourceName)
	Expect(err).NotTo(HaveOccurred())

	err = pollTillServiceAccountAvailable(ctx, clientset, ns.Name, istioCSRServiceAccountName)
	Expect(err).NotTo(HaveOccurred())

	protoBytes, err := testassets.ReadFile("testdata/ca.proto")
	Expect(err).NotTo(HaveOccurred())
	_, err = clientset.CoreV1().ConfigMaps(ns.Name).Create(ctx, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "proto-cm", Namespace: ns.Name},
		Data:       map[string]string{"ca.proto": string(protoBytes)},
	}, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() {
		_ = clientset.CoreV1().ConfigMaps(ns.Name).Delete(ctx, "proto-cm", metav1.DeleteOptions{})
	})

	csr := generateCM786CSR(ns.Name)
	loader.CreateFromFile(AssetFunc(testassets.ReadFile).WithTemplateValues(
		IstioCSRGRPCurlJobConfig{
			CertificateSigningRequest: csr,
			IstioCSRStatus:            istioCSRStatus,
		},
	), filepath.Join("testdata", "istio", "grpcurl_job.yaml"), ns.Name)
	DeferCleanup(func() {
		policy := metav1.DeletePropagationBackground
		_ = clientset.BatchV1().Jobs(ns.Name).Delete(ctx, grpcAppName, metav1.DeleteOptions{PropagationPolicy: &policy})
	})

	err = pollTillJobCompleted(ctx, clientset, ns.Name, grpcAppName)
	Expect(err).NotTo(HaveOccurred())

	pods, err := clientset.CoreV1().Pods(ns.Name).List(ctx, metav1.ListOptions{LabelSelector: fmt.Sprintf("app=%s", grpcAppName)})
	Expect(err).NotTo(HaveOccurred())

	var succeededPodName string
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodSucceeded {
			succeededPodName = pod.Name
		}
	}
	Expect(succeededPodName).NotTo(BeEmpty())

	req := clientset.CoreV1().Pods(ns.Name).GetLogs(succeededPodName, &corev1.PodLogOptions{})
	logStream, err := req.Stream(ctx)
	Expect(err).NotTo(HaveOccurred())
	defer logStream.Close()

	logData, err := io.ReadAll(logStream)
	Expect(err).NotTo(HaveOccurred())

	var entry LogEntry
	Expect(json.Unmarshal(logData, &entry)).To(Succeed())
	Expect(entry.CertChain).NotTo(BeEmpty())

	for _, certPEM := range entry.CertChain {
		Expect(library.ValidateCertificate(certPEM, "my-selfsigned-ca")).To(Succeed())
	}
}

func runCM786ServiceMeshIstioCSRIntegrationTest(ctx context.Context, clientset *kubernetes.Clientset, istioCSRNamespace string) {
	istioGVR := schema.GroupVersionResource{Group: "sailoperator.io", Version: "v1", Resource: "istios"}
	istioCR, err := loader.DynamicClient.Resource(istioGVR).Get(ctx, "default", metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			Skip("OpenShift Service Mesh (sailoperator.io/v1 Istio/default) is not installed; CM-786 steps 6-7 require manual mesh qualification")
		}
		Expect(err).NotTo(HaveOccurred())
	}

	caAddress, found, err := unstructuredNestedString(istioCR.Object, "spec", "values", "global", "caAddress")
	if err != nil || !found {
		Skip("Service Mesh Istio CR does not expose spec.values.global.caAddress; skipping CM-786 step 7 mesh CA check")
	}
	Expect(caAddress).To(ContainSubstring("cert-manager-istio-csr"))

	svc, err := clientset.CoreV1().Services(istioCSRNamespace).Get(ctx, istioCSRServiceName, metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred())
	Expect(svc.Spec.Ports).NotTo(BeEmpty())

	Eventually(func(g Gomega) {
		ep, epErr := clientset.CoreV1().Endpoints(istioCSRNamespace).Get(ctx, istioCSRServiceName, metav1.GetOptions{})
		g.Expect(epErr).NotTo(HaveOccurred())
		g.Expect(ep.Subsets).NotTo(BeEmpty())
	}, lowTimeout, fastPollInterval).Should(Succeed())
}

func generateCM786CSR(istioNamespace string) string {
	csrTemplate := &x509.CertificateRequest{
		Subject: pkix.Name{
			Organization:       []string{"CM-786"},
			OrganizationalUnit: []string{"cert-manager-operator-e2e"},
			Country:            []string{"US"},
		},
		URIs: []*url.URL{
			{Scheme: "spiffe", Host: "cluster.local", Path: fmt.Sprintf("/ns/%s/sa/cert-manager-istio-csr", istioNamespace)},
		},
		SignatureAlgorithm: x509.SHA256WithRSA,
	}
	csr, err := library.GenerateCSR(csrTemplate)
	Expect(err).NotTo(HaveOccurred())
	return csr
}

func verifyResourceRecreation(ctx context.Context, deleteFunc func() error, getFunc func() error) {
	Expect(deleteFunc()).To(Succeed())
	Eventually(func() error {
		return getFunc()
	}, lowTimeout, fastPollInterval).Should(Succeed(), "resource was not recreated by controller")
}

func verifyOperatorManagedLabels(labels map[string]string, name string) {
	Expect(labels).To(HaveKeyWithValue("app.kubernetes.io/managed-by", "cert-manager-operator"))
	Expect(labels).To(HaveKeyWithValue("app.kubernetes.io/name", name))
	Expect(labels).To(HaveKeyWithValue("app.kubernetes.io/part-of", "cert-manager-operator"))
}

func allOperandPodsHealthy(ctx context.Context, clientset *kubernetes.Clientset, istioNamespace string) error {
	namespaces := []string{operandNamespace, istioNamespace}
	for _, ns := range namespaces {
		pods, err := clientset.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		for _, pod := range pods.Items {
			for _, cs := range pod.Status.ContainerStatuses {
				if cs.State.Waiting != nil && cs.State.Waiting.Reason == "CrashLoopBackOff" {
					return fmt.Errorf("pod %s/%s container %s in CrashLoopBackOff", ns, pod.Name, cs.Name)
				}
			}
		}
	}
	return nil
}

func mergeLabels(existing map[string]string, extra map[string]string) map[string]string {
	out := make(map[string]string, len(existing)+len(extra))
	for k, v := range existing {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func unstructuredNestedString(obj map[string]interface{}, fields ...string) (string, bool, error) {
	val, found, err := unstructuredNestedField(obj, fields...)
	if !found || err != nil {
		return "", found, err
	}
	s, ok := val.(string)
	return s, ok, nil
}

func unstructuredNestedField(obj map[string]interface{}, fields ...string) (interface{}, bool, error) {
	var current interface{} = obj
	for _, field := range fields {
		m, ok := current.(map[string]interface{})
		if !ok {
			return nil, false, nil
		}
		val, ok := m[field]
		if !ok {
			return nil, false, nil
		}
		current = val
	}
	return current, true, nil
}

func loadIstioCSRFromTemplate(namespace string, cfg IstioCSRConfig) (*v1alpha1.IstioCSR, error) {
	raw, err := testassets.ReadFile(filepath.Join("testdata", "istio", "istio_csr_template.yaml"))
	if err != nil {
		return nil, err
	}
	rendered, err := replaceWithTemplate(string(raw), cfg)
	if err != nil {
		return nil, err
	}
	var istioCSR v1alpha1.IstioCSR
	if err := yaml.Unmarshal(rendered, &istioCSR); err != nil {
		return nil, err
	}
	istioCSR.Namespace = namespace
	// Align istio data-plane namespace with the test namespace. The template defaults to
	// istio-system (matching istio_csr_test.go), but multi-operand tests use a dedicated NS.
	istioCSR.Spec.IstioCSRConfig.Istio.Namespace = namespace
	return &istioCSR, nil
}

// expectDeleteClean fails the spec when delete returns an unexpected error (NotFound is OK).
func expectDeleteClean(err error, resource string) {
	if err == nil || apierrors.IsNotFound(err) {
		return
	}
	Expect(err).NotTo(HaveOccurred(), "cleanup delete "+resource)
}

// isMicroShiftCluster reports whether the cluster is MicroShift (no OLM/Subscription).
func isMicroShiftCluster(ctx context.Context, clientset *kubernetes.Clientset) bool {
	for _, ns := range []string{"kube-public", "kube-system"} {
		_, err := clientset.CoreV1().ConfigMaps(ns).Get(ctx, "microshift-version", metav1.GetOptions{})
		if err == nil {
			return true
		}
		if !apierrors.IsNotFound(err) {
			continue
		}
	}
	return false
}

func ensureMultiOperandCRsAbsent(ctx context.Context, istioNamespace string) {
	_, err := trustManagerClient().Get(ctx, "cluster", metav1.GetOptions{})
	Expect(apierrors.IsNotFound(err)).To(BeTrue())
	_, err = certmanageroperatorclient.OperatorV1alpha1().IstioCSRs(istioNamespace).Get(ctx, istioCSRResourceName, metav1.GetOptions{})
	Expect(apierrors.IsNotFound(err)).To(BeTrue())
}
