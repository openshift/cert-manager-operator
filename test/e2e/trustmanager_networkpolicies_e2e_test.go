//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"crypto/x509"
	"fmt"

	trustapi "github.com/cert-manager/trust-manager/pkg/apis/trust/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	testutils "github.com/openshift/cert-manager-operator/pkg/controller/istiocsr"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	tmNPDenyAllName              = "trust-manager-deny-all"
	tmNPAllowEgressAPIServerName = "trust-manager-allow-egress-to-api-server"
	tmNPAllowIngressWebhookName  = "trust-manager-allow-ingress-to-webhook"
	tmNPAllowIngressMetricsName  = "trust-manager-allow-ingress-to-metrics"
	tmNPOwnedByLabelKey          = "trustmanager.operator.openshift.io/owned-by"
	tmNPOwnedByLabelValue        = "trust-manager"
	uwmNamespace                 = "openshift-user-workload-monitoring"
)

var trustManagerNetworkPolicyNames = []string{
	tmNPDenyAllName,
	tmNPAllowEgressAPIServerName,
	tmNPAllowIngressWebhookName,
	tmNPAllowIngressMetricsName,
}

var _ = Describe("TrustManager NetworkPolicies", Ordered, Label("Platform:Generic", "Feature:TrustManager", "TechPreview"), func() {
	var (
		ctx = context.Background()

		clientset                        *kubernetes.Clientset
		originalUnsupportedAddonFeatures string
		originalOperatorLogLevel         string
		hasOLMSubscription               bool
	)

	BeforeAll(func() {
		cs, err := kubernetes.NewForConfig(cfg)
		Expect(err).NotTo(HaveOccurred())
		clientset = cs

		_, subErr := getCertManagerOperatorSubscription(ctx, loader)
		hasOLMSubscription = subErr == nil
		if !hasOLMSubscription {
			By("no OLM subscription in cert-manager-operator; using local operator with TrustManager already enabled")
			return
		}
		trustManagerBeforeAll(ctx, &clientset, &originalUnsupportedAddonFeatures, &originalOperatorLogLevel)()
	})

	AfterAll(func() {
		if !hasOLMSubscription {
			return
		}
		trustManagerAfterAll(ctx, &originalUnsupportedAddonFeatures, &originalOperatorLogLevel)()
	})

	BeforeEach(trustManagerBeforeEach())
	AfterEach(trustManagerAfterEach(ctx))

	expectTrustManagerNetworkPolicies := func() {
		for _, name := range trustManagerNetworkPolicyNames {
			np, err := clientset.NetworkingV1().NetworkPolicies(trustManagerNamespace).Get(ctx, name, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "NetworkPolicy %s", name)
			Expect(np.Spec.PodSelector.MatchLabels).To(HaveKeyWithValue("app", trustManagerCommonName))
			Expect(np.Labels[tmNPOwnedByLabelKey]).To(Equal(tmNPOwnedByLabelValue))
		}

		egressNP, err := clientset.NetworkingV1().NetworkPolicies(trustManagerNamespace).Get(ctx, tmNPAllowEgressAPIServerName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(egressNP.Spec.Egress).NotTo(BeEmpty())
		Expect(egressNP.Spec.Egress[0].Ports[0].Port.IntVal).To(Equal(int32(6443)))

		webhookNP, err := clientset.NetworkingV1().NetworkPolicies(trustManagerNamespace).Get(ctx, tmNPAllowIngressWebhookName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(webhookNP.Spec.Ingress).NotTo(BeEmpty())
		Expect(webhookNP.Spec.Ingress[0].Ports[0].Port.IntVal).To(Equal(int32(6443)))

		metricsNP, err := clientset.NetworkingV1().NetworkPolicies(trustManagerNamespace).Get(ctx, tmNPAllowIngressMetricsName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(metricsNP.Spec.Ingress).NotTo(BeEmpty())
		Expect(metricsNP.Spec.Ingress[0].Ports[0].Port.IntVal).To(Equal(int32(9402)))
		Expect(metricsNP.Spec.Ingress[0].From).NotTo(BeEmpty())
		Expect(metricsNP.Spec.Ingress[0].From[0].NamespaceSelector.MatchLabels).To(HaveKeyWithValue("kubernetes.io/metadata.name", uwmNamespace))

		denyNP, err := clientset.NetworkingV1().NetworkPolicies(trustManagerNamespace).Get(ctx, tmNPDenyAllName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(denyNP.Spec.PolicyTypes).To(ContainElements(networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress))
	}

	It("CJ-1: creates four reserved policies regardless of CertManager defaultNetworkPolicy", func() {
		cm, err := certmanageroperatorclient.OperatorV1alpha1().CertManagers().Get(ctx, "cluster", metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		By(fmt.Sprintf("CertManager.spec.defaultNetworkPolicy=%q (trust-manager NPs must still apply)", cm.Spec.DefaultNetworkPolicy))

		createTrustManager(ctx, newTrustManagerCR())
		expectTrustManagerDeploymentPresent(ctx)
		expectTrustManagerNetworkPolicies()
	})

	It("CJ-2: admits Bundle create with network policies applied", func() {
		createTrustManager(ctx, newTrustManagerCR())
		expectTrustManagerNetworkPolicies()

		targetNS := createNamespaceWithCleanup(ctx, "tm-np-bundle-", nil)
		caTweak := func(cert *x509.Certificate) {
			cert.IsCA = true
			cert.KeyUsage |= x509.KeyUsageCertSign
		}
		pem := testutils.GenerateCertificate("tm-np-admit-ca", []string{"cert-manager-operator-e2e"}, caTweak)
		bundleName := fmt.Sprintf("tm-np-admit-%d", GinkgoRandomSeed())
		bundle := newBundle(bundleName).
			WithInLineSource(pem).
			WithConfigMapTarget("ca.crt").
			WithNamespaceSelector(map[string]string{"kubernetes.io/metadata.name": targetNS.Name}).
			Build()
		createBundleWithCleanup(ctx, bundle)

		var got trustapi.Bundle
		Eventually(func() error {
			return bundleClient.Get(ctx, crclient.ObjectKey{Name: bundleName}, &got)
		}, lowTimeout, fastPollInterval).Should(Succeed())
	})

	It("CJ-3: restores drifted deny-all policy after TrustManager spec change and does not select cert-manager pods", func() {
		createTrustManager(ctx, newTrustManagerCR())
		expectTrustManagerNetworkPolicies()

		denyNP, err := clientset.NetworkingV1().NetworkPolicies(trustManagerNamespace).Get(ctx, tmNPDenyAllName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		patched := denyNP.DeepCopy()
		patched.Spec.PolicyTypes = []networkingv1.PolicyType{networkingv1.PolicyTypeIngress}
		_, err = clientset.NetworkingV1().NetworkPolicies(trustManagerNamespace).Update(ctx, patched, metav1.UpdateOptions{})
		Expect(err).NotTo(HaveOccurred())

		tm, err := trustManagerClient().Get(ctx, "cluster", metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		if tm.Spec.ControllerConfig.Labels == nil {
			tm.Spec.ControllerConfig.Labels = map[string]string{}
		}
		tm.Spec.ControllerConfig.Labels["openspec-e2e-drift-trigger"] = fmt.Sprintf("%d", GinkgoRandomSeed())
		_, err = trustManagerClient().Update(ctx, tm, metav1.UpdateOptions{})
		Expect(err).NotTo(HaveOccurred())

		Eventually(func(g Gomega) {
			np, getErr := clientset.NetworkingV1().NetworkPolicies(trustManagerNamespace).Get(ctx, tmNPDenyAllName, metav1.GetOptions{})
			g.Expect(getErr).NotTo(HaveOccurred())
			g.Expect(np.Spec.PolicyTypes).To(ContainElements(networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress))
		}, highTimeout, fastPollInterval).Should(Succeed())

		deploy, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, "cert-manager", metav1.GetOptions{})
		if err == nil {
			appLabel := deploy.Spec.Template.Labels["app"]
			Expect(appLabel).NotTo(Equal(trustManagerCommonName))
		}
	})

	It("CJ-4: allows metrics scrape from openshift-user-workload-monitoring when monitoring stack is present", func() {
		createTrustManager(ctx, newTrustManagerCR())
		expectTrustManagerNetworkPolicies()

		_, err := clientset.CoreV1().Namespaces().Get(ctx, uwmNamespace, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			Skip("namespace openshift-user-workload-monitoring not present")
		}
		Expect(err).NotTo(HaveOccurred())

		Skip("ServiceMonitor + Prometheus verification is cluster-specific; implement using testdata/observe/servicemonitor.yaml when UWM is available")
	})
})
