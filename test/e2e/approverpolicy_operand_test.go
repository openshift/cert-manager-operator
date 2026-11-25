//go:build e2e

package e2e

import (
	"context"
	"time"

	policyv1alpha1 "github.com/cert-manager/approver-policy/pkg/apis/policy/v1alpha1"
	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	cmclient "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// Installation, feature-gate failures, handoff recovery, and finalizer failures
// live in approverpolicy_test.go. This suite uses the installed operand as a user:
// publish a policy, grant its use permission, and request a Certificate and TLS Secret.
// Direct CertificateRequests cover manual retry and external signer approval only.
// Serial prevents overlapping changes to the shared singleton and approval RBAC.
var _ = Describe(
	"ApproverPolicy operand",
	Serial,
	Ordered,
	Label("Platform:Generic", "Feature:ApproverPolicyManager", "TechPreview"),
	func() {
		var (
			ctx                      = context.Background()
			client                   crclient.Client
			originalGates            string
			originalOperatorLogLevel string

			namespace *corev1.Namespace
			issuer    cmmeta.IssuerReference
			policy    *policyv1alpha1.CertificateRequestPolicy
		)

		// Certificates are submitted by the user, but cert-manager creates their
		// CertificateRequests. Policy-use permission therefore belongs to this account.
		certManagerSubject := rbacv1.Subject{
			Kind:      "ServiceAccount",
			Name:      "cert-manager",
			Namespace: operandNamespace,
		}

		BeforeAll(func() {
			client = approverPolicyManagerBeforeAll(ctx, &originalGates, &originalOperatorLogLevel, true)
			Expect(apiextensionsv1.AddToScheme(client.Scheme())).To(Succeed())

			// User workflows share one installation; each spec creates its own namespace, policy, and RBAC.
			createApproverPolicyManager(ctx, newApproverPolicyManagerCR())
			waitForAutoApproverHandoff(ctx)
		})

		AfterAll(func() {
			if client == nil {
				return
			}

			// Keep the feature gate enabled until finalization finishes. Restore settings
			// afterward, even if a cleanup assertion fails.
			defer approverPolicyManagerAfterAll(ctx, &originalGates, &originalOperatorLogLevel)

			By("removing the operand and restoring built-in approval after user workflows")
			cleanupApproverPolicyManager(ctx, client)
			for _, obj := range approverPolicyResources() {
				Expect(apierrors.IsNotFound(client.Get(ctx, crclient.ObjectKeyFromObject(obj), obj))).To(
					BeTrue(),
					"%s should be cleaned up",
					resourceDescription(obj),
				)
			}
		})

		BeforeEach(func() {
			By("waiting for operator status to become available")
			Expect(VerifyHealthyOperatorConditions(certmanageroperatorclient.OperatorV1alpha1())).To(Succeed())

			By("creating an isolated namespace for the user workflow")
			var err error
			namespace, err = loader.CreateTestingNS("e2e-approver-policy", false)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() {
				loader.DeleteTestingNS(namespace.Name, func() bool {
					return CurrentSpecReport().Failed()
				})
			})

			issuer = cmmeta.IssuerReference{
				Name:  namespace.Name,
				Kind:  "ClusterIssuer",
				Group: "cert-manager.io",
			}
			policy = newCertificateRequestPolicy(namespace.Name, issuer)
		})

		Context("Certificate issuance", func() {
			BeforeEach(func() {
				By("creating a ready issuer for real certificate issuance")
				createSelfSignedClusterIssuer(ctx, issuer.Name)
			})

			// A permitted Certificate produces a usable TLS Secret. Deleting that Secret
			// triggers a new approved request and a different certificate.
			It("should issue a Certificate and reissue it after its Secret is deleted", func() {
				By("creating a policy and authorizing cert-manager to use it")
				createCertificateRequestPolicy(ctx, client, policy)
				grantOperandPolicyUse(ctx, namespace.Name, policy.Name, certManagerSubject)

				By("requesting a Certificate and verifying its TLS Secret")
				certificate := newOperandCertificate(namespace.Name, "workload", issuer)
				request := createOperandCertificate(ctx, certificate)
				Expect(request.Spec.Username).To(Equal("system:serviceaccount:cert-manager:cert-manager"))
				original := waitForOperandCertificateIssued(ctx, certificate, request)

				By("deleting the TLS Secret to trigger reissuance")
				Expect(k8sClientSet.CoreV1().Secrets(namespace.Name).Delete(
					ctx, certificate.Spec.SecretName, metav1.DeleteOptions{},
				)).To(Succeed())

				// A new request UID and different certificate bytes prevent stale Ready
				// status from satisfying the reissuance check.
				renewal := waitForCertificateOwnedRequest(ctx, namespace.Name, certificate.Name, request.UID)
				waitForOperandCertificateIssued(ctx, certificate, renewal)
				waitForOperandSecret(ctx, namespace.Name, certificate.Spec.SecretName, certificate.Spec.DNSNames[0], original.Raw)
			})

			// With neither a policy nor use permission, the request stays pending.
			// Adding the policy and a ClusterRoleBinding allows approval and issuance.
			It("should issue a pending Certificate after adding a policy and cluster-scoped use permission", func() {
				const (
					certName   = "pending-then-signed"
					secretName = "pending-then-signed-tls"
					dnsName    = "policy-sign.handoff.example.com"
				)

				By("creating a Certificate with no CertificateRequestPolicy or use permission")
				createDNSCertificate(ctx, namespace.Name, certName, secretName, issuer.Name, dnsName)
				request := waitForCertificateOwnedRequest(ctx, namespace.Name, certName, "")
				Consistently(func(g Gomega) {
					current, err := certmanagerClient.CertmanagerV1().CertificateRequests(namespace.Name).Get(
						ctx, request.Name, metav1.GetOptions{},
					)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(certificateRequestHasDecision(current)).To(BeFalse())
				}, 15*time.Second, fastPollInterval).Should(Succeed())

				By("creating a matching CertificateRequestPolicy and cluster-scoped use RBAC")
				createAllowingCertificateRequestPolicy(ctx, client, policy.Name, namespace.Name, issuer.Name, dnsName)
				createCertificateRequestPolicyUseRBAC(ctx, policy.Name)

				By("waiting for approver-policy to approve and cert-manager to sign")
				waitForApproverPolicyDecision(ctx, request, cmv1.CertificateRequestConditionApproved)
				Expect(waitForCertificateReadiness(ctx, certName, namespace.Name)).To(Succeed())
			})

			// With no policy, issuance stays pending. Adding a matching policy releases
			// the existing request without recreating the Certificate.
			It("should wait for a matching policy before issuing a Certificate", func() {
				By("granting policy-use permission before the policy exists")
				grantOperandPolicyUse(ctx, namespace.Name, policy.Name, certManagerSubject)
				certificate := newOperandCertificate(namespace.Name, "waiting-for-policy", issuer)
				request := createOperandCertificate(ctx, certificate)
				expectOperandCertificatePending(ctx, certificate, request)

				By("adding the policy and verifying issuance from the same request")
				createCertificateRequestPolicy(ctx, client, policy)
				waitForOperandCertificateIssued(ctx, certificate, request)
			})

			// Policy-use permission is required for issuance. Revoking it blocks new
			// requests; restoring it releases them without changing earlier approvals.
			It("should require policy-use permission and observe its revocation", func() {
				By("creating a policy without granting cert-manager permission to use it")
				createCertificateRequestPolicy(ctx, client, policy)
				certificate := newOperandCertificate(namespace.Name, "unbound", issuer)
				request := createOperandCertificate(ctx, certificate)
				expectOperandCertificatePending(ctx, certificate, request)

				By("granting permission and verifying issuance")
				grantOperandPolicyUse(ctx, namespace.Name, policy.Name, certManagerSubject)
				waitForOperandCertificateIssued(ctx, certificate, request)

				By("revoking permission before requesting another Certificate")
				Expect(k8sClientSet.RbacV1().RoleBindings(namespace.Name).Delete(
					ctx, policy.Name, metav1.DeleteOptions{},
				)).To(Succeed())
				waitForOperandPolicyUse(ctx, namespace.Name, policy.Name, certManagerSubject, false)
				blocked := newOperandCertificate(namespace.Name, "revoked", issuer)
				pending := createOperandCertificate(ctx, blocked)
				expectOperandCertificatePending(ctx, blocked, pending)
				verifyApproverPolicyDecision(ctx, Default, request, cmv1.CertificateRequestConditionApproved)

				By("restoring the binding and verifying issuance of the pending Certificate")
				_, err := k8sClientSet.RbacV1().RoleBindings(namespace.Name).Create(ctx, &rbacv1.RoleBinding{
					ObjectMeta: metav1.ObjectMeta{Name: policy.Name},
					RoleRef: rbacv1.RoleRef{
						APIGroup: rbacv1.GroupName,
						Kind:     "Role",
						Name:     policy.Name,
					},
					Subjects: []rbacv1.Subject{certManagerSubject},
				}, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred())
				waitForOperandCertificateIssued(ctx, blocked, pending)
			})

			// A DNS name outside the allow list is denied with no TLS Secret. Correcting
			// the policy does not change the existing request's terminal decision.
			It("should deny an unpermitted DNS name and retain that denial after a policy change", func() {
				By("allowing only a different DNS name")
				policy.Spec.Allowed.DNSNames.Values = ptr.To([]string{"other.policy.example.com"})
				createCertificateRequestPolicy(ctx, client, policy)
				grantOperandPolicyUse(ctx, namespace.Name, policy.Name, certManagerSubject)

				certificate := newOperandCertificate(namespace.Name, "denied", issuer)
				request := createOperandCertificate(ctx, certificate)
				expectOperandCertificateDenied(ctx, certificate, request)

				By("correcting the policy and checking that the original request stays denied")
				updateCertificateRequestPolicy(ctx, client, policy, func(policy *policyv1alpha1.CertificateRequestPolicy) {
					policy.Spec.Allowed.DNSNames.Values = ptr.To([]string{"*.policy.example.com"})
				})
				expectOperandCertificateDenied(ctx, certificate, request)
			})

			Context("policy requirements", func() {
				// An otherwise matching Certificate cannot obtain a TLS Secret if it asks
				// for a key usage the policy does not permit.
				It("should deny a Certificate requesting an unpermitted key usage", func() {
					By("creating a policy that permits server authentication only")
					createCertificateRequestPolicy(ctx, client, policy)
					grantOperandPolicyUse(ctx, namespace.Name, policy.Name, certManagerSubject)

					By("requesting client authentication as an additional usage")
					certificate := newOperandCertificate(namespace.Name, "client-auth", issuer)
					certificate.Spec.Usages = append(certificate.Spec.Usages, cmv1.UsageClientAuth)
					request := createOperandCertificate(ctx, certificate)
					expectOperandCertificateDenied(ctx, certificate, request)
				})

				// A short-lived Certificate is issued, while a Certificate requesting a
				// lifetime beyond the configured maximum is denied.
				It("should enforce the policy's maximum certificate lifetime", func() {
					By("limiting certificates to a one-day lifetime")
					policy.Spec.Constraints = &policyv1alpha1.CertificateRequestPolicyConstraints{
						MaxDuration: &metav1.Duration{Duration: 24 * time.Hour},
					}
					createCertificateRequestPolicy(ctx, client, policy)
					grantOperandPolicyUse(ctx, namespace.Name, policy.Name, certManagerSubject)

					By("issuing a one-hour Certificate")
					allowed := newOperandCertificate(namespace.Name, "short-lived", issuer)
					request := createOperandCertificate(ctx, allowed)
					waitForOperandCertificateIssued(ctx, allowed, request)

					By("denying a Certificate requesting two days")
					denied := newOperandCertificate(namespace.Name, "long-lived", issuer)
					denied.Spec.Duration = &metav1.Duration{Duration: 48 * time.Hour}
					request = createOperandCertificate(ctx, denied)
					expectOperandCertificateDenied(ctx, denied, request)
				})

				// The configured private-key requirements permit an RSA Certificate and
				// reject an ECDSA Certificate, proving constraints affect actual issuance.
				It("should enforce the policy's private-key requirements", func() {
					By("requiring RSA keys of at least 2048 bits")
					policy.Spec.Constraints = &policyv1alpha1.CertificateRequestPolicyConstraints{
						PrivateKey: &policyv1alpha1.CertificateRequestPolicyConstraintsPrivateKey{
							Algorithm: ptr.To(cmv1.RSAKeyAlgorithm),
							MinSize:   ptr.To(2048),
						},
					}
					createCertificateRequestPolicy(ctx, client, policy)
					grantOperandPolicyUse(ctx, namespace.Name, policy.Name, certManagerSubject)

					By("issuing a Certificate with an RSA key")
					allowed := newOperandCertificate(namespace.Name, "rsa", issuer)
					request := createOperandCertificate(ctx, allowed)
					waitForOperandCertificateIssued(ctx, allowed, request)

					By("denying a Certificate with an ECDSA key")
					denied := newOperandCertificate(namespace.Name, "ecdsa", issuer)
					denied.Spec.PrivateKey = &cmv1.CertificatePrivateKey{
						Algorithm: cmv1.ECDSAKeyAlgorithm,
						Size:      256,
					}
					request = createOperandCertificate(ctx, denied)
					expectOperandCertificateDenied(ctx, denied, request)
				})

				// A namespace-aware CEL rule permits that namespace's DNS name and denies
				// another name even though both match the wildcard allow list.
				It("should apply a namespace-aware CEL rule to Certificate issuance", func() {
					By("requiring DNS names to match the requesting namespace")
					policy.Spec.Allowed.DNSNames.Validations = []policyv1alpha1.ValidationRule{
						{Rule: "self == cr.namespace + '.policy.example.com'"},
					}
					createCertificateRequestPolicy(ctx, client, policy)
					grantOperandPolicyUse(ctx, namespace.Name, policy.Name, certManagerSubject)

					By("issuing a Certificate for the namespace's DNS name")
					allowed := newOperandCertificate(namespace.Name, "namespace-dns", issuer)
					allowed.Spec.DNSNames = []string{namespace.Name + ".policy.example.com"}
					request := createOperandCertificate(ctx, allowed)
					waitForOperandCertificateIssued(ctx, allowed, request)

					By("denying a different DNS name within the same wildcard")
					denied := newOperandCertificate(namespace.Name, "other-dns", issuer)
					request = createOperandCertificate(ctx, denied)
					expectOperandCertificateDenied(ctx, denied, request)
				})
			})

			Context("policy scope", func() {
				// A policy permits issuance from its selected issuer only. Selecting a
				// second issuer releases that issuer's previously pending request.
				It("should issue Certificates only from the selected issuer", func() {
					By("authorizing the policy for one issuer")
					createCertificateRequestPolicy(ctx, client, policy)
					grantOperandPolicyUse(ctx, namespace.Name, policy.Name, certManagerSubject)
					allowed := newOperandCertificate(namespace.Name, "selected-issuer", issuer)
					request := createOperandCertificate(ctx, allowed)
					waitForOperandCertificateIssued(ctx, allowed, request)

					By("requesting a Certificate from another ready issuer")
					otherIssuer := issuer
					otherIssuer.Name += "-other"
					createSelfSignedClusterIssuer(ctx, otherIssuer.Name)
					blocked := newOperandCertificate(namespace.Name, "other-issuer", otherIssuer)
					pending := createOperandCertificate(ctx, blocked)
					expectOperandCertificatePending(ctx, blocked, pending)

					By("selecting the second issuer and issuing its pending Certificate")
					updateCertificateRequestPolicy(ctx, client, policy, func(policy *policyv1alpha1.CertificateRequestPolicy) {
						policy.Spec.Selector.IssuerRef.Name = ptr.To(otherIssuer.Name)
					})
					waitForOperandCertificateIssued(ctx, blocked, pending)
				})

				// Policy-use RBAC alone does not bypass namespace selection. Only selected
				// namespaces receive certificates until the policy's scope is expanded.
				It("should issue Certificates only in selected namespaces", func() {
					By("granting policy use in two namespaces but selecting only the first")
					otherNamespace := createNamespaceWithCleanup(ctx, "e2e-policy-other-", nil)
					createCertificateRequestPolicy(ctx, client, policy)
					grantOperandPolicyUse(ctx, namespace.Name, policy.Name, certManagerSubject)
					grantOperandPolicyUse(ctx, otherNamespace.Name, policy.Name, certManagerSubject)

					By("issuing a Certificate in the selected namespace")
					allowed := newOperandCertificate(namespace.Name, "selected-namespace", issuer)
					request := createOperandCertificate(ctx, allowed)
					waitForOperandCertificateIssued(ctx, allowed, request)

					By("leaving a Certificate in the other namespace pending")
					blocked := newOperandCertificate(otherNamespace.Name, "other-namespace", issuer)
					pending := createOperandCertificate(ctx, blocked)
					expectOperandCertificatePending(ctx, blocked, pending)

					By("adding that namespace to the policy and verifying issuance")
					updateCertificateRequestPolicy(ctx, client, policy, func(policy *policyv1alpha1.CertificateRequestPolicy) {
						policy.Spec.Selector.Namespace.MatchNames = []string{namespace.Name, otherNamespace.Name}
					})
					waitForOperandCertificateIssued(ctx, blocked, pending)
				})

				// Adding the namespace label required by a policy releases a pending
				// Certificate without editing the policy or resubmitting the Certificate.
				It("should issue a pending Certificate when its namespace gains the required label", func() {
					By("requiring a namespace label that is not yet present")
					policy.Spec.Selector.Namespace.MatchLabels = map[string]string{
						"e2e.cert-manager.io/policy-access": "allowed",
					}
					createCertificateRequestPolicy(ctx, client, policy)
					grantOperandPolicyUse(ctx, namespace.Name, policy.Name, certManagerSubject)
					certificate := newOperandCertificate(namespace.Name, "namespace-label", issuer)
					request := createOperandCertificate(ctx, certificate)
					expectOperandCertificatePending(ctx, certificate, request)

					By("labeling the namespace and verifying issuance of the same request")
					Expect(retry.RetryOnConflict(retry.DefaultRetry, func() error {
						current, err := k8sClientSet.CoreV1().Namespaces().Get(ctx, namespace.Name, metav1.GetOptions{})
						if err != nil {
							return err
						}
						if current.Labels == nil {
							current.Labels = map[string]string{}
						}
						current.Labels["e2e.cert-manager.io/policy-access"] = "allowed"
						_, err = k8sClientSet.CoreV1().Namespaces().Update(ctx, current, metav1.UpdateOptions{})
						return err
					})).To(Succeed())
					waitForOperandCertificateIssued(ctx, certificate, request)
				})
			})

			// Any allowing policy permits issuance. Tightening it blocks a new Certificate
			// when all matching policies deny, without changing the earlier approval.
			It("should issue when any matching policy allows and deny when all reject", func() {
				By("creating one denying policy and one allowing policy")
				policy.Spec.Allowed.DNSNames.Values = ptr.To([]string{"other.policy.example.com"})
				createCertificateRequestPolicy(ctx, client, policy)
				grantOperandPolicyUse(ctx, namespace.Name, policy.Name, certManagerSubject)
				allowing := newCertificateRequestPolicy(namespace.Name, issuer)
				allowing.Name += "-allowing"
				createCertificateRequestPolicy(ctx, client, allowing)
				grantOperandPolicyUse(ctx, namespace.Name, allowing.Name, certManagerSubject)

				By("issuing a Certificate under the allowing policy")
				certificate := newOperandCertificate(namespace.Name, "any-allow", issuer)
				request := createOperandCertificate(ctx, certificate)
				waitForOperandCertificateIssued(ctx, certificate, request)

				By("tightening the allowing policy and requesting another Certificate")
				updateCertificateRequestPolicy(ctx, client, allowing, func(policy *policyv1alpha1.CertificateRequestPolicy) {
					policy.Spec.Allowed.DNSNames.Values = ptr.To([]string{"other.policy.example.com"})
				})
				denied := newOperandCertificate(namespace.Name, "all-deny", issuer)
				rejected := createOperandCertificate(ctx, denied)
				expectOperandCertificateDenied(ctx, denied, rejected)
				verifyApproverPolicyDecision(ctx, Default, request, cmv1.CertificateRequestConditionApproved)
			})
		})

		Context("direct CertificateRequests", func() {
			var (
				requester        cmclient.Interface
				requesterSubject rbacv1.Subject
			)

			BeforeEach(func() {
				By("creating a ServiceAccount that submits its own CertificateRequests")
				requester = newOperandRequester(ctx, namespace.Name)
				requesterSubject = rbacv1.Subject{
					Kind:      "ServiceAccount",
					Name:      "requester",
					Namespace: namespace.Name,
				}
			})

			// A denied request stays denied after policy correction. A user can delete
			// and recreate that request to obtain approval under the corrected policy.
			It("should approve a manually recreated request after its policy is corrected", func() {
				By("submitting a request that the policy denies")
				policy.Spec.Allowed.DNSNames.Values = ptr.To([]string{"other.policy.example.com"})
				createCertificateRequestPolicy(ctx, client, policy)
				grantOperandPolicyUse(ctx, namespace.Name, policy.Name, requesterSubject)
				request := createOperandRequest(ctx, requester, namespace.Name, "retry", newOperandPolicyRequest(issuer))
				waitForApproverPolicyDecision(ctx, request, cmv1.CertificateRequestConditionDenied)

				By("correcting the policy while leaving the denied request intact")
				updateCertificateRequestPolicy(ctx, client, policy, func(policy *policyv1alpha1.CertificateRequestPolicy) {
					policy.Spec.Allowed.DNSNames.Values = ptr.To([]string{"*.policy.example.com"})
				})
				Consistently(func(g Gomega) {
					verifyApproverPolicyDecision(ctx, g, request, cmv1.CertificateRequestConditionDenied)
				}, operandPolicyObservationTime, fastPollInterval).Should(Succeed())

				By("manually deleting and recreating the request")
				deleteOperandRequest(ctx, request)
				replacement := createOperandRequest(ctx, requester, namespace.Name, request.Name, newOperandPolicyRequest(issuer))
				Expect(replacement.UID).NotTo(Equal(request.UID))
				waitForApproverPolicyDecision(ctx, replacement, cmv1.CertificateRequestConditionApproved)
			})

			// External requests require signer permission as well as a matching policy.
			// Exact scope allows one issuer, wildcard scope allows others, and revoking
			// permission blocks new requests until access is restored.
			It("should honor exact and wildcard approveSignerNames for external issuers", func() {
				By("registering an external issuer API and restricting approval to built-in issuers")
				externalIssuer := createOperandExternalIssuerAPI(ctx, client, namespace.Name)
				internalSigners := []string{"issuers.cert-manager.io/*", "clusterissuers.cert-manager.io/*"}
				// Restore signer permissions before AfterAll deletes the singleton, even if the test fails.
				defer updateApproverPolicySignerNames(ctx, nil)
				updateApproverPolicySignerNames(ctx, internalSigners)
				externalSigner := "externalissuers." + externalIssuer.Group

				// Wait for authorization revocation before testing a negative outcome.
				// Exact namespaced signers use <namespace>.<issuer-name> after the slash.
				externalSignerAccess := authorizationv1.ResourceAttributes{
					Group:    "cert-manager.io",
					Resource: "signers",
					Verb:     "approve",
					Name:     externalSigner + "/" + namespace.Name + "." + externalIssuer.Name,
				}
				approverSubject := rbacv1.Subject{
					Kind:      "ServiceAccount",
					Name:      approverPolicyName,
					Namespace: operandNamespace,
				}
				waitForOperandAccess(ctx, approverSubject, externalSignerAccess, false)

				policy = newCertificateRequestPolicy(namespace.Name, externalIssuer)
				policy.Spec.Selector.IssuerRef.Name = ptr.To("*")
				createCertificateRequestPolicy(ctx, client, policy)
				grantOperandPolicyUse(ctx, namespace.Name, policy.Name, requesterSubject)
				request := createOperandRequest(ctx, requester, namespace.Name, "external", newOperandPolicyRequest(externalIssuer))
				expectOperandRequestPending(ctx, request)

				By("permitting the named issuer and approving the pending request")
				updateApproverPolicySignerNames(ctx, append(internalSigners, externalSignerAccess.Name))
				waitForApproverPolicyDecision(ctx, request, cmv1.CertificateRequestConditionApproved)
				otherIssuer := externalIssuer
				otherIssuer.Name = "other"
				otherRequest := createOperandRequest(ctx, requester, namespace.Name, "other-external", newOperandPolicyRequest(otherIssuer))
				expectOperandRequestPending(ctx, otherRequest)

				By("permitting all issuers of that kind and approving the second request")
				updateApproverPolicySignerNames(ctx, append(internalSigners, externalSigner+"/*"))
				waitForApproverPolicyDecision(ctx, otherRequest, cmv1.CertificateRequestConditionApproved)

				By("revoking external signer permission before submitting another request")
				updateApproverPolicySignerNames(ctx, internalSigners)
				waitForOperandAccess(ctx, approverSubject, externalSignerAccess, false)
				externalSignerAccess.Name = externalSigner + "/*"
				waitForOperandAccess(ctx, approverSubject, externalSignerAccess, false)
				pending := createOperandRequest(ctx, requester, namespace.Name, "external-revoked", newOperandPolicyRequest(externalIssuer))
				expectOperandRequestPending(ctx, pending)

				By("restoring signer permission and approving the pending request")
				updateApproverPolicySignerNames(ctx, append(internalSigners, externalSigner+"/*"))
				waitForApproverPolicyDecision(ctx, pending, cmv1.CertificateRequestConditionApproved)
			})
		})
	},
)
