//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	configapiv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/tlsprofile"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var csvSchema = schema.GroupVersionResource{
	Group:    "operators.coreos.com",
	Version:  "v1alpha1",
	Resource: "clusterserviceversions",
}

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
				"ECDHE-RSA-AES128-GCM-SHA256", // HTTP/2 required
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
		Expect(err).NotTo(HaveOccurred(), "failed to probe tlsAdherence field availability on apiserver/cluster")
	})

	AfterAll(func() {
		By("restoring original apiserver TLS configuration")
		Eventually(func() error {
			return restoreClusterAPIServerTLSConfig(ctx, original)
		}, lowTimeout, fastPollInterval).Should(Succeed())

		By("clearing unsupportedConfigOverrides left by any test scenarios")
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
		Expect(err).NotTo(HaveOccurred(), "failed to resolve effective TLS spec for Modern profile")

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
		Expect(err).NotTo(HaveOccurred(), "failed to resolve effective TLS spec for Intermediate profile")

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
		Expect(err).NotTo(HaveOccurred(), "failed to resolve effective TLS spec for Old profile")

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
		Expect(err).NotTo(HaveOccurred(), "failed to resolve effective TLS spec for Custom profile")

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

		expectedIntermediate, err := tlsprofile.EffectiveSpec(tlsProfileIntermediateProfile)
		Expect(err).NotTo(HaveOccurred(), "failed to resolve effective TLS spec for Intermediate profile in S9")
		// verifyOperandTLSArgsMatchClusterProfile waits for the full --tls-cipher-suites=<IANA list>
		// argument. verifyDeploymentArgs cannot be used with a "--tls-cipher-suites=" prefix here
		// because it requires exact set membership.
		Expect(verifyOperandTLSArgsMatchClusterProfile(certmanagerWebhookDeployment, expectedIntermediate)).
			To(Succeed(), "webhook must match Intermediate profile before switch")

		By("patching apiserver: Modern + StrictAllComponents (cipher args must disappear)")
		Expect(updateClusterAPIServerTLSConfig(ctx, tlsProfileModernProfile,
			configapiv1.TLSAdherencePolicyStrictAllComponents)).
			To(Succeed())

		By("verifying all operands match Modern and cipher suite args are gone")
		expectedModern, err := tlsprofile.EffectiveSpec(tlsProfileModernProfile)
		Expect(err).NotTo(HaveOccurred(), "failed to resolve effective TLS spec for Modern profile in S9")
		for _, dep := range tlsProfileTestDeployments {
			Expect(verifyOperandTLSArgsMatchClusterProfile(dep, expectedModern)).
				To(Succeed(), "deployment %s must match Modern profile after switch", dep)
		}
	})

	It("should enable HTTPS metrics dynamic serving on cert-manager operands", func() {
		By("verifying metrics-dynamic-serving args and prometheus.io/scheme=https annotation")
		for _, name := range []string{
			certmanagerControllerDeployment,
			certmanagerWebhookDeployment,
			certmanagerCAinjectorDeployment,
		} {
			err := verifyOperandMetricsHTTPS(name)
			Expect(err).NotTo(HaveOccurred(), "deployment %s", name)
		}
	})

	It("should reject TLS 1.2 and accept TLS 1.3 on metrics and webhook listeners under Modern", func() {
		original := requireAPIServerTLSConfig(ctx)
		DeferCleanup(restoreAPIServerTLSConfigCleanup(ctx, original))

		modernSpec := patchStrictTLSProfile(ctx, &configapiv1.TLSSecurityProfile{
			Type: configapiv1.TLSProfileModernType,
		})
		expectCertManagerListenersMatchProfile(ctx, modernSpec)

		By("handshaking metrics and webhook sockets: TLS 1.3 succeeds, TLS 1.2 fails")
		assertModernListenerEnforcement(ctx)
	})

	It("should enforce Intermediate TLS 1.2 ciphers on metrics and webhook listeners", func() {
		original := requireAPIServerTLSConfig(ctx)
		DeferCleanup(restoreAPIServerTLSConfigCleanup(ctx, original))

		intermediateSpec := patchStrictTLSProfile(ctx, &configapiv1.TLSSecurityProfile{
			Type: configapiv1.TLSProfileIntermediateType,
		})
		expectCertManagerListenersMatchProfile(ctx, intermediateSpec)

		By("handshaking metrics and webhook sockets: TLS 1.2 + allowed ECDSA GCM succeeds; TLS 1.0 and CBC fail")
		assertIntermediateListenerEnforcement(ctx)
	})

	It("should apply Custom TLS 1.3 to cert-manager operand listeners", func() {
		original := requireAPIServerTLSConfig(ctx)
		DeferCleanup(restoreAPIServerTLSConfigCleanup(ctx, original))

		customSpec := patchStrictTLSProfile(ctx, customTLS13SecurityProfile())
		expectCertManagerListenersMatchProfile(ctx, customSpec)

		By("handshaking metrics and webhook sockets: Custom TLS 1.3 succeeds, TLS 1.2 fails")
		assertModernListenerEnforcement(ctx)
	})

	It("should claim tls-profiles feature on the operator CSV when installed via OLM", func() {
		installed, err := certManagerOperatorSubscriptionInstalled(ctx, loader)
		Expect(err).NotTo(HaveOccurred())
		if !installed {
			Skip("no OLM Subscription; CSV annotation check not applicable")
		}

		By("listing ClusterServiceVersions in the operator namespace")
		csvClient := loader.DynamicClient.Resource(csvSchema).Namespace(operatorNamespace)
		csvs, err := csvClient.List(ctx, metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(csvs.Items).NotTo(BeEmpty(), "expected at least one CSV in %s", operatorNamespace)

		found := false
		for _, csv := range csvs.Items {
			name := csv.GetName()
			if !strings.Contains(name, "cert-manager-operator") {
				continue
			}
			annotations := csv.GetAnnotations()
			Expect(annotations).To(HaveKeyWithValue("features.operators.openshift.io/tls-profiles", "true"),
				"CSV %s missing tls-profiles feature annotation", name)
			found = true
			break
		}
		Expect(found).To(BeTrue(), "no cert-manager-operator CSV found in %s", operatorNamespace)
	})

	// E2E-012 — Strict Intermediate → Legacy: every profile-managed TLS flag is gone;
	// leftover Intermediate cipher flags must not remain; live sockets accept TLS 1.2 again
	// after a prior Strict Modern restriction.
	It("should strip every profile-managed TLS flag and restore TLS 1.2 on Strict to Legacy", func() {
		original := requireAPIServerTLSConfig(ctx)
		DeferCleanup(restoreAPIServerTLSConfigCleanup(ctx, original))

		operandDeployments := []string{
			certmanagerControllerDeployment,
			certmanagerWebhookDeployment,
			certmanagerCAinjectorDeployment,
		}

		By("applying Strict Modern so listeners reject TLS 1.2")
		modernSpec := patchStrictTLSProfile(ctx, &configapiv1.TLSSecurityProfile{
			Type: configapiv1.TLSProfileModernType,
		})
		expectCertManagerListenersMatchProfile(ctx, modernSpec)
		assertModernListenerEnforcement(ctx)

		By("applying Strict Intermediate so leftover cipher flags would exist if Legacy only dropped Modern strings")
		intermediateSpec := patchStrictTLSProfile(ctx, &configapiv1.TLSSecurityProfile{
			Type: configapiv1.TLSProfileIntermediateType,
		})
		expectCertManagerListenersMatchProfile(ctx, intermediateSpec)

		By("patching apiserver to LegacyAdheringComponentsOnly with Intermediate profile still set")
		patchLegacyTLSProfile(ctx, &configapiv1.TLSSecurityProfile{
			Type: configapiv1.TLSProfileIntermediateType,
		})

		By("waiting for every profile-managed TLS flag key to be absent")
		expectProfileManagedTLSFlagsGone(ctx, operandDeployments)

		By("handshaking sockets: TLS 1.2 succeeds after Strict → Legacy (no longer TLS 1.3-only)")
		assertTLS12Accepted(ctx, certManagerTLSEndpoints())
	})

	// E2E-010 — Day-2 Modern → Intermediate on cert-manager operands.
	It("should update cert-manager operand TLS args when apiserver profile changes Modern to Intermediate", func() {
		original := requireAPIServerTLSConfig(ctx)
		DeferCleanup(restoreAPIServerTLSConfigCleanup(ctx, original))

		modernSpec := patchStrictTLSProfile(ctx, &configapiv1.TLSSecurityProfile{
			Type: configapiv1.TLSProfileModernType,
		})

		operandDeployments := []string{
			certmanagerControllerDeployment,
			certmanagerWebhookDeployment,
			certmanagerCAinjectorDeployment,
		}

		By("verifying cert-manager operands match Modern EffectiveSpec")
		for _, name := range operandDeployments {
			err := verifyOperandTLSArgsMatchClusterProfile(name, modernSpec)
			Expect(err).NotTo(HaveOccurred(), "modern profile args on %s", name)
		}

		By("patching apiserver cluster profile to Intermediate")
		intermediateSpec := patchStrictTLSProfile(ctx, &configapiv1.TLSSecurityProfile{
			Type: configapiv1.TLSProfileIntermediateType,
		})

		By("verifying cert-manager operands converge to Intermediate EffectiveSpec")
		for _, name := range operandDeployments {
			err := verifyOperandTLSArgsMatchClusterProfile(name, intermediateSpec)
			Expect(err).NotTo(HaveOccurred(), "intermediate profile args on %s", name)
		}
	})

	// The operator metrics listener is fixed at process start, so a profile
	// change and an adherence change each exit the container. Kubelet restarts
	// it in place: the Pod UID stays the same and only the container ID changes.
	It("should restart the operator container when the cluster TLS profile changes", func() {
		original := requireAPIServerTLSConfig(ctx)
		DeferCleanup(restoreAPIServerTLSConfigCleanup(ctx, original))

		baseline := ensureStrictTLSProfileSettled(ctx, &configapiv1.TLSSecurityProfile{
			Type: configapiv1.TLSProfileModernType,
		})

		By("patching apiserver cluster profile from Modern to Intermediate")
		patchStrictTLSProfile(ctx, &configapiv1.TLSSecurityProfile{
			Type: configapiv1.TLSProfileIntermediateType,
		})
		expectOperatorContainerRestart(ctx, baseline.RestartKey)
	})

	It("should restart the operator container when tlsAdherence changes and the profile stays the same", func() {
		original := requireAPIServerTLSConfig(ctx)
		DeferCleanup(restoreAPIServerTLSConfigCleanup(ctx, original))

		baseline := ensureStrictTLSProfileSettled(ctx, &configapiv1.TLSSecurityProfile{
			Type: configapiv1.TLSProfileIntermediateType,
		})

		By("patching apiserver to LegacyAdheringComponentsOnly with the Intermediate profile unchanged")
		patchLegacyTLSProfile(ctx, &configapiv1.TLSSecurityProfile{
			Type: configapiv1.TLSProfileIntermediateType,
		})
		expectOperatorContainerRestart(ctx, baseline.RestartKey)
	})

	It("should not restart the operator container when an unrelated apiserver field changes", func() {
		pod := expectReadyOperatorContainer(ctx)
		apiServer, err := configClient.APIServers().Get(ctx, "cluster", metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred(), "read apiserver audit profile")

		originalAudit := apiServer.Spec.Audit.Profile
		next := configapiv1.WriteRequestBodiesAuditProfileType
		if originalAudit == next {
			next = configapiv1.DefaultAuditProfileType
		}
		By(fmt.Sprintf("patching apiserver audit profile from %q to %q", originalAudit, next))
		Expect(patchAPIServerAuditProfile(ctx, next)).To(Succeed())
		DeferCleanup(func() {
			Expect(patchAPIServerAuditProfile(context.Background(), originalAudit)).To(Succeed())
		})

		Consistently(func() (string, error) {
			current, err := readyOperatorContainer(ctx)
			if err != nil {
				return "", err
			}
			return current.RestartKey, nil
		}, 15*time.Second, fastPollInterval).Should(Equal(pod.RestartKey))
	})

	Context("trust-manager webhook TLS", Ordered, func() {
		var (
			tmCtx                            = context.Background()
			clientset                        *kubernetes.Clientset
			originalUnsupportedAddonFeatures string
			originalOperatorLogLevel         string
		)

		BeforeAll(trustManagerBeforeAll(tmCtx, &clientset, &originalUnsupportedAddonFeatures, &originalOperatorLogLevel))
		AfterAll(trustManagerAfterAll(tmCtx, &originalUnsupportedAddonFeatures, &originalOperatorLogLevel))
		AfterEach(trustManagerAfterEach(tmCtx))

		// Journey 1 — E2E-001
		It("should configure trust-manager webhook TLS args from StrictAllComponents Modern profile", func() {
			original := requireAPIServerTLSConfig(tmCtx)
			DeferCleanup(restoreAPIServerTLSConfigCleanup(tmCtx, original))

			createTrustManager(tmCtx, newTrustManagerCR())
			expectTrustManagerDeploymentPresent(tmCtx)

			expectedSpec := patchStrictTLSProfile(tmCtx, &configapiv1.TLSSecurityProfile{
				Type: configapiv1.TLSProfileModernType,
			})

			By("verifying trust-manager webhook TLS flags match Modern EffectiveSpec")
			err := verifyOperandTLSArgsMatchClusterProfile(trustManagerDeploymentName, expectedSpec)
			Expect(err).NotTo(HaveOccurred(), "deployment %s", trustManagerDeploymentName)

			By("verifying trust-manager deployment is Available after TLS reconcile")
			err = waitForDeploymentRollout(tmCtx, operandNamespace, trustManagerDeploymentName, lowTimeout)
			Expect(err).NotTo(HaveOccurred())
		})

		// Journey 2 — E2E-002
		It("should update trust-manager TLS args when apiserver profile changes Modern to Intermediate", func() {
			original := requireAPIServerTLSConfig(tmCtx)
			DeferCleanup(restoreAPIServerTLSConfigCleanup(tmCtx, original))

			createTrustManager(tmCtx, newTrustManagerCR())
			expectTrustManagerDeploymentPresent(tmCtx)

			modernSpec := patchStrictTLSProfile(tmCtx, &configapiv1.TLSSecurityProfile{
				Type: configapiv1.TLSProfileModernType,
			})
			err := verifyOperandTLSArgsMatchClusterProfile(trustManagerDeploymentName, modernSpec)
			Expect(err).NotTo(HaveOccurred(), "modern profile args")

			By("patching apiserver cluster profile to Intermediate")
			intermediateSpec := patchStrictTLSProfile(tmCtx, &configapiv1.TLSSecurityProfile{
				Type: configapiv1.TLSProfileIntermediateType,
			})

			By("verifying trust-manager args converge to Intermediate EffectiveSpec")
			err = verifyOperandTLSArgsMatchClusterProfile(trustManagerDeploymentName, intermediateSpec)
			Expect(err).NotTo(HaveOccurred(), "intermediate profile args")
		})

		// E2E-011 — Intermediate → Modern strips cipher flags on cert-manager operands + trust-manager.
		It("should strip TLS cipher flags when apiserver profile changes Intermediate to Modern", func() {
			original := requireAPIServerTLSConfig(tmCtx)
			DeferCleanup(restoreAPIServerTLSConfigCleanup(tmCtx, original))

			createTrustManager(tmCtx, newTrustManagerCR())
			expectTrustManagerDeploymentPresent(tmCtx)

			deployments := []string{
				certmanagerControllerDeployment,
				certmanagerWebhookDeployment,
				certmanagerCAinjectorDeployment,
				trustManagerDeploymentName,
			}

			By("applying Strict Intermediate and confirming cipher-bearing TLS args")
			intermediateSpec := patchStrictTLSProfile(tmCtx, &configapiv1.TLSSecurityProfile{
				Type: configapiv1.TLSProfileIntermediateType,
			})
			for _, name := range deployments {
				err := verifyOperandTLSArgsMatchClusterProfile(name, intermediateSpec)
				Expect(err).NotTo(HaveOccurred(), "intermediate profile args on %s", name)
			}

			By("patching apiserver cluster profile to Modern (TLS1.3)")
			modernSpec := patchStrictTLSProfile(tmCtx, &configapiv1.TLSSecurityProfile{
				Type: configapiv1.TLSProfileModernType,
			})

			By("verifying deployments converge to Modern min-version args with cipher flags stripped")
			for _, name := range deployments {
				err := verifyOperandTLSArgsMatchClusterProfile(name, modernSpec)
				Expect(err).NotTo(HaveOccurred(), "modern profile args / cipher strip on %s", name)
			}
		})

		// Journey 3 — E2E-003
		It("should apply Custom TLS profile flags to trust-manager", func() {
			original := requireAPIServerTLSConfig(tmCtx)
			DeferCleanup(restoreAPIServerTLSConfigCleanup(tmCtx, original))

			createTrustManager(tmCtx, newTrustManagerCR())
			expectTrustManagerDeploymentPresent(tmCtx)

			customProfile := &configapiv1.TLSSecurityProfile{
				Type: configapiv1.TLSProfileCustomType,
				Custom: &configapiv1.CustomTLSProfile{
					TLSProfileSpec: configapiv1.TLSProfileSpec{
						MinTLSVersion: configapiv1.VersionTLS12,
						Ciphers: []string{
							"ECDHE-ECDSA-AES128-GCM-SHA256",
							"ECDHE-RSA-AES128-GCM-SHA256",
						},
					},
				},
			}
			expectedSpec := patchStrictTLSProfile(tmCtx, customProfile)

			By("verifying trust-manager args match Custom EffectiveSpec")
			err := verifyOperandTLSArgsMatchClusterProfile(trustManagerDeploymentName, expectedSpec)
			Expect(err).NotTo(HaveOccurred(), "custom profile args")
		})

		It("should apply Custom TLS 1.3 flags to trust-manager and reject TLS 1.2 on the webhook", func() {
			original := requireAPIServerTLSConfig(tmCtx)
			DeferCleanup(restoreAPIServerTLSConfigCleanup(tmCtx, original))

			createTrustManager(tmCtx, newTrustManagerCR())
			expectTrustManagerDeploymentPresent(tmCtx)

			customSpec := patchStrictTLSProfile(tmCtx, customTLS13SecurityProfile())
			By("verifying trust-manager args match Custom TLS 1.3 EffectiveSpec without cipher flags")
			err := verifyOperandTLSArgsMatchClusterProfile(trustManagerDeploymentName, customSpec)
			Expect(err).NotTo(HaveOccurred(), "custom tls13 profile args")
			err = waitForDeploymentRollout(tmCtx, operandNamespace, trustManagerDeploymentName, lowTimeout)
			Expect(err).NotTo(HaveOccurred())
			expectTrustManagerTLSSecret(tmCtx)

			By("handshaking trust-manager webhook: TLS 1.3 succeeds, TLS 1.2 fails")
			assertModernListenerEnforcementOn(tmCtx, []certManagerTLSEndpoint{trustManagerWebhookEndpoint()})
		})

		// Journey 4 — E2E-004
		It("should keep trust-manager Certificate Ready after Modern TLS profile is applied", func() {
			original := requireAPIServerTLSConfig(tmCtx)
			DeferCleanup(restoreAPIServerTLSConfigCleanup(tmCtx, original))

			createTrustManager(tmCtx, newTrustManagerCR())
			expectTrustManagerDeploymentPresent(tmCtx)

			By("waiting for trust-manager Certificate to become ready before TLS patch")
			err := waitForCertificateReadiness(tmCtx, trustManagerCertificateName, trustManagerNamespace)
			Expect(err).NotTo(HaveOccurred())

			expectedSpec := patchStrictTLSProfile(tmCtx, &configapiv1.TLSSecurityProfile{
				Type: configapiv1.TLSProfileModernType,
			})
			err = verifyOperandTLSArgsMatchClusterProfile(trustManagerDeploymentName, expectedSpec)
			Expect(err).NotTo(HaveOccurred())

			By("re-checking Certificate readiness and TLS secret after profile application")
			err = waitForCertificateReadiness(tmCtx, trustManagerCertificateName, trustManagerNamespace)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				secret, err := k8sClientSet.CoreV1().Secrets(trustManagerNamespace).Get(tmCtx, trustManagerTLSSecretName, metav1.GetOptions{})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(secret.Data).To(HaveKey("tls.crt"))
				g.Expect(secret.Data).To(HaveKey("tls.key"))
			}, lowTimeout, fastPollInterval).Should(Succeed())
		})

		// Journey 5 — NEG-001
		It("should not apply Modern TLS flags to trust-manager under LegacyAdheringComponentsOnly", func() {
			original := requireAPIServerTLSConfig(tmCtx)
			DeferCleanup(restoreAPIServerTLSConfigCleanup(tmCtx, original))

			modernProfile := &configapiv1.TLSSecurityProfile{
				Type: configapiv1.TLSProfileModernType,
			}
			By("patching apiserver to LegacyAdheringComponentsOnly with Modern profile before TrustManager exists")
			err := updateClusterAPIServerTLSConfig(tmCtx, modernProfile, configapiv1.TLSAdherencePolicyLegacyAdheringComponentsOnly)
			if isTLSAdherenceUnsupported(err) {
				Skip(fmt.Sprintf("apiserver tlsAdherence is not available on this cluster: %v", err))
			}
			Expect(err).NotTo(HaveOccurred())

			createTrustManager(tmCtx, newTrustManagerCR())
			expectTrustManagerDeploymentPresent(tmCtx)

			modernSpec, err := tlsprofile.EffectiveSpec(modernProfile)
			Expect(err).NotTo(HaveOccurred())
			unexpected := tlsprofile.TrustManagerWebhookTLSArgs(modernSpec)
			Expect(unexpected).NotTo(BeEmpty())

			By("consistently verifying Modern Strict TLS args are absent on trust-manager")
			Consistently(func() error {
				return verifyOperandTLSArgsNotPresent(trustManagerDeploymentName, unexpected)
			}, 15*time.Second, fastPollInterval).Should(Succeed())
		})

		It("should strip every trust-manager profile TLS flag on Strict Intermediate to Legacy", func() {
			original := requireAPIServerTLSConfig(tmCtx)
			DeferCleanup(restoreAPIServerTLSConfigCleanup(tmCtx, original))

			createTrustManager(tmCtx, newTrustManagerCR())
			expectTrustManagerDeploymentPresent(tmCtx)

			By("applying Strict Intermediate so leftover cipher flags would exist")
			intermediateSpec := patchStrictTLSProfile(tmCtx, &configapiv1.TLSSecurityProfile{
				Type: configapiv1.TLSProfileIntermediateType,
			})
			err := verifyOperandTLSArgsMatchClusterProfile(trustManagerDeploymentName, intermediateSpec)
			Expect(err).NotTo(HaveOccurred(), "intermediate profile args")
			err = waitForDeploymentRollout(tmCtx, operandNamespace, trustManagerDeploymentName, lowTimeout)
			Expect(err).NotTo(HaveOccurred())
			expectTrustManagerTLSSecret(tmCtx)

			By("patching apiserver to LegacyAdheringComponentsOnly")
			patchLegacyTLSProfile(tmCtx, &configapiv1.TLSSecurityProfile{
				Type: configapiv1.TLSProfileIntermediateType,
			})

			expectProfileManagedTLSFlagsGone(tmCtx, []string{trustManagerDeploymentName})

			By("handshaking trust-manager webhook: TLS 1.2 succeeds after Strict → Legacy")
			assertTLS12Accepted(tmCtx, []certManagerTLSEndpoint{trustManagerWebhookEndpoint()})
		})

		// Journey 5 — NEG-002
		It("should retain trust-manager TLS args after operator pod restart under Strict Modern", func() {
			original := requireAPIServerTLSConfig(tmCtx)
			DeferCleanup(restoreAPIServerTLSConfigCleanup(tmCtx, original))

			createTrustManager(tmCtx, newTrustManagerCR())
			expectTrustManagerDeploymentPresent(tmCtx)

			expectedSpec := patchStrictTLSProfile(tmCtx, &configapiv1.TLSSecurityProfile{
				Type: configapiv1.TLSProfileModernType,
			})
			err := verifyOperandTLSArgsMatchClusterProfile(trustManagerDeploymentName, expectedSpec)
			Expect(err).NotTo(HaveOccurred())

			By("deleting operator controller-manager pods")
			err = deleteOperatorControllerPods(tmCtx)
			Expect(err).NotTo(HaveOccurred())

			By("waiting for operator to become healthy again")
			err = verifyHealthyOperatorConditionsWithTimeout(certmanageroperatorclient.OperatorV1alpha1(), highTimeout)
			Expect(err).NotTo(HaveOccurred())

			By("verifying trust-manager TLS args still match Modern EffectiveSpec")
			err = verifyOperandTLSArgsMatchClusterProfile(trustManagerDeploymentName, expectedSpec)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})

// requireAPIServerTLSConfig reads the cluster APIServer TLS configuration or skips the test.
func requireAPIServerTLSConfig(ctx context.Context) *apiserverTLSConfig {
	GinkgoHelper()
	original, err := getClusterAPIServerTLSConfig(ctx)
	if apierrors.IsNotFound(err) {
		Skip("apiserver.config.openshift.io/cluster is not available on this cluster")
	}
	Expect(err).NotTo(HaveOccurred(), "failed to read apiserver TLS configuration")
	return original
}

// restoreAPIServerTLSConfigCleanup returns a function that restores the saved APIServer TLS configuration.
func restoreAPIServerTLSConfigCleanup(ctx context.Context, original *apiserverTLSConfig) func() {
	return func() {
		By("[cleanup] restoring original apiserver TLS configuration")
		Eventually(func() error {
			return restoreClusterAPIServerTLSConfig(ctx, original)
		}, lowTimeout, fastPollInterval).Should(Succeed())
	}
}

// expectCertManagerListenersMatchProfile waits until cert-manager TLS args match spec and the deployments roll out.
func expectCertManagerListenersMatchProfile(ctx context.Context, spec *configapiv1.TLSProfileSpec) {
	GinkgoHelper()
	operandDeployments := []string{
		certmanagerControllerDeployment,
		certmanagerWebhookDeployment,
		certmanagerCAinjectorDeployment,
	}
	By("waiting for cert-manager operand TLS args and rollout")
	for _, name := range operandDeployments {
		err := verifyOperandTLSArgsMatchClusterProfile(name, spec)
		Expect(err).NotTo(HaveOccurred(), "deployment %s", name)
		err = waitForDeploymentRollout(ctx, operandNamespace, name, lowTimeout)
		Expect(err).NotTo(HaveOccurred(), "rollout %s", name)
		err = verifyOperandMetricsHTTPS(name)
		Expect(err).NotTo(HaveOccurred(), "https metrics %s", name)
	}
	Eventually(func() error {
		_, err := k8sClientSet.CoreV1().Secrets(operandNamespace).Get(ctx, metricsCASecretName, metav1.GetOptions{})
		return err
	}, lowTimeout, fastPollInterval).Should(Succeed(), "metrics CA secret")
	Eventually(func() error {
		_, err := k8sClientSet.CoreV1().Secrets(operandNamespace).Get(ctx, webhookCASecretName, metav1.GetOptions{})
		return err
	}, lowTimeout, fastPollInterval).Should(Succeed(), "webhook CA secret")
}

// assertModernListenerEnforcement checks that cert-manager listeners accept TLS 1.3 and reject TLS 1.2.
func assertModernListenerEnforcement(ctx context.Context) {
	GinkgoHelper()
	assertModernListenerEnforcementOn(ctx, certManagerTLSEndpoints())
}

// assertModernListenerEnforcementOn checks that the given listeners accept TLS 1.3 and reject TLS 1.2.
func assertModernListenerEnforcementOn(ctx context.Context, endpoints []certManagerTLSEndpoint) {
	GinkgoHelper()
	for _, ep := range endpoints {
		Eventually(func() error {
			return handshakeOperandPort(ctx, ep.deployment, ep.serverName, ep.caSecret, ep.port, tls13Only())
		}, lowTimeout, fastPollInterval).Should(Succeed(), "%s TLS 1.3", ep.name)

		err := handshakeOperandPort(ctx, ep.deployment, ep.serverName, ep.caSecret, ep.port, tls12Only())
		Expect(err).To(HaveOccurred(), "%s TLS 1.2 should be rejected under TLS 1.3 min version", ep.name)
	}
}

// assertIntermediateListenerEnforcement checks Intermediate cipher and version limits on cert-manager listeners.
func assertIntermediateListenerEnforcement(ctx context.Context) {
	GinkgoHelper()
	allowed := tls12Cipher(tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256)
	rejected := tls12Cipher(tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256)
	for _, ep := range certManagerTLSEndpoints() {
		Eventually(func() error {
			return handshakeOperandPort(ctx, ep.deployment, ep.serverName, ep.caSecret, ep.port, allowed)
		}, lowTimeout, fastPollInterval).Should(Succeed(), "%s TLS 1.2 allowed cipher", ep.name)

		err := handshakeOperandPort(ctx, ep.deployment, ep.serverName, ep.caSecret, ep.port, rejected)
		Expect(err).To(HaveOccurred(), "%s TLS 1.2 CBC cipher should be rejected under Intermediate", ep.name)

		err = handshakeOperandPort(ctx, ep.deployment, ep.serverName, ep.caSecret, ep.port, tls10Only())
		Expect(err).To(HaveOccurred(), "%s TLS 1.0 should be rejected under Intermediate", ep.name)
	}
}

type certManagerTLSEndpoint struct {
	name       string
	deployment string
	serverName string
	caSecret   string
	port       int
}

// certManagerTLSEndpoints lists the cert-manager metrics and webhook ports to probe.
func certManagerTLSEndpoints() []certManagerTLSEndpoint {
	return []certManagerTLSEndpoint{
		{
			name:       "controller-metrics",
			deployment: certmanagerControllerDeployment,
			serverName: metricsServerName("cert-manager"),
			caSecret:   metricsCASecretName,
			port:       metricsPort,
		},
		{
			name:       "webhook-metrics",
			deployment: certmanagerWebhookDeployment,
			serverName: metricsServerName("cert-manager-webhook"),
			caSecret:   metricsCASecretName,
			port:       metricsPort,
		},
		{
			name:       "cainjector-metrics",
			deployment: certmanagerCAinjectorDeployment,
			serverName: metricsServerName("cert-manager-cainjector"),
			caSecret:   metricsCASecretName,
			port:       metricsPort,
		},
		{
			name:       "webhook-serving",
			deployment: certmanagerWebhookDeployment,
			serverName: metricsServerName("cert-manager-webhook"),
			caSecret:   webhookCASecretName,
			port:       webhookSecurePort,
		},
	}
}

// trustManagerWebhookEndpoint returns the trust-manager webhook port to probe.
func trustManagerWebhookEndpoint() certManagerTLSEndpoint {
	return certManagerTLSEndpoint{
		name:       "trust-manager-webhook",
		deployment: trustManagerDeploymentName,
		serverName: metricsServerName(trustManagerServiceName),
		caSecret:   trustManagerTLSSecretName,
		port:       trustManagerWebhookPort,
	}
}

// customTLS13SecurityProfile returns a Custom profile whose minimum version is TLS 1.3.
func customTLS13SecurityProfile() *configapiv1.TLSSecurityProfile {
	return &configapiv1.TLSSecurityProfile{
		Type: configapiv1.TLSProfileCustomType,
		Custom: &configapiv1.CustomTLSProfile{
			TLSProfileSpec: configapiv1.TLSProfileSpec{
				MinTLSVersion: configapiv1.VersionTLS13,
				Ciphers:       []string{"TLS_AES_128_GCM_SHA256"},
			},
		},
	}
}

// patchLegacyTLSProfile sets tlsAdherence to LegacyAdheringComponentsOnly with the given profile.
func patchLegacyTLSProfile(ctx context.Context, profile *configapiv1.TLSSecurityProfile) {
	GinkgoHelper()
	By(fmt.Sprintf("patching apiserver cluster to LegacyAdheringComponentsOnly with %s TLS profile", profile.Type))
	err := updateClusterAPIServerTLSConfig(ctx, profile, configapiv1.TLSAdherencePolicyLegacyAdheringComponentsOnly)
	if isTLSAdherenceUnsupported(err) {
		Skip(fmt.Sprintf("apiserver tlsAdherence is not available on this cluster: %v", err))
	}
	Expect(err).NotTo(HaveOccurred(), "failed to patch apiserver TLS configuration")
}

// expectProfileManagedTLSFlagsGone waits until the deployments drop cluster-profile TLS flags.
func expectProfileManagedTLSFlagsGone(ctx context.Context, deployments []string) {
	GinkgoHelper()
	for _, name := range deployments {
		err := waitForOperandProfileTLSArgKeysAbsent(name)
		Expect(err).NotTo(HaveOccurred(), "deployment %s still has profile-managed TLS flags under Legacy", name)

		Consistently(func() error {
			return verifyOperandProfileTLSArgKeysAbsent(name)
		}, 15*time.Second, fastPollInterval).Should(Succeed(), "deployment %s", name)

		if name != trustManagerDeploymentName {
			Eventually(func() error {
				return verifyOperandMetricsHTTPS(name)
			}, lowTimeout, fastPollInterval).Should(Succeed(), "https metrics remain on %s", name)
		}

		err = waitForDeploymentRollout(ctx, operandNamespace, name, lowTimeout)
		Expect(err).NotTo(HaveOccurred(), "rollout %s", name)
	}
}

// assertTLS12Accepted checks that each listener accepts a TLS 1.2 handshake.
func assertTLS12Accepted(ctx context.Context, endpoints []certManagerTLSEndpoint) {
	GinkgoHelper()
	for _, ep := range endpoints {
		Eventually(func() error {
			return handshakeOperandPort(ctx, ep.deployment, ep.serverName, ep.caSecret, ep.port, tls12Only())
		}, lowTimeout, fastPollInterval).Should(Succeed(), "%s TLS 1.2 after Legacy", ep.name)
	}
}

// expectTrustManagerTLSSecret waits until the trust-manager TLS secret exists.
func expectTrustManagerTLSSecret(ctx context.Context) {
	GinkgoHelper()
	Eventually(func() error {
		_, err := k8sClientSet.CoreV1().Secrets(operandNamespace).Get(ctx, trustManagerTLSSecretName, metav1.GetOptions{})
		return err
	}, lowTimeout, fastPollInterval).Should(Succeed(), "trust-manager TLS secret")
}

// patchStrictTLSProfile sets tlsAdherence to StrictAllComponents and returns the resolved profile spec.
func patchStrictTLSProfile(ctx context.Context, profile *configapiv1.TLSSecurityProfile) *configapiv1.TLSProfileSpec {
	GinkgoHelper()
	By(fmt.Sprintf("patching apiserver cluster to StrictAllComponents with %s TLS profile", profile.Type))
	err := updateClusterAPIServerTLSConfig(ctx, profile, configapiv1.TLSAdherencePolicyStrictAllComponents)
	if isTLSAdherenceUnsupported(err) {
		Skip(fmt.Sprintf("apiserver tlsAdherence is not available on this cluster: %v", err))
	}
	Expect(err).NotTo(HaveOccurred(), "failed to patch apiserver TLS configuration")

	expectedSpec, err := tlsprofile.EffectiveSpec(profile)
	Expect(err).NotTo(HaveOccurred(), "failed to resolve expected TLS profile spec")
	return expectedSpec
}

// expectTrustManagerDeploymentPresent waits until the trust-manager deployment exists.
func expectTrustManagerDeploymentPresent(ctx context.Context) {
	GinkgoHelper()
	By("waiting for trust-manager deployment to exist")
	Eventually(func() error {
		_, err := k8sClientSet.AppsV1().Deployments(operandNamespace).Get(ctx, trustManagerDeploymentName, metav1.GetOptions{})
		return err
	}, lowTimeout, fastPollInterval).Should(Succeed())
}

const operatorContainerName = "cert-manager-operator"

// operatorContainerIdentity identifies one running operator container.
// RestartKey changes on an in-place container restart. The Pod UID does not,
// because RequestShutdown exits the process and kubelet restarts that container.
type operatorContainerIdentity struct {
	Name       string
	RestartKey string
}

// readyOperatorContainer returns the Ready operator container and a key that changes on restart.
func readyOperatorContainer(ctx context.Context) (*operatorContainerIdentity, error) {
	dep, err := k8sClientSet.AppsV1().Deployments(operatorNamespace).Get(ctx, operatorDeploymentName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	selector, err := metav1.LabelSelectorAsSelector(dep.Spec.Selector)
	if err != nil {
		return nil, err
	}
	pods, err := k8sClientSet.CoreV1().Pods(operatorNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, err
	}

	var newest *corev1.Pod
	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning {
			continue
		}
		ready := false
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
				ready = true
				break
			}
		}
		if !ready {
			continue
		}
		if newest == nil || pod.CreationTimestamp.After(newest.CreationTimestamp.Time) {
			newest = pod
		}
	}
	if newest == nil {
		return nil, fmt.Errorf("no ready operator pod in %s", operatorNamespace)
	}

	var restartKey string
	for _, status := range newest.Status.ContainerStatuses {
		if status.Name != operatorContainerName {
			continue
		}
		if status.ContainerID != "" {
			restartKey = status.ContainerID
		} else {
			restartKey = fmt.Sprintf("%s/%d", newest.UID, status.RestartCount)
		}
		break
	}
	if restartKey == "" {
		return nil, fmt.Errorf("operator container %s has no restart key on pod %s", operatorContainerName, newest.Name)
	}
	return &operatorContainerIdentity{Name: newest.Name, RestartKey: restartKey}, nil
}

// expectReadyOperatorContainer waits until the operator container is Ready.
func expectReadyOperatorContainer(ctx context.Context) *operatorContainerIdentity {
	GinkgoHelper()
	var got *operatorContainerIdentity
	Eventually(func() error {
		pod, err := readyOperatorContainer(ctx)
		if err != nil {
			return err
		}
		got = pod
		return nil
	}, highTimeout, fastPollInterval).Should(Succeed(), "ready operator container")
	return got
}

// expectOperatorContainerRestart waits for kubelet to start a new operator
// container. highTimeout covers the restart backoff kubelet applies after
// repeated graceful exits in this suite.
func expectOperatorContainerRestart(ctx context.Context, previousRestartKey string) *operatorContainerIdentity {
	GinkgoHelper()
	var got *operatorContainerIdentity
	Eventually(func() error {
		pod, err := readyOperatorContainer(ctx)
		if err != nil {
			return err
		}
		if pod.RestartKey == previousRestartKey {
			return fmt.Errorf("operator container restartKey still %s", previousRestartKey)
		}
		got = pod
		return nil
	}, highTimeout, fastPollInterval).Should(Succeed(), "operator container restart from %s", previousRestartKey)
	return got
}

// ensureStrictTLSProfileSettled applies profile under StrictAllComponents and
// waits out the operator restart that change causes. If the cluster is already
// on that profile and adherence, the current container is the baseline.
func ensureStrictTLSProfileSettled(ctx context.Context, profile *configapiv1.TLSSecurityProfile) *operatorContainerIdentity {
	GinkgoHelper()
	current, err := getClusterAPIServerTLSConfig(ctx)
	Expect(err).NotTo(HaveOccurred(), "read apiserver TLS configuration")
	before := expectReadyOperatorContainer(ctx)
	if current.adherence == configapiv1.TLSAdherencePolicyStrictAllComponents &&
		current.tlsProfile != nil && current.tlsProfile.Type == profile.Type {
		return before
	}
	patchStrictTLSProfile(ctx, profile)
	return expectOperatorContainerRestart(ctx, before.RestartKey)
}

func deleteOperatorControllerPods(ctx context.Context) error {
	dep, err := k8sClientSet.AppsV1().Deployments(operatorNamespace).Get(ctx, operatorDeploymentName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	selector, err := metav1.LabelSelectorAsSelector(dep.Spec.Selector)
	if err != nil {
		return err
	}
	return k8sClientSet.CoreV1().Pods(operatorNamespace).DeleteCollection(ctx, metav1.DeleteOptions{
		GracePeriodSeconds: ptr.To[int64](0),
	}, metav1.ListOptions{LabelSelector: selector.String()})
}

// setWebhookUnsupportedArgs sets spec.unsupportedConfigOverrides.webhook.args on the
// certmanager/cluster CR. Passing nil clears the entire UnsupportedConfigOverrides field
// (not just the webhook section), which is intentional for AfterAll cleanup to ensure a
// clean slate regardless of which scenario last wrote overrides.
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
