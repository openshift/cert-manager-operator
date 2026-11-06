//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"crypto/x509"
	"fmt"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	testutils "github.com/openshift/cert-manager-operator/pkg/controller/istiocsr"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
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

		Expect(waitForConfigMapTarget(ctx, bundleClient, bundleName, targetNS.Name, "ca.crt", pem, highTimeout)).To(Succeed())
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
	})

	It("CJ-4: allows metrics scrape from openshift-user-workload-monitoring and denies other clients", func() {
		createTrustManager(ctx, newTrustManagerCR())
		expectTrustManagerNetworkPolicies()

		By("confirming a client outside openshift-user-workload-monitoring cannot reach metrics")
		expectTrustManagerMetricsDenied(ctx, clientset)

		_, err := clientset.CoreV1().Namespaces().Get(ctx, uwmNamespace, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			Skip("namespace openshift-user-workload-monitoring not present")
		}
		Expect(err).NotTo(HaveOccurred())

		pods, err := clientset.CoreV1().Pods(uwmNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/name=prometheus",
		})
		Expect(err).NotTo(HaveOccurred())
		ready := false
		for i := range pods.Items {
			if isPodReady(&pods.Items[i]) {
				ready = true
				break
			}
		}
		if !ready {
			Skip("user-workload-monitoring Prometheus is not ready")
		}

		By("creating a ServiceMonitor for trust-manager-metrics")
		smFile := filepath.Join("testdata", "observe", "trust-manager-servicemonitor.yaml")
		loader.CreateFromFile(testassets.ReadFile, smFile, trustManagerNamespace)
		DeferCleanup(func() {
			loader.DeleteFromFile(testassets.ReadFile, smFile, trustManagerNamespace)
		})

		By("waiting for user-workload monitoring to scrape trust-manager")
		route, err := routeClient.Routes("openshift-monitoring").Get(ctx, "thanos-querier", metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(route.Status.Ingress).NotTo(BeEmpty())
		queryURL := fmt.Sprintf("https://%s/api/v1/query", route.Status.Ingress[0].Host)
		token, err := getSAToken(ctx, "prometheus-k8s", "openshift-monitoring")
		Expect(err).NotTo(HaveOccurred())

		err = wait.PollUntilContextTimeout(ctx, slowPollInterval, highTimeout, false, func(pollCtx context.Context) (bool, error) {
			promPod, podErr := readyPrometheusPod(pollCtx, clientset)
			if podErr != nil {
				return false, nil
			}
			output, queryErr := execInPod(pollCtx, cfg, clientset, "openshift-monitoring", promPod, "prometheus",
				"curl", "-s", "-S", "-k",
				"-H", fmt.Sprintf("Authorization: Bearer %s", token),
				"--data-urlencode", `query=up{namespace="cert-manager",service="trust-manager-metrics"}==1`,
				queryURL,
			)
			if queryErr != nil {
				return false, nil
			}
			return strings.Contains(output, `"status":"success"`) && !strings.Contains(output, `"result":[]`), nil
		})
		Expect(err).NotTo(HaveOccurred(), "timeout waiting for trust-manager metrics scrape")
	})
})

// expectNonEmptyNamespaceSelector fails when namespaceSelector is missing or matches every
// namespace ({} / empty label selector), which would allow overly broad policy peers.
func expectNonEmptyNamespaceSelector(ns *metav1.LabelSelector) {
	Expect(ns).NotTo(BeNil(), "namespace selector must be set (empty {} matches all namespaces)")
	Expect(ns.MatchExpressions).To(BeEmpty(), "unexpected namespace matchExpressions on trust-manager operator policy")
	Expect(ns.MatchLabels).NotTo(BeEmpty(), "namespace selector must not be empty (matches all namespaces)")
}

func expectTrustManagerAPIServerEgressPolicy(np *networkingv1.NetworkPolicy) {
	Expect(np.Spec.PolicyTypes).To(Equal([]networkingv1.PolicyType{networkingv1.PolicyTypeEgress}))
	Expect(np.Spec.Ingress).To(BeEmpty())
	Expect(np.Spec.Egress).To(HaveLen(1))
	rule := np.Spec.Egress[0]
	Expect(rule.Ports).To(HaveLen(1))
	Expect(rule.Ports[0].Protocol).NotTo(BeNil())
	Expect(*rule.Ports[0].Protocol).To(Equal(corev1.ProtocolTCP))
	Expect(rule.Ports[0].Port).To(Equal(intstr.FromInt32(6443)))
	Expect(rule.To).To(HaveLen(1))
	peer := rule.To[0]
	Expect(peer.IPBlock).To(BeNil())
	Expect(peer.PodSelector).To(BeNil())
	expectNonEmptyNamespaceSelector(peer.NamespaceSelector)
	Expect(peer.NamespaceSelector.MatchLabels).To(Equal(map[string]string{
		"kubernetes.io/metadata.name": "openshift-kube-apiserver",
	}))
}

func expectTrustManagerWebhookIngressPolicy(np *networkingv1.NetworkPolicy) {
	Expect(np.Spec.PolicyTypes).To(Equal([]networkingv1.PolicyType{networkingv1.PolicyTypeIngress}))
	Expect(np.Spec.Egress).To(BeEmpty())
	Expect(np.Spec.Ingress).To(HaveLen(1))
	rule := np.Spec.Ingress[0]
	Expect(rule.Ports).To(HaveLen(1))
	Expect(rule.Ports[0].Protocol).NotTo(BeNil())
	Expect(*rule.Ports[0].Protocol).To(Equal(corev1.ProtocolTCP))
	Expect(rule.Ports[0].Port).To(Equal(intstr.FromInt32(6443)))
	Expect(rule.From).To(HaveLen(1))
	peer := rule.From[0]
	Expect(peer.IPBlock).To(BeNil())
	Expect(peer.PodSelector).To(BeNil())
	expectNonEmptyNamespaceSelector(peer.NamespaceSelector)
	Expect(peer.NamespaceSelector.MatchLabels).To(Equal(map[string]string{
		"policy-group.network.openshift.io/host-network": "",
	}))
}

func expectTrustManagerMetricsIngressPolicy(np *networkingv1.NetworkPolicy) {
	Expect(np.Spec.PolicyTypes).To(Equal([]networkingv1.PolicyType{networkingv1.PolicyTypeIngress}))
	Expect(np.Spec.Egress).To(BeEmpty())
	Expect(np.Spec.Ingress).To(HaveLen(1))
	rule := np.Spec.Ingress[0]
	Expect(rule.Ports).To(HaveLen(1))
	Expect(rule.Ports[0].Protocol).NotTo(BeNil())
	Expect(*rule.Ports[0].Protocol).To(Equal(corev1.ProtocolTCP))
	Expect(rule.Ports[0].Port).To(Equal(intstr.FromInt32(9402)))
	Expect(rule.From).To(HaveLen(1))
	peer := rule.From[0]
	Expect(peer.IPBlock).To(BeNil())
	Expect(peer.PodSelector).To(BeNil())
	expectNonEmptyNamespaceSelector(peer.NamespaceSelector)
	Expect(peer.NamespaceSelector.MatchLabels).To(Equal(map[string]string{
		"kubernetes.io/metadata.name": uwmNamespace,
	}))
}

func expectTrustManagerDenyAllPolicy(np *networkingv1.NetworkPolicy) {
	Expect(np.Spec.PolicyTypes).To(Equal([]networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress}))
	Expect(np.Spec.Ingress).To(BeEmpty())
	Expect(np.Spec.Egress).To(BeEmpty())
}

func readyPrometheusPod(ctx context.Context, clientset kubernetes.Interface) (string, error) {
	pods, err := clientset.CoreV1().Pods("openshift-monitoring").List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/name=prometheus",
	})
	if err != nil {
		return "", err
	}
	for i := range pods.Items {
		if isPodReady(&pods.Items[i]) {
			return pods.Items[i].Name, nil
		}
	}
	return "", fmt.Errorf("no ready Prometheus pod")
}

func expectTrustManagerMetricsDenied(ctx context.Context, clientset kubernetes.Interface) {
	ns := createNamespaceWithCleanup(ctx, "tm-np-deny-", nil)
	const podName = "metrics-probe"
	allowPrivilegeEscalation := false
	runAsNonRoot := true
	runAsUser := int64(65534)
	privileged := false
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
						`if wget -T 5 -q -O /dev/null http://trust-manager-metrics.cert-manager.svc:9402/metrics; then echo allowed; exit 1; else echo denied; exit 0; fi`,
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
		g.Expect(got.Status.Phase).To(Equal(corev1.PodSucceeded))
	}, highTimeout, fastPollInterval).Should(Succeed())

	logs, err := clientset.CoreV1().Pods(ns.Name).GetLogs(podName, &corev1.PodLogOptions{}).DoRaw(ctx)
	Expect(err).NotTo(HaveOccurred())
	Expect(string(logs)).To(ContainSubstring("denied"))
	Expect(string(logs)).NotTo(ContainSubstring("allowed"))
}
