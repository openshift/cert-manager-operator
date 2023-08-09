package e2e

import (
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

func TestAll(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Cert Manager Suite")
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
