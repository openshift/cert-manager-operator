//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cluster TLS security profile", Label("Platform:Generic"), Ordered, func() {
	var ctx context.Context

	BeforeAll(func() {
		ctx = context.Background()
	})

	BeforeEach(func() {
		By("waiting for operator status to become available")
		err := VerifyHealthyOperatorConditions(certmanageroperatorclient.OperatorV1alpha1())
		Expect(err).NotTo(HaveOccurred(), "Operator is expected to be available")
	})

	It("should configure operand container TLS args from apiserver cluster profile", func() {
		state, err := getClusterTLSProfileState(ctx)
		if apierrors.IsNotFound(err) {
			Skip("apiserver.config.openshift.io/cluster is not available on this cluster")
		}
		Expect(err).NotTo(HaveOccurred(), "failed to read cluster TLS profile state")

		if !state.honor {
			Skip(fmt.Sprintf("cluster tlsAdherence=%q does not require cert-manager to honor the TLS profile", state.adherence))
		}

		By("verifying cert-manager operand deployments expose cluster TLS flags")
		for _, name := range []string{
			certmanagerControllerDeployment,
			certmanagerWebhookDeployment,
			certmanagerCAinjectorDeployment,
		} {
			err := verifyOperandTLSArgsMatchClusterProfile(name, state.spec)
			Expect(err).NotTo(HaveOccurred(), "deployment %s", name)
		}
	})
})
