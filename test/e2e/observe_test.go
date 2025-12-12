//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	operatorv1 "github.com/openshift/api/operator/v1"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Logging Configuration", Label("TechPreview"), Ordered, func() {
	var ctx context.Context

	const (
		operatorNamespace      = "cert-manager-operator"
		operandNamespace       = "cert-manager"
		operatorDeploymentName = "cert-manager-operator-controller-manager"
	)

	BeforeAll(func() {
		ctx = context.Background()
	})

	BeforeEach(func() {
		By("waiting for operator status to become available")
		err := VerifyHealthyOperatorConditions(certmanageroperatorclient.OperatorV1alpha1())
		Expect(err).NotTo(HaveOccurred(), "Operator is expected to be available")
	})

	Context("operator log level", func() {
		It("should allow setting operator log level via subscription", func() {
			By("setting operator log level to 6 via subscription env var")
			err := patchSubscriptionWithEnvVars(ctx, loader, map[string]string{
				"OPERATOR_LOG_LEVEL": "6",
			})
			Expect(err).NotTo(HaveOccurred(), "failed to patch subscription with OPERATOR_LOG_LEVEL environment variable")

			DeferCleanup(func(ctx context.Context) {
				By("removing OPERATOR_LOG_LEVEL from subscription")
				if err := patchSubscriptionWithEnvVars(ctx, loader, map[string]string{}); err != nil {
					fmt.Fprintf(GinkgoWriter, "failed to remove env var from subscription during cleanup: %v\n", err)
					return
				}
			})

			By("waiting for operator deployment to have the new log level and rollout")
			err = waitForDeploymentEnvVarAndRollout(ctx, operatorNamespace, operatorDeploymentName, "OPERATOR_LOG_LEVEL", "6", lowTimeout)
			Expect(err).NotTo(HaveOccurred(), "timeout waiting for cert-manager-operator deployment to have OPERATOR_LOG_LEVEL=6 and complete rollout")
		})
	})

	Context("operand log level", func() {
		It("should allow setting operand log level via CertManager CR", func() {
			By("getting CertManager CR")
			certManager, err := certmanageroperatorclient.OperatorV1alpha1().CertManagers().Get(ctx, "cluster", metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to get CertManager CR")

			By("setting operand log level to Trace")
			originalLogLevel := certManager.Spec.LogLevel // record original log level
			certManager.Spec.LogLevel = operatorv1.Trace
			_, err = certmanageroperatorclient.OperatorV1alpha1().CertManagers().Update(ctx, certManager, metav1.UpdateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to update CertManager CR with Trace log level")

			DeferCleanup(func(ctx context.Context) {
				By("restoring original log level")
				certManager, err := certmanageroperatorclient.OperatorV1alpha1().CertManagers().Get(ctx, "cluster", metav1.GetOptions{})
				if err != nil {
					fmt.Fprintf(GinkgoWriter, "failed to get CertManager CR during cleanup: %v\n", err)
					return
				}
				certManager.Spec.LogLevel = originalLogLevel // restore original log level
				if _, err = certmanageroperatorclient.OperatorV1alpha1().CertManagers().Update(ctx, certManager, metav1.UpdateOptions{}); err != nil {
					fmt.Fprintf(GinkgoWriter, "failed to restore original log level during cleanup: %v\n", err)
					return
				}
			})

			By("waiting for operand deployments to have --v=6 argument and rollout")
			for _, name := range []string{"cert-manager", "cert-manager-cainjector", "cert-manager-webhook"} {
				err = waitForDeploymentArgAndRollout(ctx, operandNamespace, name, "--v=6", lowTimeout)
				Expect(err).NotTo(HaveOccurred(), "timeout waiting for %s deployment to have --v=6 and complete rollout", name)
			}
		})

		It("should reject invalid log level values", func() {
			By("getting CertManager CR")
			certManager, err := certmanageroperatorclient.OperatorV1alpha1().CertManagers().Get(ctx, "cluster", metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to get CertManager CR")

			By("attempting to set an invalid log level")
			certManager.Spec.LogLevel = "InvalidLevel"
			_, err = certmanageroperatorclient.OperatorV1alpha1().CertManagers().Update(ctx, certManager, metav1.UpdateOptions{})

			By("verifying the update is rejected by API validation")
			Expect(err).To(HaveOccurred(), "update with invalid log level should fail")
			Expect(err.Error()).To(ContainSubstring("Unsupported value"), "error should mention unsupported value")
		})
	})
})

var _ = Describe("Monitoring and Metrics", Label("TechPreview"), Ordered, func() {
	var ctx context.Context

	const (
		clusterMonitoringNamespace      = "openshift-monitoring"
		clusterMonitoringConfigMapName  = "cluster-monitoring-config"
		userWorkloadMonitoringNamespace = "openshift-user-workload-monitoring"
	)

	var (
		thanosQuerierURL string
		token            string
	)

	// findReadyPrometheusPod returns the name of a ready Prometheus pod.
	findReadyPrometheusPod := func(ctx context.Context) (string, error) {
		pods, err := loader.KubeClient.CoreV1().Pods(clusterMonitoringNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/name=prometheus",
		})
		if err != nil {
			return "", fmt.Errorf("failed to list Prometheus pods: %w", err)
		}
		for i := range pods.Items {
			if isPodReady(&pods.Items[i]) {
				return pods.Items[i].Name, nil
			}
		}
		return "", fmt.Errorf("no ready Prometheus pod found")
	}

	// queryPrometheusMetrics executes a PromQL query against Thanos Querier.
	queryPrometheusMetrics := func(ctx context.Context, jobLabel string) (string, error) {
		prometheusPod, err := findReadyPrometheusPod(ctx)
		if err != nil {
			return "", err
		}

		query := fmt.Sprintf(`{job="%s"}`, jobLabel)
		curlCmd := []string{
			"curl", "-s", "-S", "-k",
			"-H", fmt.Sprintf("Authorization: Bearer %s", token),
			"--data-urlencode", fmt.Sprintf("query=%s", query),
			thanosQuerierURL,
		}

		output, err := execInPod(ctx, cfg, loader.KubeClient, clusterMonitoringNamespace, prometheusPod, "prometheus", curlCmd...)
		if err != nil {
			return "", fmt.Errorf("failed to execute query: %w", err)
		}
		return output, nil
	}

	// validateMetricsResponse checks if the Prometheus query response contains expected data.
	validateMetricsResponse := func(output, componentName, jobLabel string) bool {
		if !strings.Contains(output, `"status":"success"`) {
			GinkgoLogr.Info("Query did not return success status", "component", componentName)
			return false
		}
		if strings.Contains(output, `"result":[]`) {
			GinkgoLogr.Info("Query returned empty result set - metrics not yet available", "component", componentName)
			return false
		}
		if !strings.Contains(output, fmt.Sprintf(`"namespace":"%s"`, operandNamespace)) {
			GinkgoLogr.Info("Query did not return expected namespace", "component", componentName, "expected", operandNamespace)
			return false
		}
		if !strings.Contains(output, fmt.Sprintf(`"job":"%s"`, jobLabel)) {
			GinkgoLogr.Info("Query did not return expected job label", "component", componentName, "expected", jobLabel)
			return false
		}
		return true
	}

	// testComponentMetrics creates a ServiceMonitor and verifies metrics are scraped.
	testComponentMetrics := func(serviceMonitorName, appName, componentName, jobLabel string) {
		By(fmt.Sprintf("creating ServiceMonitor for cert-manager %s", componentName))
		loader.CreateFromFile(
			AssetFunc(testassets.ReadFile).WithTemplateValues(ServiceMonitorConfig{
				Name:          serviceMonitorName,
				Namespace:     operandNamespace,
				AppName:       appName,
				ComponentName: componentName,
			}),
			filepath.Join("testdata", "observe", "servicemonitor.yaml"),
			operandNamespace,
		)

		DeferCleanup(func(ctx context.Context) {
			if err := loader.DynamicClient.Resource(serviceMonitorGVR).Namespace(operandNamespace).Delete(ctx, serviceMonitorName, metav1.DeleteOptions{}); err != nil {
				fmt.Fprintf(GinkgoWriter, "failed to delete ServiceMonitor %s during cleanup: %v\n", serviceMonitorName, err)
			}
		})

		By(fmt.Sprintf("waiting for cert-manager %s metrics to be available", componentName))
		err := wait.PollUntilContextTimeout(ctx, slowPollInterval, highTimeout, false, func(pollCtx context.Context) (bool, error) {
			output, err := queryPrometheusMetrics(pollCtx, jobLabel)
			if err != nil {
				GinkgoLogr.Info("Failed to query metrics", "component", componentName, "error", err)
				return false, nil
			}
			return validateMetricsResponse(output, componentName, jobLabel), nil
		})
		Expect(err).NotTo(HaveOccurred(), "timeout waiting for cert-manager %s metrics to be available", componentName)
	}

	// ensureUserWorkloadMonitoringEnabled ensures the cluster-monitoring-config ConfigMap
	// has enableUserWorkload: true set. It creates the ConfigMap if it doesn't exist,
	// or updates it if the setting is missing or false.
	ensureUserWorkloadMonitoringEnabled := func(ctx context.Context) error {
		configMap, err := loader.KubeClient.CoreV1().ConfigMaps(clusterMonitoringNamespace).Get(ctx, clusterMonitoringConfigMapName, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			// ConfigMap doesn't exist, create it
			configMap = &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterMonitoringConfigMapName,
					Namespace: clusterMonitoringNamespace,
				},
				Data: map[string]string{
					"config.yaml": "enableUserWorkload: true\n",
				},
			}
			_, err = loader.KubeClient.CoreV1().ConfigMaps(clusterMonitoringNamespace).Create(ctx, configMap, metav1.CreateOptions{})
			return err
		}
		if err != nil {
			return err
		}

		// ConfigMap exists, check if enableUserWorkload is already true
		configYAML := configMap.Data["config.yaml"]
		var config map[string]any
		if configYAML != "" {
			if err := yaml.Unmarshal([]byte(configYAML), &config); err != nil {
				return fmt.Errorf("failed to parse config.yaml: %w", err)
			}
		}
		if config == nil {
			config = make(map[string]any)
		}

		// Check if already enabled
		if enabled, ok := config["enableUserWorkload"].(bool); ok && enabled {
			return nil
		}

		// Need to enable it manually
		config["enableUserWorkload"] = true
		updatedYAML, err := yaml.Marshal(config)
		if err != nil {
			return fmt.Errorf("failed to marshal config.yaml: %w", err)
		}

		if configMap.Data == nil {
			configMap.Data = make(map[string]string)
		}
		configMap.Data["config.yaml"] = string(updatedYAML)
		_, err = loader.KubeClient.CoreV1().ConfigMaps(clusterMonitoringNamespace).Update(ctx, configMap, metav1.UpdateOptions{})
		return err
	}

	BeforeAll(func() {
		ctx = context.Background()

		By("waiting for operator status to become available")
		err := VerifyHealthyOperatorConditions(certmanageroperatorclient.OperatorV1alpha1())
		Expect(err).NotTo(HaveOccurred(), "Operator is expected to be available")

		By("ensuring user workload monitoring is enabled")
		err = ensureUserWorkloadMonitoringEnabled(ctx)
		Expect(err).NotTo(HaveOccurred(), "failed to enable user workload monitoring")

		By("waiting for user-workload-monitoring pods to be ready")
		err = wait.PollUntilContextTimeout(ctx, slowPollInterval, highTimeout, true, func(pollCtx context.Context) (bool, error) {
			pods, err := loader.KubeClient.CoreV1().Pods(userWorkloadMonitoringNamespace).List(pollCtx, metav1.ListOptions{})
			if err != nil {
				return false, nil
			}
			if len(pods.Items) == 0 {
				return false, nil
			}
			for i := range pods.Items {
				if !isPodReady(&pods.Items[i]) {
					return false, nil
				}
			}
			return true, nil
		})
		Expect(err).NotTo(HaveOccurred(), "timeout waiting for user-workload-monitoring pods to be ready")

		By("getting Thanos Querier route")
		route, err := routeClient.Routes(clusterMonitoringNamespace).Get(ctx, "thanos-querier", metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to get thanos-querier route")
		Expect(route.Status.Ingress).NotTo(BeEmpty(), "route ingress should not be empty")

		thanosQuerierHost := route.Status.Ingress[0].Host
		Expect(thanosQuerierHost).NotTo(BeEmpty(), "Thanos Querier host should not be empty")
		thanosQuerierURL = fmt.Sprintf("https://%s/api/v1/query", thanosQuerierHost)

		By("getting Prometheus service account token")
		token, err = getSAToken(ctx, "prometheus-k8s", clusterMonitoringNamespace)
		Expect(err).NotTo(HaveOccurred(), "failed to get prometheus-k8s service account token")
		Expect(token).NotTo(BeEmpty(), "service account token should not be empty")
	})

	Context("user workload monitoring", func() {
		It("should scrape cert-manager controller metrics", func() {
			testComponentMetrics("cert-manager-controller", "cert-manager", "controller", "cert-manager")
		})

		It("should scrape cert-manager cainjector metrics", func() {
			testComponentMetrics("cert-manager-cainjector", "cainjector", "cainjector", "cert-manager-cainjector")
		})

		It("should scrape cert-manager webhook metrics", func() {
			testComponentMetrics("cert-manager-webhook", "webhook", "webhook", "cert-manager-webhook")
		})
	})
})
