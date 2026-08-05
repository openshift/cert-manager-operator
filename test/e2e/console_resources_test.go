//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/openshift/library-go/pkg/operator/resource/resourceread"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/openshift/cert-manager-operator/pkg/controller/certmanager"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
	"github.com/openshift/cert-manager-operator/pkg/operator/utils"
)

var consoleExpectedAssets = []struct {
	file string
	name string
	gvr  schema.GroupVersionResource
}{
	{file: "console/cert-manager-acme-issuer-sample.yaml", name: "cert-manager-acme-issuer-sample", gvr: certmanager.ConsoleYAMLSampleGVR},
	{file: "console/cert-manager-certificate-sample.yaml", name: "cert-manager-certificate-sample", gvr: certmanager.ConsoleYAMLSampleGVR},
	{file: "console/cert-manager-issuer-sample.yaml", name: "cert-manager-issuer-sample", gvr: certmanager.ConsoleYAMLSampleGVR},
	{file: "console/cert-manager-example-quickstart.yaml", name: "cert-manager-example", gvr: certmanager.ConsoleQuickStartGVR},
}

func hasConsoleCapability() (bool, error) {
	return utils.NewResourceDiscoverer(
		certmanager.ConsoleYAMLSampleGVR,
		k8sClientSet.Discovery(),
	).Discover()
}

var _ = Describe("Console Resources", Label("Platform:Generic"), Ordered, func() {
	var (
		ctx            context.Context
		consoleEnabled bool
	)

	BeforeAll(func() {
		ctx = context.Background()

		By("detecting Console capability on the cluster")
		enabled, err := hasConsoleCapability()
		Expect(err).NotTo(HaveOccurred(), "failed to check Console capability")
		consoleEnabled = enabled

		if consoleEnabled {
			fmt.Fprintf(GinkgoWriter, "Console capability is ENABLED on this cluster\n")
		} else {
			fmt.Fprintf(GinkgoWriter, "Console capability is DISABLED on this cluster\n")
		}
	})

	BeforeEach(func() {
		By("waiting for operator status to become available")
		err := VerifyHealthyOperatorConditions(certmanageroperatorclient.OperatorV1alpha1())
		Expect(err).NotTo(HaveOccurred(), "operator is expected to be available")
	})

	Context("when Console capability is enabled", func() {
		It("should create all console resources with correct content", func() {
			if !consoleEnabled {
				Skip("Console capability is not enabled on this cluster")
			}

			for _, asset := range consoleExpectedAssets {
				By(fmt.Sprintf("verifying %s exists", asset.name))

				var live *unstructured.Unstructured
				err := wait.PollUntilContextTimeout(ctx, fastPollInterval, lowTimeout, true, func(ctx context.Context) (bool, error) {
					obj, getErr := loader.DynamicClient.Resource(asset.gvr).Get(ctx, asset.name, metav1.GetOptions{})
					if getErr != nil {
						if apierrors.IsNotFound(getErr) {
							return false, nil
						}
						return false, getErr
					}
					live = obj
					return true, nil
				})
				Expect(err).NotTo(HaveOccurred(), "timed out waiting for %s to be created", asset.name)

				By(fmt.Sprintf("validating content of %s", asset.name))
				data, err := assets.Asset(asset.file)
				Expect(err).NotTo(HaveOccurred(), "failed to load bindata asset %s", asset.file)
				expected := resourceread.ReadUnstructuredOrDie(data)

				liveSpec, ok := live.Object["spec"].(map[string]interface{})
				Expect(ok).To(BeTrue(), "live spec should be a map for %s", asset.name)
				expectedSpec, ok := expected.Object["spec"].(map[string]interface{})
				Expect(ok).To(BeTrue(), "expected spec should be a map for %s", asset.name)

				for key, expectedVal := range expectedSpec {
					Expect(liveSpec).To(HaveKeyWithValue(key, expectedVal),
						"spec.%s mismatch for %s", key, asset.name)
				}
			}
		})
	})

	Context("when Console capability is disabled", func() {
		It("should not create any console resources", func() {
			if consoleEnabled {
				Skip("Console capability is enabled on this cluster")
			}

			for _, asset := range consoleExpectedAssets {
				By(fmt.Sprintf("verifying %s does not exist", asset.name))
				_, err := loader.DynamicClient.Resource(asset.gvr).Get(ctx, asset.name, metav1.GetOptions{})
				Expect(apierrors.IsNotFound(err)).To(BeTrue(),
					"expected %s to not exist on consoleless cluster, but got: %v", asset.name, err)
			}
		})
	})
})
