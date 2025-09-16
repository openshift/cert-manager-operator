//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	certmanagerclientset "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned"
	opv1 "github.com/openshift/api/operator/v1"
	certmanoperatorclient "github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned"
	"github.com/openshift/cert-manager-operator/test/library"
	configv1 "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

const (
	operatorName = "cert-manager"

	certManagerController = operatorName + "-controller"
	certManagerWebhook    = operatorName + "-webhook"
	certManagerCAInjector = operatorName + "-cainjector"

	certmanagerControllerDeployment = "cert-manager"
	certmanagerWebhookDeployment    = "cert-manager-webhook"
	certmanagerCAinjectorDeployment = "cert-manager-cainjector"

	operandNamespace = "cert-manager"
)

var (
	cfg          *rest.Config
	k8sClientSet *kubernetes.Clientset

	loader library.DynamicResourceLoader

	configClient              *configv1.ConfigV1Client
	certmanageroperatorclient *certmanoperatorclient.Clientset
	certmanagerClient         *certmanagerclientset.Clientset

	validOperatorStatusConditions = map[string]opv1.ConditionStatus{
		"Available":   opv1.ConditionTrue,
		"Degraded":    opv1.ConditionFalse,
		"Progressing": opv1.ConditionFalse,
	}

	invalidOperatorStatusConditions = map[string]opv1.ConditionStatus{
		"Degraded": opv1.ConditionTrue,
	}

	clusterName string
)

func getTestDir() string {
	// test is running in an OpenShift CI Prow job
	if os.Getenv("OPENSHIFT_CI") == "true" {
		return os.Getenv("ARTIFACT_DIR")
	}
	// not running in a CI job
	return "/tmp"
}

// getClusterName returns the cluster name from the provided rest.Config
func getClusterName(cfg *rest.Config) string {
	// Extract cluster name from the API server URL
	if cfg.Host != "" {
		host := cfg.Host
		host = strings.TrimPrefix(host, "https://")
		host = strings.TrimPrefix(host, "http://")
		return host
	}

	return "unknown-cluster"
}

func TestAll(t *testing.T) {
	RegisterFailHandler(Fail)

	suiteConfig, reportConfig := GinkgoConfiguration()

	testDir := getTestDir()
	reportConfig.JSONReport = filepath.Join(testDir, "report.json")
	reportConfig.JUnitReport = filepath.Join(testDir, "junit.xml")
	reportConfig.NoColor = true
	reportConfig.VeryVerbose = true
	reportConfig.ShowNodeEvents = true
	reportConfig.FullTrace = true
	reportConfig.SilenceSkips = true

	RunSpecs(t, "Cert Manager Suite", suiteConfig, reportConfig)
}

var _ = BeforeSuite(func() {
	var err error
	cfg, err = config.GetConfig()
	Expect(err).NotTo(HaveOccurred())

	clusterName = getClusterName(cfg)
	By(fmt.Sprintf("using cluster: %s", clusterName))

	By("creating Kubernetes client set")
	k8sClientSet, err = kubernetes.NewForConfig(cfg)
	Expect(err).NotTo(HaveOccurred())

	By("creating cert-manager operator client")
	certmanageroperatorclient, err = certmanoperatorclient.NewForConfig(cfg)
	Expect(err).NotTo(HaveOccurred())
	Expect(certmanageroperatorclient).NotTo(BeNil())

	By("verifying operator and cert-manager deployments status is available")
	err = VerifyHealthyOperatorConditions(certmanageroperatorclient.OperatorV1alpha1())
	Expect(err).NotTo(HaveOccurred(), "operator is expected to be available")

	By("creating dynamic resources client")
	loader = library.NewDynamicResourceLoader(context.TODO(), &testing.T{})

	By("creating openshift config client")
	configClient, err = configv1.NewForConfig(cfg)
	Expect(err).NotTo(HaveOccurred())

	By("creating cert-manager client")
	certmanagerClient, err = certmanagerclientset.NewForConfig(cfg)
	Expect(err).NotTo(HaveOccurred())

	By("storing initial operator deployment environment variables")
	err = storeInitialOperatorEnvVars(context.Background(), k8sClientSet)
	Expect(err).NotTo(HaveOccurred())
})
