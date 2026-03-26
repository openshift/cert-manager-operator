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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	certmanagerclientset "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned"
	opv1 "github.com/openshift/api/operator/v1"
	certmanoperatorclient "github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned"
	"github.com/openshift/cert-manager-operator/test/library"
	configv1 "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	operatorv1 "github.com/openshift/client-go/operator/clientset/versioned/typed/operator/v1"
	routev1 "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"

	corev1 "k8s.io/api/core/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	trustapi "github.com/cert-manager/trust-manager/pkg/apis/trust/v1alpha1"
)

const (
	operatorName = "cert-manager"

	certManagerController = operatorName + "-controller"
	certManagerWebhook    = operatorName + "-webhook"
	certManagerCAInjector = operatorName + "-cainjector"

	certmanagerControllerDeployment = "cert-manager"
	certmanagerWebhookDeployment    = "cert-manager-webhook"
	certmanagerCAinjectorDeployment = "cert-manager-cainjector"

	operandNamespace       = "cert-manager"
	operatorNamespace      = "cert-manager-operator"
	operatorDeploymentName = "cert-manager-operator-controller-manager"
)

var (
	cfg          *rest.Config
	k8sClientSet *kubernetes.Clientset

	loader library.DynamicResourceLoader

	configClient              *configv1.ConfigV1Client
	oseOperatorClient         *operatorv1.OperatorV1Client
	routeClient               *routev1.RouteV1Client
	certmanageroperatorclient *certmanoperatorclient.Clientset
	certmanagerClient         *certmanagerclientset.Clientset
	bundleClient              crclient.Client

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

	suiteConfig.Timeout = 120 * time.Minute // Set Ginkgo suite-level timeout
	suiteConfig.FailFast = false            // Continue after first failure to see all issues
	suiteConfig.FlakeAttempts = 0           // Retry on flaky tests (helpful when deflaking tests)
	suiteConfig.MustPassRepeatedly = 1      // Must pass repeatedly times (helpful when deflaking tests)

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

	By("creating config.openshift.io/v1 client")
	configClient, err = configv1.NewForConfig(cfg)
	Expect(err).NotTo(HaveOccurred())

	By("creating operator.openshift.io/v1 client")
	oseOperatorClient, err = operatorv1.NewForConfig(cfg)
	Expect(err).NotTo(HaveOccurred())

	By("creating openshift route client")
	routeClient, err = routev1.NewForConfig(cfg)
	Expect(err).NotTo(HaveOccurred())

	By("creating cert-manager client")
	certmanagerClient, err = certmanagerclientset.NewForConfig(cfg)
	Expect(err).NotTo(HaveOccurred())

	By("creating controller-runtime client for Bundle CRs")
	bundleScheme := k8sruntime.NewScheme()
	Expect(trustapi.AddToScheme(bundleScheme)).NotTo(HaveOccurred())
	Expect(corev1.AddToScheme(bundleScheme)).NotTo(HaveOccurred())
	bundleClient, err = crclient.New(cfg, crclient.Options{Scheme: bundleScheme})
	Expect(err).NotTo(HaveOccurred())

	By("setting defaultNetworkPolicy to true")
	err = resetCertManagerNetworkPolicyState(context.TODO(), certmanageroperatorclient)
	Expect(err).NotTo(HaveOccurred())
})
