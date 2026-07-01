//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"

	configapiv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/tlsprofile"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/retry"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// tlsProfileTestDeployments lists every cert-manager operand that the TLS hook touches.
var tlsProfileTestDeployments = []string{
	certmanagerControllerDeployment,
	certmanagerWebhookDeployment,
	certmanagerCAinjectorDeployment,
}

// tlsProfileModernProfile is {"type":"Modern","modern":{}} — the OpenShift API union requires
// the matching sub-object to be present alongside the type discriminator.
var tlsProfileModernProfile = &configapiv1.TLSSecurityProfile{
	Type:   configapiv1.TLSProfileModernType,
	Modern: &configapiv1.ModernTLSProfile{},
}

var tlsProfileIntermediateProfile = &configapiv1.TLSSecurityProfile{
	Type:         configapiv1.TLSProfileIntermediateType,
	Intermediate: &configapiv1.IntermediateTLSProfile{},
}

var tlsProfileOldProfile = &configapiv1.TLSSecurityProfile{
	Type: configapiv1.TLSProfileOldType,
	Old:  &configapiv1.OldTLSProfile{},
}

// tlsProfileCustomProfile uses a small cipher list that satisfies the HTTP/2 requirement
// (must include ECDHE-RSA-AES128-GCM-SHA256 or ECDHE-ECDSA-AES128-GCM-SHA256).
var tlsProfileCustomProfile = &configapiv1.TLSSecurityProfile{
	Type: configapiv1.TLSProfileCustomType,
	Custom: &configapiv1.CustomTLSProfile{
		TLSProfileSpec: configapiv1.TLSProfileSpec{
			MinTLSVersion: configapiv1.VersionTLS12,
			Ciphers: []string{
				"ECDHE-RSA-AES128-GCM-SHA256",   // HTTP/2 required
				"ECDHE-RSA-AES256-GCM-SHA384",
				"ECDHE-ECDSA-AES256-GCM-SHA384",
			},
		},
	},
}

var _ = Describe("Cluster TLS security profile", Label("Platform:Generic", "Feature:TLSProfile", "TechPreview"), Ordered, func() {
	var (
		ctx      context.Context
		original *apiserverTLSConfig
	)

	BeforeAll(func() {
		ctx = context.Background()

		By("saving original apiserver TLS configuration")
		var err error
		original, err = getClusterAPIServerTLSConfig(ctx)
		if apierrors.IsNotFound(err) {
			Skip("apiserver.config.openshift.io/cluster not available on this cluster")
		}
		Expect(err).NotTo(HaveOccurred(), "failed to read original apiserver TLS config")

		By("verifying tlsAdherence field is available in this cluster's API schema")
		err = updateClusterAPIServerTLSConfig(ctx, nil,
			configapiv1.TLSAdherencePolicyLegacyAdheringComponentsOnly)
		if isTLSAdherenceUnsupported(err) {
			Skip(fmt.Sprintf("apiserver tlsAdherence field not available on this cluster: %v", err))
		}
		Expect(err).NotTo(HaveOccurred())
	})

	AfterAll(func() {
		By("restoring original apiserver TLS configuration")
		Eventually(func() error {
			return restoreClusterAPIServerTLSConfig(ctx, original)
		}, lowTimeout, fastPollInterval).Should(Succeed())

		By("clearing any unsupportedConfigOverrides left by scenario 8")
		Eventually(func() error {
			return setWebhookUnsupportedArgs(ctx, nil)
		}, lowTimeout, fastPollInterval).Should(Succeed())
	})

	BeforeEach(func() {
		By("waiting for operator to be healthy before each scenario")
		Expect(VerifyHealthyOperatorConditions(certmanageroperatorclient.OperatorV1alpha1())).
			To(Succeed(), "operator must be healthy before scenario starts")
	})

	// ─────────────────────────────────────────────────────────────────────────
	// Scenario 1: LegacyAdheringComponentsOnly — operands must be untouched
	// ─────────────────────────────────────────────────────────────────────────
	It("S1: should not inject TLS args when tlsAdherence is LegacyAdheringComponentsOnly", func() {
		By("patching apiserver: tlsSecurityProfile=nil, tlsAdherence=LegacyAdheringComponentsOnly")
		Expect(updateClusterAPIServerTLSConfig(ctx, nil,
			configapiv1.TLSAdherencePolicyLegacyAdheringComponentsOnly)).
			To(Succeed())

		By("verifying no TLS profile args appear on any operand")
		for _, dep := range tlsProfileTestDeployments {
			Expect(verifyDeploymentArgs(k8sClientSet, dep,
				[]string{"--tls-min-version=", "--metrics-tls-min-version="}, false)).
				To(Succeed(), "deployment %s must not have TLS profile args", dep)
		}
	})

	// ─────────────────────────────────────────────────────────────────────────
	// Scenario 2: Modern (TLS 1.3) + StrictAllComponents
	// ─────────────────────────────────────────────────────────────────────────
	It("S2: should inject VersionTLS13 args and no cipher flags for Modern profile", func() {
		By("patching apiserver: Modern + StrictAllComponents")
		Expect(updateClusterAPIServerTLSConfig(ctx, tlsProfileModernProfile,
			configapiv1.TLSAdherencePolicyStrictAllComponents)).
			To(Succeed())

		expectedSpec, err := tlsprofile.EffectiveSpec(tlsProfileModernProfile)
		Expect(err).NotTo(HaveOccurred())

		By("verifying all operands receive VersionTLS13 args")
		for _, dep := range tlsProfileTestDeployments {
			Expect(verifyOperandTLSArgsMatchClusterProfile(dep, expectedSpec)).
				To(Succeed(), "deployment %s", dep)
		}

		By("verifying cert-manager-webhook has NO cipher suite args (TLS 1.3 ignores ciphers)")
		Expect(verifyDeploymentArgs(k8sClientSet, certmanagerWebhookDeployment,
			tlsprofile.CertManagerCipherSuiteArgKeys, false)).
			To(Succeed(), "cipher suite args must be absent for TLS 1.3")

		By("verifying controller has no --tls-min-version (no main TLS listener)")
		Expect(verifyDeploymentArgs(k8sClientSet, certmanagerControllerDeployment,
			[]string{"--tls-min-version="}, false)).
			To(Succeed(), "controller must not have --tls-min-version")
	})

	// ─────────────────────────────────────────────────────────────────────────
	// Scenario 3: Intermediate (TLS 1.2 + ciphers) + StrictAllComponents
	// ─────────────────────────────────────────────────────────────────────────
	It("S3: should inject VersionTLS12 args with full cipher list for Intermediate profile", func() {
		By("patching apiserver: Intermediate + StrictAllComponents")
		Expect(updateClusterAPIServerTLSConfig(ctx, tlsProfileIntermediateProfile,
			configapiv1.TLSAdherencePolicyStrictAllComponents)).
			To(Succeed())

		expectedSpec, err := tlsprofile.EffectiveSpec(tlsProfileIntermediateProfile)
		Expect(err).NotTo(HaveOccurred())

		By("verifying all operands receive VersionTLS12 args with ciphers")
		for _, dep := range tlsProfileTestDeployments {
			Expect(verifyOperandTLSArgsMatchClusterProfile(dep, expectedSpec)).
				To(Succeed(), "deployment %s", dep)
		}

		By("verifying controller and cainjector do NOT have --tls-min-version (metrics-only TLS)")
		for _, dep := range []string{certmanagerControllerDeployment, certmanagerCAinjectorDeployment} {
			Expect(verifyDeploymentArgs(k8sClientSet, dep,
				[]string{"--tls-min-version="}, false)).
				To(Succeed(), "deployment %s must not have --tls-min-version", dep)
		}
	})

	// ─────────────────────────────────────────────────────────────────────────
	// Scenario 4: Old (TLS 1.0 + extended ciphers) + StrictAllComponents
	// ─────────────────────────────────────────────────────────────────────────
	It("S4: should inject VersionTLS10 args with legacy cipher list for Old profile", func() {
		By("patching apiserver: Old + StrictAllComponents")
		Expect(updateClusterAPIServerTLSConfig(ctx, tlsProfileOldProfile,
			configapiv1.TLSAdherencePolicyStrictAllComponents)).
			To(Succeed())

		expectedSpec, err := tlsprofile.EffectiveSpec(tlsProfileOldProfile)
		Expect(err).NotTo(HaveOccurred())

		By("verifying all operands receive VersionTLS10 args with extended cipher list")
		for _, dep := range tlsProfileTestDeployments {
			Expect(verifyOperandTLSArgsMatchClusterProfile(dep, expectedSpec)).
				To(Succeed(), "deployment %s", dep)
		}
	})

	// ─────────────────────────────────────────────────────────────────────────
	// Scenario 5: Custom profile + StrictAllComponents
	// OpenSSL cipher names must be converted to IANA format in the injected args.
	// ─────────────────────────────────────────────────────────────────────────
	It("S5: should inject IANA-converted cipher names for Custom profile", func() {
		By("patching apiserver: Custom (TLS1.2 + 3 ciphers) + StrictAllComponents")
		Expect(updateClusterAPIServerTLSConfig(ctx, tlsProfileCustomProfile,
			configapiv1.TLSAdherencePolicyStrictAllComponents)).
			To(Succeed())

		expectedSpec, err := tlsprofile.EffectiveSpec(tlsProfileCustomProfile)
		Expect(err).NotTo(HaveOccurred())

		By("verifying all operands receive custom TLS args with IANA-converted cipher names")
		// verifyOperandTLSArgsMatchClusterProfile compares the full --tls-cipher-suites=... arg,
		// which contains IANA names (e.g. TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256) converted from
		// the OpenSSL names supplied in the custom profile (e.g. ECDHE-RSA-AES128-GCM-SHA256).
		for _, dep := range tlsProfileTestDeployments {
			Expect(verifyOperandTLSArgsMatchClusterProfile(dep, expectedSpec)).
				To(Succeed(), "deployment %s", dep)
		}
	})

	// ─────────────────────────────────────────────────────────────────────────
	// Scenario 6: LegacyAdheringComponentsOnly with a non-nil profile
	// cert-manager was never a legacy-adhering component → must still get no args.
	// ─────────────────────────────────────────────────────────────────────────
	It("S6: should not inject TLS args when tlsAdherence is LegacyAdheringComponentsOnly even with non-nil profile", func() {
		By("patching apiserver: Modern profile but LegacyAdheringComponentsOnly")
		Expect(updateClusterAPIServerTLSConfig(ctx, tlsProfileModernProfile,
			configapiv1.TLSAdherencePolicyLegacyAdheringComponentsOnly)).
			To(Succeed())

		By("verifying no TLS profile args appear on any operand")
		for _, dep := range tlsProfileTestDeployments {
			Expect(verifyDeploymentArgs(k8sClientSet, dep,
				[]string{"--tls-min-version=", "--metrics-tls-min-version="}, false)).
				To(Succeed(), "deployment %s must not have TLS profile args with LegacyAdheringComponentsOnly", dep)
		}
	})

	// ─────────────────────────────────────────────────────────────────────────
	// Scenario 7: Rollback StrictAllComponents → LegacyAdheringComponentsOnly
	// Previously injected TLS args must be removed from all operands.
	// ─────────────────────────────────────────────────────────────────────────
	It("S7: should remove TLS args when tlsAdherence rolls back from Strict to Legacy", func() {
		By("patching apiserver: Intermediate + StrictAllComponents (args appear)")
		Expect(updateClusterAPIServerTLSConfig(ctx, tlsProfileIntermediateProfile,
			configapiv1.TLSAdherencePolicyStrictAllComponents)).
			To(Succeed())

		By("waiting for TLS 1.2 args to be present (confirming Strict applied)")
		Expect(verifyDeploymentArgs(k8sClientSet, certmanagerWebhookDeployment,
			[]string{"--tls-min-version=VersionTLS12"}, true)).
			To(Succeed(), "webhook must have VersionTLS12 args before rollback")

		By("patching apiserver back to LegacyAdheringComponentsOnly")
		Expect(updateClusterAPIServerTLSConfig(ctx, tlsProfileIntermediateProfile,
			configapiv1.TLSAdherencePolicyLegacyAdheringComponentsOnly)).
			To(Succeed())

		By("verifying TLS args are removed from all operands after rollback")
		for _, dep := range tlsProfileTestDeployments {
			Expect(verifyDeploymentArgs(k8sClientSet, dep,
				[]string{"--tls-min-version=", "--metrics-tls-min-version="}, false)).
				To(Succeed(), "deployment %s must not have TLS args after rollback", dep)
		}
	})

	// ─────────────────────────────────────────────────────────────────────────
	// Scenario 8: unsupportedConfigOverrides (break-glass) wins over cluster TLS
	// Hook ordering: WithClusterTLSProfile → withUnsupportedArgsOverride (last = wins).
	// ─────────────────────────────────────────────────────────────────────────
	It("S8: unsupportedConfigOverrides should override cluster TLS args", func() {
		By("patching apiserver: Intermediate + StrictAllComponents (cluster enforces TLS 1.2)")
		Expect(updateClusterAPIServerTLSConfig(ctx, tlsProfileIntermediateProfile,
			configapiv1.TLSAdherencePolicyStrictAllComponents)).
			To(Succeed())
		Expect(verifyDeploymentArgs(k8sClientSet, certmanagerWebhookDeployment,
			[]string{"--tls-min-version=VersionTLS12"}, true)).
			To(Succeed(), "cluster TLS 1.2 must be applied before testing override")

		By("applying unsupportedConfigOverrides to force --tls-min-version=VersionTLS10 on webhook")
		Expect(setWebhookUnsupportedArgs(ctx, []string{"--tls-min-version=VersionTLS10"})).
			To(Succeed())

		By("verifying VersionTLS10 override wins over cluster VersionTLS12")
		Expect(verifyDeploymentArgs(k8sClientSet, certmanagerWebhookDeployment,
			[]string{"--tls-min-version=VersionTLS10"}, true)).
			To(Succeed(), "unsupportedConfigOverrides VersionTLS10 must win over cluster VersionTLS12")

		By("removing unsupportedConfigOverrides")
		Expect(setWebhookUnsupportedArgs(ctx, nil)).To(Succeed())

		By("verifying cluster TLS 1.2 restores after removing override")
		Expect(verifyDeploymentArgs(k8sClientSet, certmanagerWebhookDeployment,
			[]string{"--tls-min-version=VersionTLS12"}, true)).
			To(Succeed(), "cluster TLS 1.2 must restore after override is removed")
	})

	// ─────────────────────────────────────────────────────────────────────────
	// Scenario 9: Live profile switch Intermediate → Modern
	// Cipher suite args must disappear when switching to TLS 1.3.
	// Go's crypto/tls ignores CipherSuites at TLS 1.3; injecting them is misleading.
	// ─────────────────────────────────────────────────────────────────────────
	It("S9: should remove cipher suite args when switching from Intermediate to Modern profile", func() {
		By("patching apiserver: Intermediate + StrictAllComponents (cipher args appear)")
		Expect(updateClusterAPIServerTLSConfig(ctx, tlsProfileIntermediateProfile,
			configapiv1.TLSAdherencePolicyStrictAllComponents)).
			To(Succeed())
		Expect(verifyDeploymentArgs(k8sClientSet, certmanagerWebhookDeployment,
			[]string{"--tls-min-version=VersionTLS12", "--tls-cipher-suites="}, true)).
			To(Succeed(), "webhook must have TLS 1.2 + cipher args before profile switch")

		By("patching apiserver: Modern + StrictAllComponents (cipher args must disappear)")
		Expect(updateClusterAPIServerTLSConfig(ctx, tlsProfileModernProfile,
			configapiv1.TLSAdherencePolicyStrictAllComponents)).
			To(Succeed())

		By("verifying webhook updated to VersionTLS13")
		Expect(verifyDeploymentArgs(k8sClientSet, certmanagerWebhookDeployment,
			[]string{"--tls-min-version=VersionTLS13"}, true)).
			To(Succeed(), "webhook must update to VersionTLS13 after profile switch")

		By("verifying cipher suite args are absent after switch to TLS 1.3")
		Expect(verifyDeploymentArgs(k8sClientSet, certmanagerWebhookDeployment,
			tlsprofile.CertManagerCipherSuiteArgKeys, false)).
			To(Succeed(), "cipher suite args must be removed after switching to Modern/TLS 1.3")

		By("verifying all operands match the Modern profile")
		expectedSpec, err := tlsprofile.EffectiveSpec(tlsProfileModernProfile)
		Expect(err).NotTo(HaveOccurred())
		for _, dep := range tlsProfileTestDeployments {
			Expect(verifyOperandTLSArgsMatchClusterProfile(dep, expectedSpec)).
				To(Succeed(), "deployment %s", dep)
		}
	})
})

// setWebhookUnsupportedArgs sets (or clears) spec.unsupportedConfigOverrides.webhook.args
// on the certmanager/cluster CR. Passing nil clears the overrides.
func setWebhookUnsupportedArgs(ctx context.Context, args []string) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cm, err := certmanageroperatorclient.OperatorV1alpha1().CertManagers().
			Get(ctx, "cluster", metav1.GetOptions{})
		if err != nil {
			return err
		}

		updated := cm.DeepCopy()
		if args == nil {
			// Clear the whole unsupportedConfigOverrides field
			updated.Spec.OperatorSpec.UnsupportedConfigOverrides = runtime.RawExtension{}
		} else {
			raw, err := json.Marshal(&v1alpha1.UnsupportedConfigOverrides{
				Webhook: v1alpha1.UnsupportedConfigOverridesForCertManagerWebhook{Args: args},
			})
			if err != nil {
				return fmt.Errorf("marshal unsupportedConfigOverrides: %w", err)
			}
			updated.Spec.OperatorSpec.UnsupportedConfigOverrides = runtime.RawExtension{Raw: raw}
		}

		_, err = certmanageroperatorclient.OperatorV1alpha1().CertManagers().
			Update(ctx, updated, metav1.UpdateOptions{})
		return err
	})
}

