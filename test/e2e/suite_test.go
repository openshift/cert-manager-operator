//go:build e2e
// +build e2e

package e2e

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	certmanoperatorclient "github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

const (
	operatorName = "cert-manager"

	certManagerControllerDeploymentControllerName = operatorName + "-controller-deployment"
	certManagerWebhookDeploymentControllerName    = operatorName + "-webhook-deployment"
	certManagerCAInjectorDeploymentControllerName = operatorName + "-cainjector-deployment"
)

var (
	cfg          *rest.Config
	k8sClientSet *kubernetes.Clientset

	certmanageroperatorclient *certmanoperatorclient.Clientset
)

func getTestDir() string {
	// test is running in an OpenShift CI Prow job
	if os.Getenv("OPENSHIFT_CI") == "true" {
		return os.Getenv("ARTIFACT_DIR")
	}
	// not running in a CI job
	return "/tmp"
}

func TestAll(t *testing.T) {
	RegisterFailHandler(Fail)

	suiteConfig, reportConfig := GinkgoConfiguration()

	testDir := getTestDir()
	reportConfig.JSONReport = filepath.Join(testDir, "report.json")
	reportConfig.JUnitReport = filepath.Join(testDir, "junit.xml")
	reportConfig.NoColor = true
	reportConfig.VeryVerbose = true

	RunSpecs(t, "Cert Manager Suite", suiteConfig, reportConfig)
}

var _ = BeforeSuite(func() {
	var err error
	cfg, err = config.GetConfig()
	Expect(err).NotTo(HaveOccurred())

	k8sClientSet, err = kubernetes.NewForConfig(cfg)
	Expect(err).NotTo(HaveOccurred())

	certmanageroperatorclient, err = certmanoperatorclient.NewForConfig(cfg)
	Expect(err).NotTo(HaveOccurred())
	Expect(certmanageroperatorclient).NotTo(BeNil())

	err = waitForValidOperatorStatusCondition(certmanageroperatorclient,
		[]string{certManagerControllerDeploymentControllerName,
			certManagerWebhookDeploymentControllerName,
			certManagerCAInjectorDeploymentControllerName})
	Expect(err).NotTo(HaveOccurred(), "operator is expected to be available")
})
