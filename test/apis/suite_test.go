package apis

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/envtest/komega"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	operatorv1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	//+kubebuilder:scaffold:imports
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	g := NewGomegaWithT(t)

	var err error
	suites, err = LoadTestSuiteSpecs(filepath.Join("..", "..", "api"))
	g.Expect(err).ToNot(HaveOccurred())

	RunSpecs(t, "API Integration Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "..", "config", "crd", "bases")},
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	testScheme = scheme.Scheme
	Expect(operatorv1alpha1.AddToScheme(testScheme)).To(Succeed())

	//+kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: testScheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	// CEL requires Kube 1.25 and above, so check for the minimum server version.
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(cfg)
	Expect(err).ToNot(HaveOccurred())

	serverVersion, err := discoveryClient.ServerVersion()
	Expect(err).ToNot(HaveOccurred())

	Expect(serverVersion.Major).To(Equal("1"))

	minorInt, err := strconv.Atoi(strings.Split(serverVersion.Minor, "+")[0])
	Expect(err).ToNot(HaveOccurred())
	Expect(minorInt).To(BeNumerically(">=", 25), fmt.Sprintf("This test suite requires a Kube API server of at least version 1.25, current version is 1.%s", serverVersion.Minor))

	komega.SetClient(k8sClient)
	komega.SetContext(ctx)
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

var _ = Describe("API Integration Tests", Ordered, ContinueOnFailure, func() {
	for _, suite := range suites {
		GenerateTestSuite(suite)
	}
})
