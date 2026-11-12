//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"crypto/x509"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	testutils "github.com/openshift/cert-manager-operator/pkg/controller/istiocsr"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
)

const (
	tmNPDenyAllName              = "trust-manager-deny-all"
	tmNPAllowEgressAPIServerName = "trust-manager-allow-egress-to-api-server"
	tmNPAllowIngressWebhookName  = "trust-manager-allow-ingress-to-webhook"
	tmNPAllowIngressMetricsName  = "trust-manager-allow-ingress-to-metrics"
	tmNPOwnedByLabelKey          = "trustmanager.operator.openshift.io/owned-by"
	tmNPOwnedByLabelValue        = "trust-manager"
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

		installed, err := certManagerOperatorSubscriptionInstalled(ctx, loader)
		Expect(err).NotTo(HaveOccurred())
		hasOLMSubscription = installed
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
			Expect(np.Spec.PodSelector.MatchLabels).To(Equal(map[string]string{"app": trustManagerCommonName}))
			Expect(np.Labels[tmNPOwnedByLabelKey]).To(Equal(tmNPOwnedByLabelValue))
		}

		deploy, err := clientset.AppsV1().Deployments(trustManagerNamespace).Get(ctx, "cert-manager", metav1.GetOptions{})
		if err == nil {
			selector := labels.SelectorFromSet(labels.Set{"app": trustManagerCommonName})
			Expect(selector.Matches(labels.Set(deploy.Spec.Template.Labels))).To(BeFalse(),
				"trust-manager policies must not select cert-manager pods")
		}

		egressNP, err := clientset.NetworkingV1().NetworkPolicies(trustManagerNamespace).Get(ctx, tmNPAllowEgressAPIServerName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		expectTrustManagerAPIServerEgressPolicy(egressNP)

		webhookNP, err := clientset.NetworkingV1().NetworkPolicies(trustManagerNamespace).Get(ctx, tmNPAllowIngressWebhookName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		expectTrustManagerWebhookIngressPolicy(webhookNP)

		metricsNP, err := clientset.NetworkingV1().NetworkPolicies(trustManagerNamespace).Get(ctx, tmNPAllowIngressMetricsName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		expectTrustManagerMetricsIngressPolicy(metricsNP)

		denyNP, err := clientset.NetworkingV1().NetworkPolicies(trustManagerNamespace).Get(ctx, tmNPDenyAllName, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		expectTrustManagerDenyAllPolicy(denyNP)
	}

	// createTrustManagerReadyWithPolicies waits until the operand Deployment is Available.
	// Existence of the Deployment object is not enough: a ProgressDeadlineExceeded
	// pod leaves the webhook Service with no endpoints.
	createTrustManagerReadyWithPolicies := func() {
		createTrustManager(ctx, newTrustManagerCR())
		expectTrustManagerDeploymentPresent(ctx)
		By("waiting for trust-manager deployment to become available")
		Expect(waitForDeploymentRollout(ctx, trustManagerNamespace, trustManagerDeploymentName, highTimeout)).To(Succeed())
		expectTrustManagerNetworkPolicies()
	}

	It("CJ-1: creates four reserved policies regardless of CertManager defaultNetworkPolicy", func() {
		cm, err := certmanageroperatorclient.OperatorV1alpha1().CertManagers().Get(ctx, "cluster", metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		By(fmt.Sprintf("CertManager.spec.defaultNetworkPolicy=%q (trust-manager NPs must still apply)", cm.Spec.DefaultNetworkPolicy))

		createTrustManagerReadyWithPolicies()
	})

	It("CJ-2: admits Bundle create with network policies applied", func() {
		createTrustManagerReadyWithPolicies()

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

		Expect(waitForConfigMapTarget(ctx, bundleClient, bundleName, targetNS.Name, "ca.crt", pem, highTimeout)).To(Succeed())
	})

	It("CJ-3: restores drifted deny-all policy after TrustManager spec change and does not select cert-manager pods", func() {
		createTrustManagerReadyWithPolicies()

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
	})

	It("CJ-4: allows HTTP GET to trust-manager-metrics on TCP 9402", func() {
		createTrustManagerReadyWithPolicies()
		expectTrustManagerMetricsReachable(ctx, clientset)
	})
})

// expectNetworkPolicyTCPPort compares the NetworkPolicyPort.Port value. Port is a
// *intstr.IntOrString; comparing the pointer to intstr.FromInt32() always fails.
func expectNetworkPolicyTCPPort(port *intstr.IntOrString, want int32) {
	Expect(port).NotTo(BeNil())
	Expect(*port).To(Equal(intstr.FromInt32(want)))
}

func expectTrustManagerAPIServerEgressPolicy(np *networkingv1.NetworkPolicy) {
	Expect(np.Spec.PolicyTypes).To(Equal([]networkingv1.PolicyType{networkingv1.PolicyTypeEgress}))
	Expect(np.Spec.Ingress).To(BeEmpty())
	Expect(np.Spec.Egress).To(HaveLen(1))
	rule := np.Spec.Egress[0]
	Expect(rule.Ports).To(HaveLen(2))
	gotPorts := make([]int32, 0, len(rule.Ports))
	for i := range rule.Ports {
		Expect(rule.Ports[i].Protocol).NotTo(BeNil())
		Expect(*rule.Ports[i].Protocol).To(Equal(corev1.ProtocolTCP))
		Expect(rule.Ports[i].Port).NotTo(BeNil())
		gotPorts = append(gotPorts, rule.Ports[i].Port.IntVal)
	}
	Expect(gotPorts).To(ConsistOf(int32(443), int32(6443)))
	Expect(rule.To).To(BeEmpty())
}

func expectTrustManagerWebhookIngressPolicy(np *networkingv1.NetworkPolicy) {
	Expect(np.Spec.PolicyTypes).To(Equal([]networkingv1.PolicyType{networkingv1.PolicyTypeIngress}))
	Expect(np.Spec.Egress).To(BeEmpty())
	Expect(np.Spec.Ingress).To(HaveLen(1))
	rule := np.Spec.Ingress[0]
	Expect(rule.Ports).To(HaveLen(1))
	Expect(rule.Ports[0].Protocol).NotTo(BeNil())
	Expect(*rule.Ports[0].Protocol).To(Equal(corev1.ProtocolTCP))
	expectNetworkPolicyTCPPort(rule.Ports[0].Port, 6443)
	Expect(rule.From).To(BeEmpty())
}

func expectTrustManagerMetricsIngressPolicy(np *networkingv1.NetworkPolicy) {
	Expect(np.Spec.PolicyTypes).To(Equal([]networkingv1.PolicyType{networkingv1.PolicyTypeIngress}))
	Expect(np.Spec.Egress).To(BeEmpty())
	Expect(np.Spec.Ingress).To(HaveLen(1))
	rule := np.Spec.Ingress[0]
	Expect(rule.Ports).To(HaveLen(1))
	Expect(rule.Ports[0].Protocol).NotTo(BeNil())
	Expect(*rule.Ports[0].Protocol).To(Equal(corev1.ProtocolTCP))
	expectNetworkPolicyTCPPort(rule.Ports[0].Port, 9402)
	Expect(rule.From).To(BeEmpty())
}

func expectTrustManagerDenyAllPolicy(np *networkingv1.NetworkPolicy) {
	Expect(np.Spec.PolicyTypes).To(Equal([]networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress}))
	Expect(np.Spec.Ingress).To(BeEmpty())
	Expect(np.Spec.Egress).To(BeEmpty())
}

func expectTrustManagerMetricsReachable(ctx context.Context, clientset kubernetes.Interface) {
	By("waiting for Service " + trustManagerMetricsServiceName)
	Eventually(func() error {
		_, err := clientset.CoreV1().Services(trustManagerNamespace).Get(ctx, trustManagerMetricsServiceName, metav1.GetOptions{})
		return err
	}, lowTimeout, fastPollInterval).Should(Succeed())

	ns := createNamespaceWithCleanup(ctx, "tm-np-metrics-", nil)
	const podName = "metrics-probe"
	allowPrivilegeEscalation := false
	runAsNonRoot := true
	runAsUser := int64(65534)
	privileged := false
	metricsURL := fmt.Sprintf("http://%s.%s.svc:9402/metrics", trustManagerMetricsServiceName, trustManagerNamespace)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: podName, Namespace: ns.Name},
		Spec: corev1.PodSpec{
			RestartPolicy:                corev1.RestartPolicyNever,
			AutomountServiceAccountToken: ptr.To(false),
			Containers: []corev1.Container{{
				Name:            "probe",
				Image:           "quay.io/openshifttest/helm:3.17.0",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Command: []string{"sh", "-c",
					`if ! command -v wget >/dev/null 2>&1; then echo no-wget; exit 1; fi; ` +
						`if wget -T 15 -q -O /tmp/metrics ` + metricsURL + `; then echo allowed; cat /tmp/metrics; exit 0; else echo denied; exit 1; fi`,
				},
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: &allowPrivilegeEscalation,
					RunAsNonRoot:             &runAsNonRoot,
					RunAsUser:                &runAsUser,
					Privileged:               &privileged,
					SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
					Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
				},
			}},
		},
	}
	_, err := clientset.CoreV1().Pods(ns.Name).Create(ctx, pod, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())

	Eventually(func(g Gomega) {
		got, getErr := clientset.CoreV1().Pods(ns.Name).Get(ctx, podName, metav1.GetOptions{})
		g.Expect(getErr).NotTo(HaveOccurred())
		g.Expect(got.Status.Phase).To(Equal(corev1.PodSucceeded), "metrics probe phase=%s", got.Status.Phase)
	}, highTimeout, fastPollInterval).Should(Succeed())

	logs, err := clientset.CoreV1().Pods(ns.Name).GetLogs(podName, &corev1.PodLogOptions{}).DoRaw(ctx)
	Expect(err).NotTo(HaveOccurred())
	Expect(string(logs)).To(ContainSubstring("allowed"))
	Expect(string(logs)).NotTo(ContainSubstring("denied"))
	Expect(string(logs)).NotTo(ContainSubstring("no-wget"))
}
