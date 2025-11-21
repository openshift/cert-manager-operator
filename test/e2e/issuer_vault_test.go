//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"fmt"
	"log"
	"strings"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanagermetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/tidwall/gjson"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	// Vault server configuration
	vaultCASecretName     = "vault-server-tls"
	vaultPKISignPath      = "pki_int/sign/cluster-dot-local"
	vaultKubernetesHost   = "https://kubernetes.default.svc"
	vaultServiceAccountCA = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"

	// Vault authentication paths
	vaultAuthPathAppRole    = "approle"
	vaultAuthPathKubernetes = "/v1/auth/kubernetes"
	vaultAuthPathJWT        = "/v1/auth/jwt"

	// OIDC configuration
	oidcJWKSPath = "/openid/v1/jwks"
)

var _ = Describe("Vault Issuer", Ordered, Label("TechPreview"), func() {
	var ctx context.Context
	var cancel context.CancelFunc
	var ns *corev1.Namespace
	var vaultPodName string
	var vaultRootToken string
	var vaultServiceURL string

	// Helper function to create a standard Vault issuer
	createVaultIssuer := func(issuerName string, auth certmanagerv1.VaultAuth) *certmanagerv1.Issuer {
		return &certmanagerv1.Issuer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      issuerName,
				Namespace: ns.Name,
			},
			Spec: certmanagerv1.IssuerSpec{
				IssuerConfig: certmanagerv1.IssuerConfig{
					Vault: &certmanagerv1.VaultIssuer{
						Server: vaultServiceURL,
						CABundleSecretRef: &certmanagermetav1.SecretKeySelector{
							LocalObjectReference: certmanagermetav1.LocalObjectReference{
								Name: vaultCASecretName,
							},
							Key: "ca.crt",
						},
						Path: vaultPKISignPath,
						Auth: auth,
					},
				},
			},
		}
	}

	// Helper function to create and verify certificate
	createAndVerifyCertificate := func(issuerName, certName, commonName string) {
		By("creating certificate")
		cert := &certmanagerv1.Certificate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      certName,
				Namespace: ns.Name,
			},
			Spec: certmanagerv1.CertificateSpec{
				CommonName: commonName,
				SecretName: certName,
				IssuerRef: certmanagermetav1.ObjectReference{
					Name: issuerName,
					Kind: "Issuer",
				},
			},
		}
		_, err := certmanagerClient.CertmanagerV1().Certificates(ns.Name).Create(ctx, cert, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to create certificate")

		By("waiting for certificate to become ready")
		err = waitForCertificateReadiness(ctx, certName, ns.Name)
		Expect(err).NotTo(HaveOccurred(), "timeout waiting for certificate to become Ready")

		By("verifying certificate")
		err = verifyCertificate(ctx, certName, ns.Name, commonName)
		Expect(err).NotTo(HaveOccurred(), "certificate verification failed")
	}

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), highTimeout)
		DeferCleanup(cancel)

		By("waiting for operator status to become available")
		err := VerifyHealthyOperatorConditions(certmanageroperatorclient.OperatorV1alpha1())
		Expect(err).NotTo(HaveOccurred(), "Operator is expected to be available")

		By("creating a test namespace")
		ns, err = loader.CreateTestingNS("e2e-vault", false)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			By("cleaning up test namespace")
			loader.DeleteTestingNS(ns.Name, func() bool { return CurrentSpecReport().Failed() })
		})

		By("setting up Vault server in cluster with PKI secrets engine enabled")
		vaultReleaseName := "vault-" + randomStr(4)
		var clusterRoleBindingName string
		vaultPodName, vaultRootToken, clusterRoleBindingName, err = setupVaultServer(ctx, cfg, loader, certmanagerClient, ns.Name, vaultReleaseName)
		Expect(err).NotTo(HaveOccurred())
		Expect(vaultPodName).NotTo(BeEmpty())
		Expect(vaultRootToken).NotTo(BeEmpty())
		Expect(clusterRoleBindingName).NotTo(BeEmpty())
		DeferCleanup(func() {
			By("cleaning up ClusterRoleBinding for Vault installer")
			err := loader.KubeClient.RbacV1().ClusterRoleBindings().Delete(context.Background(), clusterRoleBindingName, metav1.DeleteOptions{})
			if err != nil && !apierrors.IsNotFound(err) {
				log.Printf("Warning: failed to delete ClusterRoleBinding %s: %v", clusterRoleBindingName, err)
			}
		})

		vaultServiceURL = fmt.Sprintf("https://%s.%s.svc:8200", vaultReleaseName, ns.Name)

		By("configuring Vault PKI engine")
		err = configureVaultPKI(ctx, cfg, loader, ns.Name, vaultPodName, vaultRootToken)
		Expect(err).NotTo(HaveOccurred())
	})

	Context("AppRole authentication", func() {
		It("should issue a valid certificate", func() {
			appRoleVaultRoleName := "cert-manager"
			vaultSecretName := "cert-manager-vault-approle"
			issuerName := "issuer-vault-approle"
			certName := "cert-from-" + issuerName
			certCommonName := certName + ".cluster.local"

			By("configuring Vault AppRole authentication")
			tokenEnv := fmt.Sprintf("export VAULT_TOKEN=%s", vaultRootToken)
			vaultCmd := fmt.Sprintf(`%s && vault auth enable approle && vault write auth/approle/role/%s token_policies="cert-manager" token_ttl=1h token_max_ttl=4h`, tokenEnv, appRoleVaultRoleName)
			_, err := execInPod(ctx, cfg, loader.KubeClient, ns.Name, vaultPodName, "vault", "sh", "-c", vaultCmd)
			Expect(err).NotTo(HaveOccurred(), "failed to configure Vault AppRole authentication")

			By("retrieving AppRole role ID")
			vaultCmd = fmt.Sprintf(`%s && vault read -format=json auth/approle/role/%s/role-id`, tokenEnv, appRoleVaultRoleName)
			output, err := execInPod(ctx, cfg, loader.KubeClient, ns.Name, vaultPodName, "vault", "sh", "-c", vaultCmd)
			Expect(err).NotTo(HaveOccurred(), "failed to retrieve AppRole role ID")
			vaultRoleID := gjson.Get(output, "data.role_id").String()
			Expect(vaultRoleID).NotTo(BeEmpty(), "AppRole role ID should not be empty")

			By("retrieving AppRole secret ID")
			vaultCmd = fmt.Sprintf(`%s && vault write -format=json -force auth/approle/role/%s/secret-id`, tokenEnv, appRoleVaultRoleName)
			output, err = execInPod(ctx, cfg, loader.KubeClient, ns.Name, vaultPodName, "vault", "sh", "-c", vaultCmd)
			Expect(err).NotTo(HaveOccurred(), "failed to retrieve AppRole secret ID")
			vaultSecretID := gjson.Get(output, "data.secret_id").String()
			Expect(vaultSecretID).NotTo(BeEmpty(), "AppRole secret ID should not be empty")

			By("creating auth secret for AppRole")
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      vaultSecretName,
					Namespace: ns.Name,
				},
				StringData: map[string]string{
					"secretId": vaultSecretID,
				},
			}
			_, err = loader.KubeClient.CoreV1().Secrets(ns.Name).Create(ctx, secret, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create AppRole secret")

			By("creating Vault issuer with AppRole authentication")
			issuer := createVaultIssuer(issuerName, certmanagerv1.VaultAuth{
				AppRole: &certmanagerv1.VaultAppRole{
					Path:   vaultAuthPathAppRole,
					RoleId: vaultRoleID,
					SecretRef: certmanagermetav1.SecretKeySelector{
						LocalObjectReference: certmanagermetav1.LocalObjectReference{
							Name: vaultSecretName,
						},
						Key: "secretId",
					},
				},
			})
			_, err = certmanagerClient.CertmanagerV1().Issuers(ns.Name).Create(ctx, issuer, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create Vault issuer with AppRole auth")

			By("waiting for issuer to become ready")
			err = waitForIssuerReadiness(ctx, issuerName, ns.Name)
			Expect(err).NotTo(HaveOccurred(), "timeout waiting for issuer to become Ready")

			createAndVerifyCertificate(issuerName, certName, certCommonName)
		})
	})

	Context("Token authentication", func() {
		It("should issue a valid certificate", func() {
			vaultSecretName := "cert-manager-vault-token"
			issuerName := "issuer-vault-token"
			certName := "cert-from-" + issuerName
			certCommonName := certName + ".cluster.local"

			By("creating Vault token with cert-manager policy")
			tokenEnv := fmt.Sprintf("export VAULT_TOKEN=%s", vaultRootToken)
			vaultCmd := fmt.Sprintf(`%s && vault token create -format=json -policy=cert-manager -ttl=720h`, tokenEnv)
			output, err := execInPod(ctx, cfg, loader.KubeClient, ns.Name, vaultPodName, "vault", "sh", "-c", vaultCmd)
			Expect(err).NotTo(HaveOccurred(), "failed to create Vault token")
			vaultToken := gjson.Get(output, "auth.client_token").String()
			Expect(vaultToken).NotTo(BeEmpty(), "Vault token should not be empty")

			By("creating auth secret for token")
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      vaultSecretName,
					Namespace: ns.Name,
				},
				StringData: map[string]string{
					"token": vaultToken,
				},
			}
			_, err = loader.KubeClient.CoreV1().Secrets(ns.Name).Create(ctx, secret, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create token secret")

			By("creating Vault issuer with token authentication")
			issuer := createVaultIssuer(issuerName, certmanagerv1.VaultAuth{
				TokenSecretRef: &certmanagermetav1.SecretKeySelector{
					LocalObjectReference: certmanagermetav1.LocalObjectReference{
						Name: vaultSecretName,
					},
					Key: "token",
				},
			})
			_, err = certmanagerClient.CertmanagerV1().Issuers(ns.Name).Create(ctx, issuer, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create Vault issuer with token auth")

			By("waiting for issuer to become ready")
			err = waitForIssuerReadiness(ctx, issuerName, ns.Name)
			Expect(err).NotTo(HaveOccurred(), "timeout waiting for issuer to become Ready")

			createAndVerifyCertificate(issuerName, certName, certCommonName)
		})
	})

	Context("Kubernetes authentication with static service account", func() {
		It("should issue a valid certificate", func() {
			serviceAccountName := "cert-manager-vault-static-serviceaccount"
			issuerName := "issuer-vault-static-serviceaccount"
			certName := "cert-from-" + issuerName
			certCommonName := certName + ".cluster.local"

			By("creating service account")
			serviceAccount := &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceAccountName,
					Namespace: ns.Name,
				},
			}
			_, err := loader.KubeClient.CoreV1().ServiceAccounts(ns.Name).Create(ctx, serviceAccount, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create service account")

			By("creating long-lived API token secret for service account")
			tokenSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceAccountName,
					Namespace: ns.Name,
					Annotations: map[string]string{
						"kubernetes.io/service-account.name": serviceAccountName,
					},
				},
				Type: corev1.SecretTypeServiceAccountToken,
			}
			_, err = loader.KubeClient.CoreV1().Secrets(ns.Name).Create(ctx, tokenSecret, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create service account token secret")

			By("configuring Kubernetes auth in Vault")
			tokenEnv := fmt.Sprintf("export VAULT_TOKEN=%s", vaultRootToken)
			vaultCmd := fmt.Sprintf(`%s && vault auth enable kubernetes && vault write auth/kubernetes/config kubernetes_host="%s" kubernetes_ca_cert=@%s && \
vault write auth/kubernetes/role/issuer bound_service_account_names=%s bound_service_account_namespaces=%s token_policies=cert-manager ttl=1h`,
				tokenEnv, vaultKubernetesHost, vaultServiceAccountCA, serviceAccountName, ns.Name)
			_, err = execInPod(ctx, cfg, loader.KubeClient, ns.Name, vaultPodName, "vault", "sh", "-c", vaultCmd)
			Expect(err).NotTo(HaveOccurred(), "failed to configure Kubernetes auth in Vault")

			By("creating Vault issuer with Kubernetes static service account")
			issuer := createVaultIssuer(issuerName, certmanagerv1.VaultAuth{
				Kubernetes: &certmanagerv1.VaultKubernetesAuth{
					Path: vaultAuthPathKubernetes,
					Role: "issuer",
					SecretRef: certmanagermetav1.SecretKeySelector{
						LocalObjectReference: certmanagermetav1.LocalObjectReference{
							Name: serviceAccountName,
						},
						Key: "token",
					},
				},
			})
			_, err = certmanagerClient.CertmanagerV1().Issuers(ns.Name).Create(ctx, issuer, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create Vault issuer with Kubernetes static service account auth")

			By("waiting for issuer to become ready")
			err = waitForIssuerReadiness(ctx, issuerName, ns.Name)
			Expect(err).NotTo(HaveOccurred(), "timeout waiting for issuer to become Ready")

			createAndVerifyCertificate(issuerName, certName, certCommonName)
		})
	})

	Context("Kubernetes authentication with bound service account", func() {
		It("should issue a valid certificate", func() {
			serviceAccountName := "cert-manager-vault-bound-serviceaccount"
			issuerName := "issuer-vault-bound-serviceaccount"
			certName := "cert-from-" + issuerName
			certCommonName := certName + ".cluster.local"

			By("creating service account")
			serviceAccount := &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceAccountName,
					Namespace: ns.Name,
				},
			}
			_, err := loader.KubeClient.CoreV1().ServiceAccounts(ns.Name).Create(ctx, serviceAccount, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create service account")

			By("creating RBAC resources for cert-manager to request tokens for this service account")
			role := &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceAccountName + "-tokenrequest",
					Namespace: ns.Name,
				},
				Rules: []rbacv1.PolicyRule{
					{
						APIGroups:     []string{""},
						Resources:     []string{"serviceaccounts/token"},
						ResourceNames: []string{serviceAccountName},
						Verbs:         []string{"create"},
					},
				},
			}
			_, err = loader.KubeClient.RbacV1().Roles(ns.Name).Create(ctx, role, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create role")

			roleBinding := &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceAccountName + "-tokenrequest",
					Namespace: ns.Name,
				},
				RoleRef: rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "Role",
					Name:     serviceAccountName + "-tokenrequest",
				},
				Subjects: []rbacv1.Subject{
					{
						Kind:      "ServiceAccount",
						Name:      "cert-manager",
						Namespace: "cert-manager",
					},
				},
			}
			_, err = loader.KubeClient.RbacV1().RoleBindings(ns.Name).Create(ctx, roleBinding, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create role binding")

			By("configuring Kubernetes auth in Vault")
			tokenEnv := fmt.Sprintf("export VAULT_TOKEN=%s", vaultRootToken)
			vaultCmd := fmt.Sprintf(`%s && vault auth enable kubernetes && vault write auth/kubernetes/config kubernetes_host="%s" kubernetes_ca_cert=@%s && \
vault write auth/kubernetes/role/issuer bound_service_account_names=%s bound_service_account_namespaces=%s token_policies=cert-manager ttl=1h`,
				tokenEnv, vaultKubernetesHost, vaultServiceAccountCA, serviceAccountName, ns.Name)
			_, err = execInPod(ctx, cfg, loader.KubeClient, ns.Name, vaultPodName, "vault", "sh", "-c", vaultCmd)
			Expect(err).NotTo(HaveOccurred(), "failed to configure Kubernetes auth in Vault")

			By("creating Vault issuer with Kubernetes bound service account")
			issuer := createVaultIssuer(issuerName, certmanagerv1.VaultAuth{
				Kubernetes: &certmanagerv1.VaultKubernetesAuth{
					Path: vaultAuthPathKubernetes,
					Role: "issuer",
					ServiceAccountRef: &certmanagerv1.ServiceAccountRef{
						Name: serviceAccountName,
					},
				},
			})
			_, err = certmanagerClient.CertmanagerV1().Issuers(ns.Name).Create(ctx, issuer, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create Vault issuer with Kubernetes bound service account auth")

			By("waiting for issuer to become ready")
			err = waitForIssuerReadiness(ctx, issuerName, ns.Name)
			Expect(err).NotTo(HaveOccurred(), "timeout waiting for issuer to become Ready")

			createAndVerifyCertificate(issuerName, certName, certCommonName)
		})
	})

	Context("Kubernetes authentication with bound service account over JWT/OIDC", func() {
		It("should issue a valid certificate", func() {
			serviceAccountName := "cert-manager-vault-bound-serviceaccount-jwt"
			issuerName := "issuer-vault-bound-serviceaccount-jwt"
			certName := "cert-from-" + issuerName
			certCommonName := certName + ".cluster.local"

			By("creating service account")
			serviceAccount := &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceAccountName,
					Namespace: ns.Name,
				},
			}
			_, err := loader.KubeClient.CoreV1().ServiceAccounts(ns.Name).Create(ctx, serviceAccount, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create service account")

			By("creating RBAC resources for cert-manager to request tokens for this service account")
			role := &rbacv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceAccountName + "-tokenrequest",
					Namespace: ns.Name,
				},
				Rules: []rbacv1.PolicyRule{
					{
						APIGroups:     []string{""},
						Resources:     []string{"serviceaccounts/token"},
						ResourceNames: []string{serviceAccountName},
						Verbs:         []string{"create"},
					},
				},
			}
			_, err = loader.KubeClient.RbacV1().Roles(ns.Name).Create(ctx, role, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create role")

			roleBinding := &rbacv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceAccountName + "-tokenrequest",
					Namespace: ns.Name,
				},
				RoleRef: rbacv1.RoleRef{
					APIGroup: "rbac.authorization.k8s.io",
					Kind:     "Role",
					Name:     serviceAccountName + "-tokenrequest",
				},
				Subjects: []rbacv1.Subject{
					{
						Kind:      "ServiceAccount",
						Name:      "cert-manager",
						Namespace: "cert-manager",
					},
				},
			}
			_, err = loader.KubeClient.RbacV1().RoleBindings(ns.Name).Create(ctx, roleBinding, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create role binding")

			By("retrieving OIDC issuer URL from cluster")
			restClient := loader.KubeClient.CoreV1().RESTClient()
			result := restClient.Get().AbsPath("/.well-known/openid-configuration").Do(ctx)
			rawBody, err := result.Raw()
			Expect(err).NotTo(HaveOccurred(), "unable to retrieve OIDC issuer from cluster")
			oidcIssuer := gjson.Get(string(rawBody), "issuer").String()
			Expect(oidcIssuer).NotTo(BeEmpty(), "OIDC issuer URL should not be empty")

			By("configuring JWT auth in Vault")
			tokenEnv := fmt.Sprintf("export VAULT_TOKEN=%s", vaultRootToken)
			vaultCmd := fmt.Sprintf(`%s && vault auth enable jwt && vault write auth/jwt/config oidc_discovery_url=%s`, tokenEnv, oidcIssuer)

			// Handle non-STS environments where OIDC issuer is internal URL
			if strings.Contains(oidcIssuer, "kubernetes.default.svc") {
				By("creating RBAC resources for anonymous user to get jwks_uri in non-STS env")
				clusterRole := &rbacv1.ClusterRole{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "vault-get-jwks-role-",
					},
					Rules: []rbacv1.PolicyRule{
						{
							NonResourceURLs: []string{oidcJWKSPath},
							Verbs:           []string{"get"},
						},
					},
				}
				createdClusterRole, err := loader.KubeClient.RbacV1().ClusterRoles().Create(ctx, clusterRole, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred(), "failed to create ClusterRole for JWKS access")
				DeferCleanup(func() {
					loader.KubeClient.RbacV1().ClusterRoles().Delete(ctx, createdClusterRole.Name, metav1.DeleteOptions{})
				})

				clusterRoleBinding := &rbacv1.ClusterRoleBinding{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: "vault-get-jwks-rolebinding-",
					},
					RoleRef: rbacv1.RoleRef{
						APIGroup: "rbac.authorization.k8s.io",
						Kind:     "ClusterRole",
						Name:     createdClusterRole.Name,
					},
					Subjects: []rbacv1.Subject{
						{
							APIGroup: "rbac.authorization.k8s.io",
							Kind:     "Group",
							Name:     "system:unauthenticated",
						},
					},
				}
				createdClusterRoleBinding, err := loader.KubeClient.RbacV1().ClusterRoleBindings().Create(ctx, clusterRoleBinding, metav1.CreateOptions{})
				Expect(err).NotTo(HaveOccurred(), "failed to create ClusterRoleBinding for JWKS access")
				DeferCleanup(func() {
					loader.KubeClient.RbacV1().ClusterRoleBindings().Delete(ctx, createdClusterRoleBinding.Name, metav1.DeleteOptions{})
				})

				// Add CA certificate for internal OIDC issuer
				vaultCmd += " oidc_discovery_ca_pem=@" + vaultServiceAccountCA
			}

			_, err = execInPod(ctx, cfg, loader.KubeClient, ns.Name, vaultPodName, "vault", "sh", "-c", vaultCmd)
			Expect(err).NotTo(HaveOccurred(), "failed to configure JWT auth in Vault")

			By("creating JWT role in Vault")
			vaultCmd = fmt.Sprintf(`%s && vault write auth/jwt/role/issuer role_type=jwt bound_audiences="vault://%s/%s" user_claim=sub bound_subject="system:serviceaccount:%s:%s" token_policies=cert-manager ttl=1m`,
				tokenEnv, ns.Name, issuerName, ns.Name, serviceAccountName)
			_, err = execInPod(ctx, cfg, loader.KubeClient, ns.Name, vaultPodName, "vault", "sh", "-c", vaultCmd)
			Expect(err).NotTo(HaveOccurred(), "failed to create JWT role in Vault")

			By("creating Vault issuer with Kubernetes bound service account using JWT auth")
			issuer := createVaultIssuer(issuerName, certmanagerv1.VaultAuth{
				Kubernetes: &certmanagerv1.VaultKubernetesAuth{
					Path: vaultAuthPathJWT,
					Role: "issuer",
					ServiceAccountRef: &certmanagerv1.ServiceAccountRef{
						Name: serviceAccountName,
					},
				},
			})
			_, err = certmanagerClient.CertmanagerV1().Issuers(ns.Name).Create(ctx, issuer, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create Vault issuer with JWT auth")

			By("waiting for issuer to become ready")
			err = waitForIssuerReadiness(ctx, issuerName, ns.Name)
			Expect(err).NotTo(HaveOccurred(), "timeout waiting for issuer to become Ready")

			createAndVerifyCertificate(issuerName, certName, certCommonName)
		})
	})
})
