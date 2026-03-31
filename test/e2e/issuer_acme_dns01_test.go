//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/msi/armmsi"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/google/uuid"
	gcpcrm "google.golang.org/api/cloudresourcemanager/v1"
	gcpiam "google.golang.org/api/iam/v1"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/openshift/cert-manager-operator/test/library"

	acmev1 "github.com/cert-manager/cert-manager/pkg/apis/acme/v1"
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanagermetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	// letsEncryptStagingServerURL is the address for the Let's Encrypt staging environment server.
	letsEncryptStagingServerURL = "https://acme-staging-v02.api.letsencrypt.org/directory"

	// acmeSolverPodLabel is the label that cert-manager uses to identify ACME solver pods.
	acmeSolverPodLabel = "acme.cert-manager.io/http01-solver"

	// acmeSolverContainerName is the name of the container in the ACME solver pod.
	acmeSolverContainerName = "acmesolver"

	// TARGET_PLATFORM is the environment variable for IBM Cloud CIS test.
	targetPlatformEnvironmentVar = "TARGET_PLATFORM"

	// CIS_CRN is the required environment variable for IBM Cloud platform.
	cisCRNEnvironmentVar = "CIS_CRN"
)

var _ = Describe("ACME Issuer DNS01 solver", Ordered, func() {
	var ctx context.Context
	var cancel context.CancelFunc
	var ns *corev1.Namespace
	var appsDomain string
	var baseDomain string

	BeforeAll(func() {
		ctx = context.Background()
		var err error

		By("getting cluster base domain and construct app domain")
		baseDomain, err = library.GetClusterBaseDomain(ctx, configClient)
		Expect(err).NotTo(HaveOccurred(), "failed to get cluster base domain")
		Expect(baseDomain).NotTo(BeEmpty(), "base domain should not be empty")
		appsDomain = "apps." + baseDomain

		By("adding required args to cert-manager controller")
		err = addOverrideArgs(certmanageroperatorclient, certmanagerControllerDeployment, []string{
			// for Issuer to use ambient credentials
			"--issuer-ambient-credentials",
			// only query the configured DNS resolvers to perform the DNS01 self-checks
			"--dns01-recursive-nameservers-only",
			// the recursive nameservers to query for doing the DNS01 self-checks
			"--dns01-recursive-nameservers=8.8.8.8:53,1.1.1.1:53",
			// use DNS-over-HTTPS for doing the DNS01 self-checks
			"--dns01-recursive-nameservers=https://1.1.1.1/dns-query",
		})
		Expect(err).NotTo(HaveOccurred())

		proxy, err := configClient.Proxies().Get(ctx, "cluster", metav1.GetOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			Expect(err).NotTo(HaveOccurred(), "failed to get cluster proxy config")
		}
		if err == nil && proxy.Spec.TrustedCA.Name != "" {
			By("creating trusted CA ConfigMap for HTTPS proxy")
			trustedCA := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "trusted-ca",
					Namespace: "cert-manager",
					Labels: map[string]string{
						"config.openshift.io/inject-trusted-cabundle": "true",
					},
				},
			}
			_, err = loader.KubeClient.CoreV1().ConfigMaps("cert-manager").Create(ctx, trustedCA, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			DeferCleanup(func(cleanupCtx context.Context) {
				loader.KubeClient.CoreV1().ConfigMaps("cert-manager").Delete(cleanupCtx, "trusted-ca", metav1.DeleteOptions{})
			})

			By("setting trusted CA ConfigMap name via subscription env var")
			err = patchSubscriptionWithEnvVars(ctx, loader, map[string]string{
				"TRUSTED_CA_CONFIGMAP_NAME": "trusted-ca",
			})
			Expect(err).NotTo(HaveOccurred(), "failed to patch subscription with 'TRUSTED_CA_CONFIGMAP_NAME' environment variable")

			DeferCleanup(func(cleanupCtx context.Context) {
				By("removing 'TRUSTED_CA_CONFIGMAP_NAME' from subscription")
				if err := patchSubscriptionWithEnvVars(cleanupCtx, loader, map[string]string{
					"TRUSTED_CA_CONFIGMAP_NAME": "",
				}); err != nil {
					fmt.Fprintf(GinkgoWriter, "failed to remove env var from subscription during cleanup: %v\n", err)
					return
				}
			})

			By("waiting for operator deployment to restart with trusted CA configuration")
			err = waitForDeploymentEnvVarAndRollout(ctx, operatorNamespace, operatorDeploymentName, "TRUSTED_CA_CONFIGMAP_NAME", "trusted-ca", lowTimeout)
			Expect(err).NotTo(HaveOccurred())
		}

		DeferCleanup(func() {
			By("resetting cert-manager state")
			err = resetCertManagerState(context.Background(), certmanageroperatorclient, loader)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	BeforeEach(func() {
		var err error
		ctx, cancel = context.WithTimeout(context.Background(), highTimeout)
		DeferCleanup(cancel)

		By("waiting for operator status to become available")
		err = VerifyHealthyOperatorConditions(certmanageroperatorclient.OperatorV1alpha1())
		Expect(err).NotTo(HaveOccurred(), "Operator is expected to be available")

		By("creating a test namespace")
		ns, err = loader.CreateTestingNS("e2e-acme-dns01", false)
		Expect(err).NotTo(HaveOccurred(), "failed to create test namespace")
		DeferCleanup(func() {
			By("cleaning up test namespace")
			loader.DeleteTestingNS(ns.Name, func() bool { return CurrentSpecReport().Failed() })
		})
	})

	// createACMEIssuer creates an ACME Issuer with the given solver configuration (follows createVaultIssuer pattern)
	createACMEIssuer := func(issuerName string, solver acmev1.ACMEChallengeSolver) *certmanagerv1.Issuer {
		return &certmanagerv1.Issuer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      issuerName,
				Namespace: ns.Name,
			},
			Spec: certmanagerv1.IssuerSpec{
				IssuerConfig: certmanagerv1.IssuerConfig{
					ACME: &acmev1.ACMEIssuer{
						Server: letsEncryptStagingServerURL,
						PrivateKey: certmanagermetav1.SecretKeySelector{
							LocalObjectReference: certmanagermetav1.LocalObjectReference{
								Name: issuerName + "-account",
							},
						},
						Solvers: []acmev1.ACMEChallengeSolver{solver},
					},
				},
			},
		}
	}

	// createACMEClusterIssuer creates an ACME ClusterIssuer with the given solver configuration
	createACMEClusterIssuer := func(clusterIssuerName string, solver acmev1.ACMEChallengeSolver) *certmanagerv1.ClusterIssuer {
		return &certmanagerv1.ClusterIssuer{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterIssuerName,
			},
			Spec: certmanagerv1.IssuerSpec{
				IssuerConfig: certmanagerv1.IssuerConfig{
					ACME: &acmev1.ACMEIssuer{
						Server: letsEncryptStagingServerURL,
						PrivateKey: certmanagermetav1.SecretKeySelector{
							LocalObjectReference: certmanagermetav1.LocalObjectReference{
								Name: clusterIssuerName + "-account",
							},
						},
						Solvers: []acmev1.ACMEChallengeSolver{solver},
					},
				},
			},
		}
	}

	// createAndVerifyACMECertificate creates a certificate and verifies it becomes ready
	createAndVerifyACMECertificate := func(ctx context.Context, certName, namespace, dnsName, issuerName, issuerKind string) {
		By(fmt.Sprintf("creating certificate %s for DNS name '%s'", certName, dnsName))
		cert := &certmanagerv1.Certificate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      certName,
				Namespace: namespace,
			},
			Spec: certmanagerv1.CertificateSpec{
				DNSNames:   []string{dnsName},
				SecretName: certName,
				IssuerRef: certmanagermetav1.ObjectReference{
					Name: issuerName,
					Kind: issuerKind,
				},
			},
		}
		_, err := certmanagerClient.CertmanagerV1().Certificates(namespace).Create(ctx, cert, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("failed to create certificate %s", certName))

		By("waiting for certificate to become ready")
		err = waitForCertificateReadiness(ctx, certName, namespace)
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("timeout waiting for certificate %s to become ready", certName))

		By("verifying certificate")
		err = verifyCertificate(ctx, cert.Spec.SecretName, namespace, cert.Spec.DNSNames[0])
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("certificate %s verification failed", certName))
	}

	// getAWSRegion retrieves AWS region from Infrastructure object
	getAWSRegion := func(ctx context.Context) []byte {
		By("getting AWS region from Infrastructure object")
		infra, err := configClient.Infrastructures().Get(ctx, "cluster", metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to get Infrastructure object")
		region := []byte(infra.Status.PlatformStatus.AWS.Region)
		Expect(region).NotTo(BeEmpty(), "AWS region should not be empty")

		return region
	}

	// getAWSCredentials retrieves AWS credentials from the kube-system namespace
	getAWSCredentials := func(ctx context.Context) (accessKeyID, secretAccessKey []byte) {
		By("obtaining AWS credentials from kube-system namespace")
		awsCredsSecret, err := loader.KubeClient.CoreV1().Secrets("kube-system").Get(ctx, "aws-creds", metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to get AWS credentials from kube-system")

		accessKeyID = awsCredsSecret.Data["aws_access_key_id"]
		secretAccessKey = awsCredsSecret.Data["aws_secret_access_key"]
		Expect(accessKeyID).NotTo(BeEmpty(), "aws_access_key_id should not be empty")
		Expect(secretAccessKey).NotTo(BeEmpty(), "aws_secret_access_key should not be empty")

		return accessKeyID, secretAccessKey
	}

	// copyAWSSecretToNamespace creates a secret in the test namespace with AWS secret access key
	copyAWSSecretToNamespace := func(ctx context.Context, namespace, secretName, secretKey string, secretAccessKey []byte) {
		By(fmt.Sprintf("copying AWS secret access key to namespace %s", namespace))
		awsSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: namespace,
			},
			Data: map[string][]byte{
				secretKey: secretAccessKey,
			},
		}
		_, err := loader.KubeClient.CoreV1().Secrets(namespace).Create(ctx, awsSecret, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("failed to create secret %s", secretName))
	}

	// setupAmbientAWSCredentials sets up ambient AWS credentials via CredentialsRequest and subscription patch
	setupAmbientAWSCredentials := func(ctx context.Context) {
		By("creating CredentialsRequest object for AWS")
		loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "credentials", "credentialsrequest_aws.yaml"), "")
		DeferCleanup(func() {
			loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "credentials", "credentialsrequest_aws.yaml"), "")
		})

		By("waiting for cloud secret to be available")
		err := wait.PollUntilContextTimeout(context.TODO(), slowPollInterval, highTimeout, true, func(context.Context) (bool, error) {
			_, err := loader.KubeClient.CoreV1().Secrets("cert-manager").Get(ctx, "aws-creds", metav1.GetOptions{})
			return err == nil, nil
		})
		Expect(err).NotTo(HaveOccurred(), "timeout waiting for AWS credentials secret")

		By("setting cloud credential secret name in subscription object")
		err = patchSubscriptionWithEnvVars(ctx, loader, map[string]string{
			"CLOUD_CREDENTIALS_SECRET_NAME": "aws-creds",
		})
		Expect(err).NotTo(HaveOccurred(), "failed to patch subscription with env vars")

		DeferCleanup(func(cleanupCtx context.Context) {
			By("removing 'CLOUD_CREDENTIALS_SECRET_NAME' from subscription")
			if err := patchSubscriptionWithEnvVars(cleanupCtx, loader, map[string]string{
				"CLOUD_CREDENTIALS_SECRET_NAME": "",
			}); err != nil {
				fmt.Fprintf(GinkgoWriter, "failed to remove env var from subscription during cleanup: %v\n", err)
				return
			}
		})
	}

	// getGCPProjectID retrieves GCP project ID from Infrastructure object
	getGCPProjectID := func(ctx context.Context) []byte {
		By("getting GCP project ID from Infrastructure object")
		infra, err := configClient.Infrastructures().Get(ctx, "cluster", metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to get Infrastructure object")
		projectID := []byte(infra.Status.PlatformStatus.GCP.ProjectID)
		Expect(projectID).NotTo(BeEmpty(), "GCP project ID should not be empty")

		return projectID
	}

	// getGCPCredentials retrieves GCP service account from the kube-system namespace
	getGCPCredentials := func(ctx context.Context) []byte {
		By("obtaining GCP service account from kube-system namespace")
		gcpCredsSecret, err := loader.KubeClient.CoreV1().Secrets("kube-system").Get(ctx, "gcp-credentials", metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to get GCP credentials from kube-system")

		serviceAccount := gcpCredsSecret.Data["service_account.json"]
		Expect(serviceAccount).NotTo(BeEmpty(), "service_account.json should not be empty")

		return serviceAccount
	}

	// copyGCPSecretToNamespace creates a secret in the test namespace with GCP service account key
	copyGCPSecretToNamespace := func(ctx context.Context, namespace, secretName, secretKey string, serviceAccount []byte) {
		By(fmt.Sprintf("copying GCP service account key to namespace %s", namespace))
		gcpSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: namespace,
			},
			Data: map[string][]byte{
				secretKey: serviceAccount,
			},
		}
		_, err := loader.KubeClient.CoreV1().Secrets(namespace).Create(ctx, gcpSecret, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("failed to create secret %s", secretName))
	}

	// setupAmbientGCPCredentials sets up ambient GCP credentials via CredentialsRequest and subscription patch
	setupAmbientGCPCredentials := func(ctx context.Context) {
		By("creating CredentialsRequest object for GCP")
		loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "credentials", "credentialsrequest_gcp.yaml"), "")
		DeferCleanup(func() {
			loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "credentials", "credentialsrequest_gcp.yaml"), "")
		})

		By("waiting for cloud secret to be available")
		err := wait.PollUntilContextTimeout(context.TODO(), slowPollInterval, highTimeout, true, func(context.Context) (bool, error) {
			_, err := loader.KubeClient.CoreV1().Secrets("cert-manager").Get(ctx, "gcp-credentials", metav1.GetOptions{})
			return err == nil, nil
		})
		Expect(err).NotTo(HaveOccurred(), "timeout waiting for GCP credentials secret")

		By("setting cloud credential secret name in subscription object")
		err = patchSubscriptionWithEnvVars(ctx, loader, map[string]string{
			"CLOUD_CREDENTIALS_SECRET_NAME": "gcp-credentials",
		})
		Expect(err).NotTo(HaveOccurred(), "failed to patch subscription with env vars")

		DeferCleanup(func(cleanupCtx context.Context) {
			By("removing 'CLOUD_CREDENTIALS_SECRET_NAME' from subscription")
			if err := patchSubscriptionWithEnvVars(cleanupCtx, loader, map[string]string{
				"CLOUD_CREDENTIALS_SECRET_NAME": "",
			}); err != nil {
				fmt.Fprintf(GinkgoWriter, "failed to remove env var from subscription during cleanup: %v\n", err)
				return
			}
		})
	}

	// removeGCPMemberFromPolicy removes a member from a role binding in a GCP IAM policy
	removeGCPMemberFromPolicy := func(policy *gcpcrm.Policy, role, member string) {
		bindingIndex, memberIndex := -1, -1
		for bIdx := range policy.Bindings {
			if policy.Bindings[bIdx].Role != role {
				continue
			}
			bindingIndex = bIdx
			for mIdx := range policy.Bindings[bindingIndex].Members {
				if policy.Bindings[bindingIndex].Members[mIdx] != member {
					continue
				}
				memberIndex = mIdx
				break
			}
		}

		if bindingIndex == -1 {
			log.Printf("Role not found in policy: role=%s", role)
			return
		}
		if memberIndex == -1 {
			log.Printf("Member not found in role binding: role=%s, member=%s", role, member)
			return
		}

		// Remove member from binding
		members := append(policy.Bindings[bindingIndex].Members[:memberIndex], policy.Bindings[bindingIndex].Members[memberIndex+1:]...)
		policy.Bindings[bindingIndex].Members = members

		// Remove binding if no members left
		if len(members) == 0 {
			policy.Bindings = append(policy.Bindings[:bindingIndex], policy.Bindings[bindingIndex+1:]...)
		}
	}

	// updateGCPIamPolicyBinding adds or removes an IAM policy binding for a GCP project
	updateGCPIamPolicyBinding := func(crmService *gcpcrm.Service, projectID, role, member string, add bool) error {
		return wait.PollUntilContextTimeout(context.TODO(), fastPollInterval, lowTimeout, true, func(context.Context) (bool, error) {
			policy, err := crmService.Projects.GetIamPolicy(projectID, &gcpcrm.GetIamPolicyRequest{}).Do()
			if err != nil {
				log.Printf("Error getting IAM policy: %v", err)
				return false, nil
			}

			if add {
				policy.Bindings = append(policy.Bindings, &gcpcrm.Binding{
					Role:    role,
					Members: []string{member},
				})
			} else {
				removeGCPMemberFromPolicy(policy, role, member)
			}

			_, err = crmService.Projects.SetIamPolicy(projectID, &gcpcrm.SetIamPolicyRequest{Policy: policy}).Do()
			if err != nil {
				log.Printf("Error setting IAM policy: %v", err)
				return false, nil
			}
			log.Printf("IAM policy updated successfully")
			return true, nil
		})
	}

	// getAzureCredentials retrieves Azure Service Principal credentials from the kube-system namespace
	getAzureCredentials := func(ctx context.Context) (clientID, clientSecret, tenantID []byte) {
		By("obtaining Azure credentials from kube-system namespace")
		azureCredsSecret, err := loader.KubeClient.CoreV1().Secrets("kube-system").Get(ctx, "azure-credentials", metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to get Azure credentials from kube-system")

		clientID = azureCredsSecret.Data["azure_client_id"]
		clientSecret = azureCredsSecret.Data["azure_client_secret"]
		tenantID = azureCredsSecret.Data["azure_tenant_id"]

		Expect(clientID).NotTo(BeEmpty(), "azure_client_id should not be empty")
		Expect(clientSecret).NotTo(BeEmpty(), "azure_client_secret should not be empty")
		Expect(tenantID).NotTo(BeEmpty(), "azure_tenant_id should not be empty")

		return clientID, clientSecret, tenantID
	}

	// getAzureDNSZoneInfo extracts the subscription ID, resource group, and zone name
	// from the OpenShift DNS config object's publicZone.ID, which is an Azure resource ID:
	// /subscriptions/<sub>/resourceGroups/<rg>/providers/Microsoft.Network/dnszones/<zone>
	getAzureDNSZoneInfo := func(ctx context.Context) (subscriptionID, resourceGroupName, hostedZoneName string) {
		By("getting Azure DNS zone info from DNS config object")
		dns, err := configClient.DNSes().Get(ctx, "cluster", metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred(), "failed to get DNS config object")
		Expect(dns.Spec.PublicZone).NotTo(BeNil(), "DNS publicZone should not be nil")

		zoneID := dns.Spec.PublicZone.ID
		Expect(zoneID).NotTo(BeEmpty(), "DNS publicZone ID should not be empty")

		parts := strings.Split(zoneID, "/")
		// Expected: /subscriptions/<sub>/resourceGroups/<rg>/providers/Microsoft.Network/dnszones/<zone>
		Expect(len(parts)).To(BeNumerically(">=", 9), "unexpected Azure resource ID format: %s", zoneID)
		Expect(parts[7]).To(Equal("dnszones"), "expected a DNS zone resource ID, got: %s", zoneID)
		subscriptionID = parts[2]
		resourceGroupName = parts[4]
		hostedZoneName = parts[8]

		Expect(subscriptionID).NotTo(BeEmpty(), "subscription ID should not be empty")
		Expect(resourceGroupName).NotTo(BeEmpty(), "DNS zone resource group should not be empty")
		Expect(hostedZoneName).NotTo(BeEmpty(), "hosted zone name should not be empty")

		return subscriptionID, resourceGroupName, hostedZoneName
	}

	// setupCCOAzureCredentials creates a CredentialsRequest for Azure with fine-grained
	// DNS Zone Contributor permissions and returns the CCO-provisioned credentials.
	// Note: Unlike setupAmbientAWSCredentials/setupAmbientGCPCredentials, this does NOT patch
	// the subscription with 'CLOUD_CREDENTIALS_SECRET_NAME' because the operator does not yet
	// support mounting Azure credentials into the cert-manager pod. Once Azure support is added to
	// 'withCloudCredentials' in credentials_request.go, it can be adapted to follow the AWS/GCP pattern.
	setupCCOAzureCredentials := func(ctx context.Context) (clientID, clientSecret, tenantID []byte) {
		By("creating CredentialsRequest object for Azure")
		loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "credentials", "credentialsrequest_azure.yaml"), "")
		DeferCleanup(func() {
			loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "credentials", "credentialsrequest_azure.yaml"), "")
		})

		By("waiting for cloud secret to be available")
		var ccoSecret *corev1.Secret
		err := wait.PollUntilContextTimeout(context.TODO(), slowPollInterval, highTimeout, true, func(context.Context) (bool, error) {
			var getErr error
			ccoSecret, getErr = loader.KubeClient.CoreV1().Secrets("cert-manager").Get(ctx, "azure-credentials", metav1.GetOptions{})
			return getErr == nil, nil
		})
		Expect(err).NotTo(HaveOccurred(), "timeout waiting for Azure credentials secret")

		By("reading CCO-provisioned credentials")
		clientID = ccoSecret.Data["azure_client_id"]
		clientSecret = ccoSecret.Data["azure_client_secret"]
		tenantID = ccoSecret.Data["azure_tenant_id"]
		Expect(clientID).NotTo(BeEmpty(), "azure_client_id should not be empty")
		Expect(clientSecret).NotTo(BeEmpty(), "azure_client_secret should not be empty")
		Expect(tenantID).NotTo(BeEmpty(), "azure_tenant_id should not be empty")

		return clientID, clientSecret, tenantID
	}

	// copyAzureSecretToNamespace creates a secret in the test namespace with Azure client secret
	copyAzureSecretToNamespace := func(ctx context.Context, namespace, secretName, secretKey string, clientSecret []byte) {
		By(fmt.Sprintf("copying Azure client secret to namespace %s", namespace))
		azureSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: namespace,
			},
			Data: map[string][]byte{
				secretKey: clientSecret,
			},
		}
		_, err := loader.KubeClient.CoreV1().Secrets(namespace).Create(ctx, azureSecret, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("failed to create secret %s", secretName))
	}

	Context("with AWS Route53", Label("Platform:AWS", "CredentialsMode:Mint"), func() {

		It("should obtain a valid certificate using explicit credentials", func() {

			// Get AWS credentials and region
			accessKeyID, secretAccessKey := getAWSCredentials(ctx)
			region := getAWSRegion(ctx)

			// Copy secret to test namespace
			secretName := "aws-secret"
			secretKey := "aws_secret_access_key"
			copyAWSSecretToNamespace(ctx, ns.Name, secretName, secretKey, secretAccessKey)

			By("creating ACME issuer with Route53 DNS-01 solver using explicit credentials")
			issuerName := "letsencrypt-dns01"
			solver := acmev1.ACMEChallengeSolver{
				DNS01: &acmev1.ACMEChallengeSolverDNS01{
					Route53: &acmev1.ACMEIssuerDNS01ProviderRoute53{
						Region:      string(region),
						AccessKeyID: string(accessKeyID),
						SecretAccessKey: certmanagermetav1.SecretKeySelector{
							LocalObjectReference: certmanagermetav1.LocalObjectReference{
								Name: secretName,
							},
							Key: secretKey,
						},
					},
				},
			}
			issuer := createACMEIssuer(issuerName, solver)
			_, err := certmanagerClient.CertmanagerV1().Issuers(ns.Name).Create(ctx, issuer, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create issuer")

			By("waiting for issuer to become ready")
			err = waitForIssuerReadiness(ctx, issuerName, ns.Name)
			Expect(err).NotTo(HaveOccurred(), "timeout waiting for issuer to become Ready")

			// Create and verify certificate
			certName := "letsencrypt-cert"
			dnsName := fmt.Sprintf("adre-%s.%s", randomStr(3), appsDomain) // acronym for "ACME DNS01 Route53 Explicit"
			createAndVerifyACMECertificate(ctx, certName, ns.Name, dnsName, issuerName, "Issuer")
		})

		It("should obtain a valid certificate using ambient credentials with ClusterIssuer", func() {

			// Get AWS credentials and region
			setupAmbientAWSCredentials(ctx)
			region := getAWSRegion(ctx)

			By("creating ACME ClusterIssuer with Route53 DNS-01 solver using ambient credentials")
			clusterIssuerName := "letsencrypt-dns01-ambient"
			solver := acmev1.ACMEChallengeSolver{
				DNS01: &acmev1.ACMEChallengeSolverDNS01{
					Route53: &acmev1.ACMEIssuerDNS01ProviderRoute53{
						Region: string(region),
					},
				},
			}
			clusterIssuer := createACMEClusterIssuer(clusterIssuerName, solver)
			_, err := certmanagerClient.CertmanagerV1().ClusterIssuers().Create(ctx, clusterIssuer, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create ClusterIssuer")
			DeferCleanup(func() {
				certmanagerClient.CertmanagerV1().ClusterIssuers().Delete(ctx, clusterIssuerName, metav1.DeleteOptions{})
			})

			By("waiting for ClusterIssuer to become ready")
			err = waitForClusterIssuerReadiness(ctx, clusterIssuerName)
			Expect(err).NotTo(HaveOccurred(), "timeout waiting for ClusterIssuer to become Ready")

			// Create and verify certificate
			certName := "letsencrypt-cert"
			dnsName := fmt.Sprintf("adra-%s.%s", randomStr(3), appsDomain) // acronym for "ACME DNS01 Route53 Ambient"
			createAndVerifyACMECertificate(ctx, certName, ns.Name, dnsName, clusterIssuerName, "ClusterIssuer")
		})

		It("should obtain a valid certificate using ambient credentials with Issuer", func() {

			// Get AWS credentials and region
			setupAmbientAWSCredentials(ctx)
			region := getAWSRegion(ctx)

			By("creating ACME Issuer with Route53 DNS-01 solver using ambient credentials")
			issuerName := "letsencrypt-dns01-ambient"
			solver := acmev1.ACMEChallengeSolver{
				DNS01: &acmev1.ACMEChallengeSolverDNS01{
					Route53: &acmev1.ACMEIssuerDNS01ProviderRoute53{
						Region: string(region),
					},
				},
			}
			issuer := createACMEIssuer(issuerName, solver)
			_, err := certmanagerClient.CertmanagerV1().Issuers(ns.Name).Create(ctx, issuer, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create Issuer")

			By("waiting for issuer to become ready")
			err = waitForIssuerReadiness(ctx, issuerName, ns.Name)
			Expect(err).NotTo(HaveOccurred(), "timeout waiting for issuer to become Ready")

			// Create and verify certificate
			certName := "letsencrypt-cert"
			dnsName := fmt.Sprintf("adra-%s.%s", randomStr(3), appsDomain) // acronym for "ACME DNS01 Route53 Ambient"
			createAndVerifyACMECertificate(ctx, certName, ns.Name, dnsName, issuerName, "Issuer")
		})

		It("should obtain a valid certificate when no hosted zone overlap", func() {

			// Get AWS credentials and region
			accessKeyID, secretAccessKey := getAWSCredentials(ctx)
			region := getAWSRegion(ctx)

			// Copy secret to cert-manager namespace
			secretName := "aws-secret-overlapped"
			secretKey := "aws_secret_access_key"
			copyAWSSecretToNamespace(ctx, "cert-manager", secretName, secretKey, secretAccessKey)
			DeferCleanup(func() {
				loader.KubeClient.CoreV1().Secrets("cert-manager").Delete(ctx, secretName, metav1.DeleteOptions{})
			})

			By("calculating parent domain from base domain")
			parts := strings.Split(baseDomain, ".")
			Expect(len(parts)).To(BeNumerically(">", 1), "cannot derive parent domain from base domain")
			parentDomain := strings.Join(parts[1:], ".")

			By("getting Route53 hosted zone ID from DNS object")
			dns, err := configClient.DNSes().Get(ctx, "cluster", metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to get DNS object")
			hostedZoneID := dns.Spec.PublicZone.ID
			Expect(hostedZoneID).NotTo(BeEmpty(), "Route53 hosted zone ID should not be empty")

			By("creating ACME ClusterIssuer with Route53 DNS-01 solver for overlapping hosted zone")
			clusterIssuerName := "letsencrypt-dns01-overlapped"
			solver := acmev1.ACMEChallengeSolver{
				Selector: &acmev1.CertificateDNSNameSelector{
					DNSZones: []string{parentDomain},
				},
				DNS01: &acmev1.ACMEChallengeSolverDNS01{
					Route53: &acmev1.ACMEIssuerDNS01ProviderRoute53{
						Region:      string(region),
						AccessKeyID: string(accessKeyID),
						SecretAccessKey: certmanagermetav1.SecretKeySelector{
							LocalObjectReference: certmanagermetav1.LocalObjectReference{
								Name: secretName,
							},
							Key: secretKey,
						},
						HostedZoneID: hostedZoneID,
					},
				},
			}
			clusterIssuer := createACMEClusterIssuer(clusterIssuerName, solver)
			_, err = certmanagerClient.CertmanagerV1().ClusterIssuers().Create(ctx, clusterIssuer, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create ClusterIssuer")
			DeferCleanup(func() {
				certmanagerClient.CertmanagerV1().ClusterIssuers().Delete(ctx, clusterIssuerName, metav1.DeleteOptions{})
			})

			By("waiting for ClusterIssuer to become ready")
			err = waitForClusterIssuerReadiness(ctx, clusterIssuerName)
			Expect(err).NotTo(HaveOccurred(), "timeout waiting for ClusterIssuer to become Ready")

			// Create and verify certificate
			certName := "letsencrypt-cert-overlapped"
			dnsName := fmt.Sprintf("adrohz-%s.%s", randomStr(3), parentDomain) // acronym for "ACME DNS01 Route53 Overlapped Hosted Zone"
			createAndVerifyACMECertificate(ctx, certName, ns.Name, dnsName, clusterIssuerName, "ClusterIssuer")
		})

		It("should obtain a valid certificate with DNS-over-HTTPS", func() {

			// Get AWS credentials and region
			accessKeyID, secretAccessKey := getAWSCredentials(ctx)
			region := getAWSRegion(ctx)

			// Copy secret to cert-manager namespace
			secretName := "aws-secret-doh"
			secretKey := "aws_secret_access_key"
			copyAWSSecretToNamespace(ctx, "cert-manager", secretName, secretKey, secretAccessKey)
			DeferCleanup(func() {
				loader.KubeClient.CoreV1().Secrets("cert-manager").Delete(ctx, secretName, metav1.DeleteOptions{})
			})

			By("creating ACME ClusterIssuer with Route53 DNS-01 solver for DNS-over-HTTPS")
			clusterIssuerName := "letsencrypt-dns01-doh"
			solver := acmev1.ACMEChallengeSolver{
				DNS01: &acmev1.ACMEChallengeSolverDNS01{
					Route53: &acmev1.ACMEIssuerDNS01ProviderRoute53{
						Region:      string(region),
						AccessKeyID: string(accessKeyID),
						SecretAccessKey: certmanagermetav1.SecretKeySelector{
							LocalObjectReference: certmanagermetav1.LocalObjectReference{
								Name: secretName,
							},
							Key: secretKey,
						},
					},
				},
			}
			clusterIssuer := createACMEClusterIssuer(clusterIssuerName, solver)
			_, err := certmanagerClient.CertmanagerV1().ClusterIssuers().Create(ctx, clusterIssuer, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create ClusterIssuer")
			DeferCleanup(func() {
				certmanagerClient.CertmanagerV1().ClusterIssuers().Delete(ctx, clusterIssuerName, metav1.DeleteOptions{})
			})

			By("waiting for ClusterIssuer to become ready")
			err = waitForClusterIssuerReadiness(ctx, clusterIssuerName)
			Expect(err).NotTo(HaveOccurred(), "timeout waiting for ClusterIssuer to become Ready")

			// Create and verify certificate
			certName := "letsencrypt-cert-doh"
			dnsName := fmt.Sprintf("adrdoh-%s.%s", randomStr(3), appsDomain) // acronym for "ACME DNS01 Route53 DNS-over-HTTPS"
			createAndVerifyACMECertificate(ctx, certName, ns.Name, dnsName, clusterIssuerName, "ClusterIssuer")
		})
	})

	Context("with AWS Route53 in STS environment", Label("Platform:AWS", "CredentialsMode:Manual"), func() {
		var region string
		var roleARN string

		BeforeAll(func() {
			By("verifying cluster is STS-enabled")
			isSTS, err := isSTSCluster(ctx, oseOperatorClient, configClient)
			Expect(err).NotTo(HaveOccurred())
			if !isSTS {
				Skip("Test requires AWS Security Token Service enabled")
			}

			By("setting up AWS authentication environment variable from credentials file")
			if os.Getenv("OPENSHIFT_CI") == "true" {
				clusterProfileDir := os.Getenv("CLUSTER_PROFILE_DIR")
				Expect(clusterProfileDir).NotTo(BeEmpty(), "CLUSTER_PROFILE_DIR should exist when running in OpenShift CI")
				os.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(clusterProfileDir, ".awscred"))
			} else {
				Expect(os.Getenv("AWS_SHARED_CREDENTIALS_FILE")).NotTo(BeEmpty(), "AWS_SHARED_CREDENTIALS_FILE must be set when running locally")
			}

			// Get AWS region and determinate partition
			region = string(getAWSRegion(ctx))
			partition := "aws"
			if strings.HasPrefix(region, "us-gov") {
				partition = "aws-us-gov"
			}

			// Get OIDC provider
			By("getting OIDC provider from Authentication object")
			authConfig, err := configClient.Authentications().Get(ctx, "cluster", metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to get Authentication object")
			oidcProvider := strings.TrimPrefix(authConfig.Spec.ServiceAccountIssuer, "https://")
			Expect(oidcProvider).NotTo(BeEmpty(), "OIDC provider not found in Authentication object")

			By("preparing AWS IAM and STS clients")
			awsConfig, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
			Expect(err).NotTo(HaveOccurred())

			iamClient := iam.NewFromConfig(awsConfig)

			stsClient := sts.NewFromConfig(awsConfig)
			callerIdentity, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
			Expect(err).NotTo(HaveOccurred())
			accountID := aws.ToString(callerIdentity.Account)

			By("creating IAM role with trust policy for cert-manager ServiceAccount")
			randomSuffix := randomStr(4)
			roleName := "e2e-cert-manager-dns01-" + randomSuffix
			policyName := "e2e-cert-manager-dns01-" + randomSuffix

			trustPolicy := fmt.Sprintf(`{
				"Version": "2012-10-17",
				"Statement": [{
					"Effect": "Allow",
					"Principal": {
						"Federated": "arn:%s:iam::%s:oidc-provider/%s"
					},
					"Action": "sts:AssumeRoleWithWebIdentity",
					"Condition": {
						"StringEquals": {
							"%s:sub": ["system:serviceaccount:cert-manager:cert-manager"]
						}
					}
				}]
			}`, partition, accountID, oidcProvider, oidcProvider)

			createRoleOutput, err := iamClient.CreateRole(ctx, &iam.CreateRoleInput{
				AssumeRolePolicyDocument: aws.String(trustPolicy),
				RoleName:                 aws.String(roleName),
			})
			Expect(err).NotTo(HaveOccurred())
			roleARN = aws.ToString(createRoleOutput.Role.Arn)

			DeferCleanup(func(ctx context.Context, client *iam.Client, name string) {
				By("Cleaning up IAM role")
				_, err := client.DeleteRole(ctx, &iam.DeleteRoleInput{RoleName: aws.String(name)})
				if err != nil {
					fmt.Fprintf(GinkgoWriter, "failed to delete IAM role during cleanup: %v\n", err)
				}
			}, iamClient, roleName)

			By("creating IAM policy for Route53 permissions")
			dnsPolicy := fmt.Sprintf(`{
				"Version": "2012-10-17",
				"Statement": [
					{
						"Effect": "Allow",
						"Action": "route53:GetChange",
						"Resource": "arn:%s:route53:::change/*"
					},
					{
						"Effect": "Allow",
						"Action": [
							"route53:ChangeResourceRecordSets",
							"route53:ListResourceRecordSets"
						],
						"Resource": "arn:%s:route53:::hostedzone/*"
					},
					{
						"Effect": "Allow",
						"Action": "route53:ListHostedZonesByName",
						"Resource": "*"
					}
				]
			}`, partition, partition)

			createPolicyOutput, err := iamClient.CreatePolicy(ctx, &iam.CreatePolicyInput{
				PolicyDocument: aws.String(dnsPolicy),
				PolicyName:     aws.String(policyName),
			})
			Expect(err).NotTo(HaveOccurred())
			policyARN := aws.ToString(createPolicyOutput.Policy.Arn)

			DeferCleanup(func(ctx context.Context, client *iam.Client, arn string) {
				By("Cleaning up IAM policy")
				_, err := client.DeletePolicy(ctx, &iam.DeletePolicyInput{PolicyArn: aws.String(arn)})
				if err != nil {
					fmt.Fprintf(GinkgoWriter, "failed to delete IAM policy during cleanup: %v\n", err)
				}
			}, iamClient, policyARN)

			By("attaching IAM policy to role")
			_, err = iamClient.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{
				PolicyArn: aws.String(policyARN),
				RoleName:  aws.String(roleName),
			})
			Expect(err).NotTo(HaveOccurred())

			DeferCleanup(func(ctx context.Context, client *iam.Client, arn, role string) {
				By("Detaching IAM policy from role")
				_, err := client.DetachRolePolicy(ctx, &iam.DetachRolePolicyInput{
					PolicyArn: aws.String(arn),
					RoleName:  aws.String(role),
				})
				if err != nil {
					fmt.Fprintf(GinkgoWriter, "failed to detach IAM policy during cleanup: %v\n", err)
				}
			}, iamClient, policyARN, roleName)
		})

		It("should obtain a valid certificate using ambient credentials through pod-identity-webhook", func() {

			By("annotating cert-manager ServiceAccount with IAM role ARN")
			sa, err := loader.KubeClient.CoreV1().ServiceAccounts("cert-manager").Get(ctx, "cert-manager", metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to get cert-manager ServiceAccount")

			if sa.Annotations == nil {
				sa.Annotations = make(map[string]string)
			}
			sa.Annotations["eks.amazonaws.com/role-arn"] = roleARN

			_, err = loader.KubeClient.CoreV1().ServiceAccounts("cert-manager").Update(ctx, sa, metav1.UpdateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to update cert-manager ServiceAccount with role annotation")

			DeferCleanup(func(ctx context.Context) {
				By("Removing IAM role annotation from ServiceAccount")
				sa, err := loader.KubeClient.CoreV1().ServiceAccounts("cert-manager").Get(ctx, "cert-manager", metav1.GetOptions{})
				if err != nil {
					fmt.Fprintf(GinkgoWriter, "failed to get ServiceAccount during cleanup: %v\n", err)
					return
				}
				if sa.Annotations != nil {
					delete(sa.Annotations, "eks.amazonaws.com/role-arn")
					_, err = loader.KubeClient.CoreV1().ServiceAccounts("cert-manager").Update(ctx, sa, metav1.UpdateOptions{})
					if err != nil {
						fmt.Fprintf(GinkgoWriter, "failed to update ServiceAccount during cleanup: %v\n", err)
					}
				}
				By("Restarting cert-manager pods to remove role assumption")
				err = loader.KubeClient.CoreV1().Pods("cert-manager").DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
					LabelSelector: "app=cert-manager",
				})
				if err != nil {
					fmt.Fprintf(GinkgoWriter, "failed to delete pods during cleanup: %v\n", err)
				}
			})

			By("restarting cert-manager pods to pick up role annotation")
			err = loader.KubeClient.CoreV1().Pods("cert-manager").DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
				LabelSelector: "app=cert-manager",
			})
			Expect(err).NotTo(HaveOccurred(), "failed to delete cert-manager pods")

			By("waiting for cert-manager deployment to rollout")
			err = waitForDeploymentRollout(ctx, "cert-manager", "cert-manager", 2*time.Minute)
			Expect(err).NotTo(HaveOccurred(), "timeout waiting for cert-manager deployment rollout")

			By("creating ACME ClusterIssuer with Route53 DNS-01 solver using ambient IRSA credentials")
			clusterIssuerName := "letsencrypt-dns01-irsa"
			solver := acmev1.ACMEChallengeSolver{
				DNS01: &acmev1.ACMEChallengeSolverDNS01{
					Route53: &acmev1.ACMEIssuerDNS01ProviderRoute53{
						Region: region,
					},
				},
			}
			clusterIssuer := createACMEClusterIssuer(clusterIssuerName, solver)
			_, err = certmanagerClient.CertmanagerV1().ClusterIssuers().Create(ctx, clusterIssuer, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create ClusterIssuer")
			DeferCleanup(func(ctx context.Context) {
				certmanagerClient.CertmanagerV1().ClusterIssuers().Delete(ctx, clusterIssuerName, metav1.DeleteOptions{})
			})

			By("waiting for ClusterIssuer to become ready")
			err = waitForClusterIssuerReadiness(ctx, clusterIssuerName)
			Expect(err).NotTo(HaveOccurred(), "timeout waiting for ClusterIssuer to become Ready")

			// Create and verify certificate
			certName := "letsencrypt-cert-irsa"
			dnsName := fmt.Sprintf("adri-%s.%s", randomStr(3), appsDomain) // acronym for "ACME DNS01 Route53 IRSA"
			createAndVerifyACMECertificate(ctx, certName, ns.Name, dnsName, clusterIssuerName, "ClusterIssuer")
		})

		It("should obtain a valid certificate using ambient credentials through manually patched secret", func() {

			By("creating STS config secret manually with AWS credentials file format")
			secretName := "aws-sts-creds"
			credContent := fmt.Sprintf("[default]\nsts_regional_endpoints = regional\nrole_arn = %s\nweb_identity_token_file = /var/run/secrets/openshift/serviceaccount/token\nregion = %s", roleARN, region)
			stsSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: "cert-manager",
				},
				StringData: map[string]string{
					"credentials": credContent,
				},
			}
			_, err := loader.KubeClient.CoreV1().Secrets("cert-manager").Create(ctx, stsSecret, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create STS credential secret")

			DeferCleanup(func(ctx context.Context) {
				By("Deleting manually created STS credential secret")
				err := loader.KubeClient.CoreV1().Secrets("cert-manager").Delete(ctx, secretName, metav1.DeleteOptions{})
				if err != nil {
					fmt.Fprintf(GinkgoWriter, "failed to delete secret during cleanup: %v\n", err)
				}
			})

			By("patching subscription to inject 'CLOUD_CREDENTIALS_SECRET_NAME' env var")
			err = patchSubscriptionWithEnvVars(ctx, loader, map[string]string{
				"CLOUD_CREDENTIALS_SECRET_NAME": secretName,
			})
			Expect(err).NotTo(HaveOccurred(), "failed to patch subscription with 'CLOUD_CREDENTIALS_SECRET_NAME'")

			DeferCleanup(func(ctx context.Context) {
				By("Removing 'CLOUD_CREDENTIALS_SECRET_NAME' from subscription")
				if err := patchSubscriptionWithEnvVars(ctx, loader, map[string]string{
					"CLOUD_CREDENTIALS_SECRET_NAME": "",
				}); err != nil {
					fmt.Fprintf(GinkgoWriter, "failed to remove env var from subscription during cleanup: %v\n", err)
					return
				}
			})

			By("waiting for cert-manager deployment to rollout with new config")
			err = waitForDeploymentRollout(ctx, "cert-manager", "cert-manager", 2*time.Minute)
			Expect(err).NotTo(HaveOccurred(), "timeout waiting for cert-manager deployment rollout")

			By("creating ACME ClusterIssuer with Route53 DNS-01 solver using ambient STS credentials")
			clusterIssuerName := "letsencrypt-dns01-sts-manual"
			solver := acmev1.ACMEChallengeSolver{
				DNS01: &acmev1.ACMEChallengeSolverDNS01{
					Route53: &acmev1.ACMEIssuerDNS01ProviderRoute53{
						Region: region,
					},
				},
			}
			clusterIssuer := createACMEClusterIssuer(clusterIssuerName, solver)
			_, err = certmanagerClient.CertmanagerV1().ClusterIssuers().Create(ctx, clusterIssuer, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create ClusterIssuer")
			DeferCleanup(func(ctx context.Context) {
				certmanagerClient.CertmanagerV1().ClusterIssuers().Delete(ctx, clusterIssuerName, metav1.DeleteOptions{})
			})

			By("waiting for ClusterIssuer to become ready")
			err = waitForClusterIssuerReadiness(ctx, clusterIssuerName)
			Expect(err).NotTo(HaveOccurred(), "timeout waiting for ClusterIssuer to become Ready")

			certName := "letsencrypt-cert-sts-manual"
			dnsName := fmt.Sprintf("adrsm-%s.%s", randomStr(3), appsDomain) // acronym for "ACME DNS01 Route53 STS Manual"
			createAndVerifyACMECertificate(ctx, certName, ns.Name, dnsName, clusterIssuerName, "ClusterIssuer")
		})
	})

	Context("with Google CloudDNS", Label("Platform:GCP", "CredentialsMode:Mint"), func() {

		It("should obtain a valid certificate using explicit credentials", func() {

			// Get GCP credentials and project ID
			serviceAccount := getGCPCredentials(ctx)
			projectID := getGCPProjectID(ctx)

			// Copy service account to test namespace
			secretName := "gcp-secret"
			secretKey := "gcp_service_account_key.json"
			copyGCPSecretToNamespace(ctx, ns.Name, secretName, secretKey, serviceAccount)

			By("creating ACME Issuer with CloudDNS DNS-01 solver using explicit credentials")
			issuerName := "letsencrypt-dns01"
			solver := acmev1.ACMEChallengeSolver{
				DNS01: &acmev1.ACMEChallengeSolverDNS01{
					CloudDNS: &acmev1.ACMEIssuerDNS01ProviderCloudDNS{
						Project: string(projectID),
						ServiceAccount: &certmanagermetav1.SecretKeySelector{
							LocalObjectReference: certmanagermetav1.LocalObjectReference{
								Name: secretName,
							},
							Key: secretKey,
						},
					},
				},
			}
			issuer := createACMEIssuer(issuerName, solver)
			_, err := certmanagerClient.CertmanagerV1().Issuers(ns.Name).Create(ctx, issuer, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create Issuer")

			By("waiting for Issuer to become ready")
			err = waitForIssuerReadiness(ctx, issuerName, ns.Name)
			Expect(err).NotTo(HaveOccurred(), "timeout waiting for Issuer to become Ready")

			// Create and verify certificate
			certName := "letsencrypt-cert"
			dnsName := fmt.Sprintf("adgce-%s.%s", randomStr(3), appsDomain) // acronym for "ACME DNS01 Google CloudDNS Explicit"
			createAndVerifyACMECertificate(ctx, certName, ns.Name, dnsName, issuerName, "Issuer")
		})

		It("should obtain a valid certificate using ambient credentials", func() {

			// Setup ambient GCP credentials
			setupAmbientGCPCredentials(ctx)

			// Get GCP project ID
			projectID := getGCPProjectID(ctx)

			By("creating ACME ClusterIssuer with CloudDNS DNS-01 solver using ambient credentials")
			clusterIssuerName := "acme-dns01-clouddns-ambient"
			solver := acmev1.ACMEChallengeSolver{
				DNS01: &acmev1.ACMEChallengeSolverDNS01{
					CloudDNS: &acmev1.ACMEIssuerDNS01ProviderCloudDNS{
						Project: string(projectID),
					},
				},
			}
			clusterIssuer := createACMEClusterIssuer(clusterIssuerName, solver)
			_, err := certmanagerClient.CertmanagerV1().ClusterIssuers().Create(ctx, clusterIssuer, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create ClusterIssuer")
			DeferCleanup(func(ctx context.Context) {
				certmanagerClient.CertmanagerV1().ClusterIssuers().Delete(ctx, clusterIssuerName, metav1.DeleteOptions{})
			})

			By("waiting for ClusterIssuer to become ready")
			err = waitForClusterIssuerReadiness(ctx, clusterIssuerName)
			Expect(err).NotTo(HaveOccurred(), "timeout waiting for ClusterIssuer to become Ready")

			// Create and verify certificate
			certName := "cert-with-acme-dns01-clouddns-ambient"
			dnsName := fmt.Sprintf("adgca-%s.%s", randomStr(3), appsDomain) // acronym for "ACME DNS01 Google CloudDNS Ambient"
			createAndVerifyACMECertificate(ctx, certName, ns.Name, dnsName, clusterIssuerName, "ClusterIssuer")
		})
	})

	Context("with Google CloudDNS in Workload Identity environment", Label("Platform:GCP", "CredentialsMode:Manual"), func() {

		It("should obtain a valid certificate using ambient credentials", func() {

			By("verifying cluster is STS-enabled")
			isSTS, err := isSTSCluster(ctx, oseOperatorClient, configClient)
			Expect(err).NotTo(HaveOccurred())
			if !isSTS {
				Skip("Test requires GCP Workload Identity enabled")
			}

			By("setting up GCP authentication environment variable from credentials file")
			if os.Getenv("OPENSHIFT_CI") == "true" {
				clusterProfileDir := os.Getenv("CLUSTER_PROFILE_DIR")
				Expect(clusterProfileDir).NotTo(BeEmpty(), "CLUSTER_PROFILE_DIR should exist when running in OpenShift CI")
				os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", filepath.Join(clusterProfileDir, "gce.json"))
			} else {
				Expect(os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")).NotTo(BeEmpty(), "GOOGLE_APPLICATION_CREDENTIALS must be set when running locally")
			}

			By("creating GCP IAM and CloudResourceManager clients")
			iamService, err := gcpiam.NewService(ctx)
			Expect(err).NotTo(HaveOccurred(), "failed to create GCP IAM service")
			crmService, err := gcpcrm.NewService(ctx)
			Expect(err).NotTo(HaveOccurred(), "failed to create GCP CloudResourceManager service")

			// Get GCP project ID
			projectID := string(getGCPProjectID(ctx))

			// Get OIDC provider
			By("getting OIDC provider from Authentication object")
			authConfig, err := configClient.Authentications().Get(ctx, "cluster", metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to get Authentication object")
			oidcProvider := strings.TrimPrefix(authConfig.Spec.ServiceAccountIssuer, "https://")
			Expect(oidcProvider).NotTo(BeEmpty(), "OIDC provider not found in Authentication object")

			By("deriving workload identity pool ID from OIDC provider")
			poolID := strings.TrimSuffix(strings.Split(oidcProvider, "/")[1], "-oidc")

			By("getting project number from GCP project")
			project, err := crmService.Projects.Get(projectID).Do()
			Expect(err).NotTo(HaveOccurred(), "failed to get GCP project")
			projectNumber := strconv.FormatInt(project.ProjectNumber, 10)

			By("constructing workload identity resource identifier")
			identifier := fmt.Sprintf("//iam.googleapis.com/projects/%s/locations/global/workloadIdentityPools/%s", projectNumber, poolID)

			By("creating GCP service account for DNS-01 solver")
			serviceAccountName := "e2e-cert-manager-dns01-" + randomStr(4)
			createSARequest := &gcpiam.CreateServiceAccountRequest{
				AccountId: serviceAccountName,
				ServiceAccount: &gcpiam.ServiceAccount{
					DisplayName: "dns01-solver service account for cert-manager",
				},
			}
			serviceAccount, err := iamService.Projects.ServiceAccounts.Create("projects/"+projectID, createSARequest).Do()
			Expect(err).NotTo(HaveOccurred(), "failed to create GCP service account")

			DeferCleanup(func(ctx context.Context, service *gcpiam.Service, name string) {
				By("Cleaning up GCP service account")
				_, err := service.Projects.ServiceAccounts.Delete(name).Do()
				if err != nil {
					fmt.Fprintf(GinkgoWriter, "failed to delete GCP service account during cleanup: %v\n", err)
				}
			}, iamService, serviceAccount.Name)

			By("waiting for GCP service account to be fully created and available")
			err = wait.PollUntilContextTimeout(context.TODO(), fastPollInterval, lowTimeout, true,
				func(context.Context) (bool, error) {
					_, err := iamService.Projects.ServiceAccounts.Get(serviceAccount.Name).Do()
					if err != nil {
						return false, nil
					}
					return true, nil
				},
			)
			Expect(err).NotTo(HaveOccurred(), "GCP service account should be created and available")

			By("adding IAM policy binding with role 'dns.admin' to GCP project")
			projectRole := "roles/dns.admin"
			projectMember := fmt.Sprintf("serviceAccount:%s", serviceAccount.Email)
			err = updateGCPIamPolicyBinding(crmService, projectID, projectRole, projectMember, true)
			Expect(err).NotTo(HaveOccurred(), "failed to add IAM policy binding")

			DeferCleanup(func(ctx context.Context, service *gcpcrm.Service, project, role, member string) {
				By("Removing IAM policy binding from GCP project")
				err := updateGCPIamPolicyBinding(service, project, role, member, false)
				if err != nil {
					fmt.Fprintf(GinkgoWriter, "failed to remove IAM policy binding during cleanup: %v\n", err)
				}
			}, crmService, projectID, projectRole, projectMember)

			By("linking cert-manager ServiceAccount to GCP service account with 'iam.workloadIdentityUser' role")
			resource := fmt.Sprintf("projects/-/serviceAccounts/%s", serviceAccount.Email)
			serviceAccountRole := "roles/iam.workloadIdentityUser"
			serviceAccountMember := fmt.Sprintf("principal:%s/subject/system:serviceaccount:cert-manager:cert-manager", identifier)

			saPolicy, err := iamService.Projects.ServiceAccounts.GetIamPolicy(resource).Do()
			Expect(err).NotTo(HaveOccurred(), "failed to get IAM policy for GCP service account")
			saPolicy.Bindings = append(saPolicy.Bindings, &gcpiam.Binding{
				Role:    serviceAccountRole,
				Members: []string{serviceAccountMember},
			})
			_, err = iamService.Projects.ServiceAccounts.SetIamPolicy(resource, &gcpiam.SetIamPolicyRequest{Policy: saPolicy}).Do()
			Expect(err).NotTo(HaveOccurred(), "failed to set IAM policy for GCP service account")

			By("creating GCP STS config secret with external_account credentials")
			secretName := "gcp-sts-creds"
			credContent := fmt.Sprintf(`{
				"type": "external_account",
				"audience": "%s/providers/%s",
				"subject_token_type": "urn:ietf:params:oauth:token-type:jwt",
				"token_url": "https://sts.googleapis.com/v1/token",
				"service_account_impersonation_url": "https://iamcredentials.googleapis.com/v1/%s:generateAccessToken",
				"credential_source": {
					"file": "/var/run/secrets/openshift/serviceaccount/token",
					"format": {
						"type": "text"
					}
				}
			}`, identifier, poolID, resource)

			stsSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: "cert-manager",
				},
				StringData: map[string]string{
					"service_account.json": credContent,
				},
			}
			_, err = loader.KubeClient.CoreV1().Secrets("cert-manager").Create(ctx, stsSecret, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create GCP STS credentials secret")

			DeferCleanup(func(ctx context.Context, namespace, name string) {
				By("Deleting GCP STS credentials secret")
				err := loader.KubeClient.CoreV1().Secrets(namespace).Delete(ctx, name, metav1.DeleteOptions{})
				if err != nil {
					fmt.Fprintf(GinkgoWriter, "failed to delete secret during cleanup: %v\n", err)
				}
			}, "cert-manager", secretName)

			By("patching subscription to inject 'CLOUD_CREDENTIALS_SECRET_NAME' env var")
			err = patchSubscriptionWithEnvVars(ctx, loader, map[string]string{
				"CLOUD_CREDENTIALS_SECRET_NAME": secretName,
			})
			Expect(err).NotTo(HaveOccurred(), "failed to patch subscription with CLOUD_CREDENTIALS_SECRET_NAME")

			DeferCleanup(func(ctx context.Context) {
				By("Removing 'CLOUD_CREDENTIALS_SECRET_NAME' from subscription")
				if err := patchSubscriptionWithEnvVars(ctx, loader, map[string]string{
					"CLOUD_CREDENTIALS_SECRET_NAME": "",
				}); err != nil {
					fmt.Fprintf(GinkgoWriter, "failed to remove env var from subscription during cleanup: %v\n", err)
					return
				}
			})

			By("waiting for cert-manager deployment to rollout with new credentials")
			err = waitForDeploymentRollout(ctx, "cert-manager", "cert-manager", 2*time.Minute)
			Expect(err).NotTo(HaveOccurred(), "timeout waiting for cert-manager deployment rollout")

			By("creating ACME ClusterIssuer with CloudDNS DNS-01 solver using ambient credentials")
			clusterIssuerName := "letsencrypt-clouddns-ambient"
			solver := acmev1.ACMEChallengeSolver{
				DNS01: &acmev1.ACMEChallengeSolverDNS01{
					CloudDNS: &acmev1.ACMEIssuerDNS01ProviderCloudDNS{
						Project: projectID,
					},
				},
			}
			clusterIssuer := createACMEClusterIssuer(clusterIssuerName, solver)
			_, err = certmanagerClient.CertmanagerV1().ClusterIssuers().Create(ctx, clusterIssuer, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create ClusterIssuer")
			DeferCleanup(func(ctx context.Context) {
				certmanagerClient.CertmanagerV1().ClusterIssuers().Delete(ctx, clusterIssuerName, metav1.DeleteOptions{})
			})

			By("waiting for ClusterIssuer to become ready")
			err = waitForClusterIssuerReadiness(ctx, clusterIssuerName)
			Expect(err).NotTo(HaveOccurred(), "timeout waiting for ClusterIssuer to become ready")

			// Create and verify certificate
			certName := "cert-with-clouddns-workload-identity"
			dnsName := fmt.Sprintf("adgcw-%s.%s", randomStr(3), appsDomain) // acronym for "ACME DNS01 Google CloudDNS Workload Identity"
			createAndVerifyACMECertificate(ctx, certName, ns.Name, dnsName, clusterIssuerName, "ClusterIssuer")
		})
	})

	Context("with Azure DNS", Label("Platform:Azure", "CredentialsMode:Mint"), func() {

		It("should obtain a valid certificate using explicit credentials through Service Principal", func() {

			// Get Azure Service Principal credentials (for authentication)
			clientID, clientSecret, tenantID := getAzureCredentials(ctx)

			// Get DNS zone subscription, resource group, and zone name from the DNS config object
			subscriptionID, resourceGroupName, hostedZoneName := getAzureDNSZoneInfo(ctx)

			// Copy client secret to test namespace
			secretName := "azure-client-secret"
			secretKey := "client-secret"
			copyAzureSecretToNamespace(ctx, ns.Name, secretName, secretKey, clientSecret)

			By("creating ACME Issuer with AzureDNS DNS-01 solver using explicit credentials")
			issuerName := "letsencrypt-dns01"
			solver := acmev1.ACMEChallengeSolver{
				DNS01: &acmev1.ACMEChallengeSolverDNS01{
					AzureDNS: &acmev1.ACMEIssuerDNS01ProviderAzureDNS{
						SubscriptionID:    subscriptionID,
						ResourceGroupName: resourceGroupName,
						HostedZoneName:    hostedZoneName,
						TenantID:          string(tenantID),
						ClientID:          string(clientID),
						ClientSecret: &certmanagermetav1.SecretKeySelector{
							LocalObjectReference: certmanagermetav1.LocalObjectReference{
								Name: secretName,
							},
							Key: secretKey,
						},
					},
				},
			}
			issuer := createACMEIssuer(issuerName, solver)
			_, err := certmanagerClient.CertmanagerV1().Issuers(ns.Name).Create(ctx, issuer, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create Issuer")

			By("waiting for Issuer to become ready")
			err = waitForIssuerReadiness(ctx, issuerName, ns.Name)
			Expect(err).NotTo(HaveOccurred(), "timeout waiting for Issuer to become Ready")

			// Create and verify certificate
			certName := "letsencrypt-cert"
			dnsName := fmt.Sprintf("adaze-%s.%s", randomStr(3), appsDomain) // acronym for "ACME DNS01 AzureDNS Explicit"
			createAndVerifyACMECertificate(ctx, certName, ns.Name, dnsName, issuerName, "Issuer")
		})

		It("should obtain a valid certificate using explicit credentials provisioned by CCO through Service Principal", func() {

			// Setup CCO-provisioned credentials for Azure (fine-grained DNS Zone Contributor)
			clientID, clientSecret, tenantID := setupCCOAzureCredentials(ctx)

			// Get DNS zone subscription, resource group, and zone name from the DNS config object
			subscriptionID, resourceGroupName, hostedZoneName := getAzureDNSZoneInfo(ctx)

			// Copy client secret to test namespace for Issuer reference
			secretName := "azure-client-secret"
			secretKey := "client-secret"
			copyAzureSecretToNamespace(ctx, ns.Name, secretName, secretKey, clientSecret)

			By("creating ACME Issuer with AzureDNS DNS-01 solver using CCO-provisioned credentials")
			issuerName := "letsencrypt-dns01-cco"
			solver := acmev1.ACMEChallengeSolver{
				DNS01: &acmev1.ACMEChallengeSolverDNS01{
					AzureDNS: &acmev1.ACMEIssuerDNS01ProviderAzureDNS{
						SubscriptionID:    subscriptionID,
						ResourceGroupName: resourceGroupName,
						HostedZoneName:    hostedZoneName,
						TenantID:          string(tenantID),
						ClientID:          string(clientID),
						ClientSecret: &certmanagermetav1.SecretKeySelector{
							LocalObjectReference: certmanagermetav1.LocalObjectReference{
								Name: secretName,
							},
							Key: secretKey,
						},
					},
				},
			}
			issuer := createACMEIssuer(issuerName, solver)
			_, err := certmanagerClient.CertmanagerV1().Issuers(ns.Name).Create(ctx, issuer, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create Issuer")

			By("waiting for Issuer to become ready")
			err = waitForIssuerReadiness(ctx, issuerName, ns.Name)
			Expect(err).NotTo(HaveOccurred(), "timeout waiting for Issuer to become Ready")

			// Create and verify certificate
			certName := "letsencrypt-cert"
			dnsName := fmt.Sprintf("adazc-%s.%s", randomStr(3), appsDomain) // acronym for "ACME DNS01 AzureDNS CCO"
			createAndVerifyACMECertificate(ctx, certName, ns.Name, dnsName, issuerName, "Issuer")
		})
	})

	Context("with Azure DNS in Workload Identity environment", Label("Platform:Azure", "CredentialsMode:Manual"), func() {
		var identityClientID string
		var subscriptionID, dnsResourceGroupName, hostedZoneName string

		BeforeAll(func() {
			By("verifying cluster is STS-enabled")
			isSTS, err := isSTSCluster(ctx, oseOperatorClient, configClient)
			Expect(err).NotTo(HaveOccurred())
			if !isSTS {
				Skip("Test requires Azure Workload Identity enabled")
			}

			By("setting up Azure authentication environment variable from credentials file")
			if os.Getenv("OPENSHIFT_CI") == "true" {
				clusterProfileDir := os.Getenv("CLUSTER_PROFILE_DIR")
				Expect(clusterProfileDir).NotTo(BeEmpty(), "CLUSTER_PROFILE_DIR should exist when running in OpenShift CI")
				os.Setenv("AZURE_AUTH_LOCATION", filepath.Join(clusterProfileDir, "osServicePrincipal.json"))
			} else {
				Expect(os.Getenv("AZURE_AUTH_LOCATION")).NotTo(BeEmpty(), "AZURE_AUTH_LOCATION must be set when running locally")
			}
			azureAuthLocation := os.Getenv("AZURE_AUTH_LOCATION")
			data, err := os.ReadFile(azureAuthLocation)
			Expect(err).NotTo(HaveOccurred(), "failed to read Azure credentials file")
			var sp struct {
				ClientID       string `json:"clientId"`
				ClientSecret   string `json:"clientSecret"`
				TenantID       string `json:"tenantId"`
				SubscriptionID string `json:"subscriptionId"`
			}
			Expect(json.Unmarshal(data, &sp)).To(Succeed(), "failed to parse Azure credentials file")

			By("creating Azure SDK credential")
			cred, err := azidentity.NewClientSecretCredential(sp.TenantID, sp.ClientID, sp.ClientSecret, nil)
			Expect(err).NotTo(HaveOccurred(), "failed to create Azure SDK credential")

			// The DNS zone may live in a different subscription than the cluster.
			// Use the DNS zone subscription for DNS-related operations (issuer, role assignment scope),
			// and the cluster subscription for infrastructure operations (RG lookup, MSI creation).
			subscriptionID, dnsResourceGroupName, hostedZoneName = getAzureDNSZoneInfo(ctx)
			clusterSubscriptionID := sp.SubscriptionID

			By("getting cluster resource group from Infrastructure object")
			infra, err := configClient.Infrastructures().Get(ctx, "cluster", metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to get Infrastructure object")
			clusterResourceGroup := infra.Status.PlatformStatus.Azure.ResourceGroupName
			Expect(clusterResourceGroup).NotTo(BeEmpty(), "Azure resource group should not be empty")

			By("getting cluster resource group location")
			rgClient, err := armresources.NewResourceGroupsClient(clusterSubscriptionID, cred, nil)
			Expect(err).NotTo(HaveOccurred(), "failed to create resource groups client")
			rg, err := rgClient.Get(ctx, clusterResourceGroup, nil)
			Expect(err).NotTo(HaveOccurred(), "failed to get resource group")
			Expect(rg.Location).NotTo(BeNil(), "resource group location should be populated")
			location := *rg.Location

			By("getting OIDC issuer from Authentication object")
			authConfig, err := configClient.Authentications().Get(ctx, "cluster", metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to get Authentication object")
			oidcIssuer := authConfig.Spec.ServiceAccountIssuer
			Expect(oidcIssuer).NotTo(BeEmpty(), "OIDC issuer not found in Authentication object")

			By("creating Azure Managed Identity")
			randomSuffix := randomStr(4)
			identityName := "e2e-cert-manager-dns01-" + randomSuffix
			msiClient, err := armmsi.NewUserAssignedIdentitiesClient(clusterSubscriptionID, cred, nil)
			Expect(err).NotTo(HaveOccurred(), "failed to create MSI client")

			identity, err := msiClient.CreateOrUpdate(ctx, clusterResourceGroup, identityName, armmsi.Identity{
				Location: &location,
			}, nil)
			Expect(err).NotTo(HaveOccurred(), "failed to create Managed Identity")
			Expect(identity.Properties).NotTo(BeNil(), "managed identity properties should be populated")
			Expect(identity.Properties.ClientID).NotTo(BeNil(), "managed identity client ID should be populated")
			Expect(identity.Properties.PrincipalID).NotTo(BeNil(), "managed identity principal ID should be populated")
			identityClientID = *identity.Properties.ClientID
			identityPrincipalID := *identity.Properties.PrincipalID

			DeferCleanup(func(ctx context.Context) {
				By("Cleaning up Azure Managed Identity")
				_, err := msiClient.Delete(ctx, clusterResourceGroup, identityName, nil)
				if err != nil {
					fmt.Fprintf(GinkgoWriter, "failed to delete Managed Identity during cleanup: %v\n", err)
				}
			})

			By("granting DNS Zone Contributor role to Managed Identity on the DNS zone")
			dnsZoneScope := fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Network/dnszones/%s",
				subscriptionID, dnsResourceGroupName, hostedZoneName)

			roleDefClient, err := armauthorization.NewRoleDefinitionsClient(cred, nil)
			Expect(err).NotTo(HaveOccurred(), "failed to create role definitions client")
			roleName := "DNS Zone Contributor"
			filter := fmt.Sprintf("roleName eq '%s'", roleName)
			pager := roleDefClient.NewListPager(dnsZoneScope, &armauthorization.RoleDefinitionsClientListOptions{Filter: &filter})
			Expect(pager.More()).To(BeTrue(), "no role definitions found for %q", roleName)
			page, err := pager.NextPage(ctx)
			Expect(err).NotTo(HaveOccurred(), "failed to list role definitions")
			Expect(page.Value).NotTo(BeEmpty(), "role definition %q not found", roleName)
			dnsZoneContributorRoleID := *page.Value[0].ID

			authClient, err := armauthorization.NewRoleAssignmentsClient(subscriptionID, cred, nil)
			Expect(err).NotTo(HaveOccurred(), "failed to create authorization client")
			roleAssignmentName := uuid.New().String()

			err = wait.PollUntilContextTimeout(ctx, fastPollInterval, lowTimeout, true, func(context.Context) (bool, error) {
				_, assignErr := authClient.Create(ctx, dnsZoneScope, roleAssignmentName, armauthorization.RoleAssignmentCreateParameters{
					Properties: &armauthorization.RoleAssignmentProperties{
						RoleDefinitionID: &dnsZoneContributorRoleID,
						PrincipalID:      &identityPrincipalID,
						PrincipalType:    to.Ptr(armauthorization.PrincipalTypeServicePrincipal),
					},
				}, nil)
				if assignErr != nil {
					var respErr *azcore.ResponseError
					if errors.As(assignErr, &respErr) &&
						respErr.StatusCode == http.StatusConflict && respErr.ErrorCode == "RoleAssignmentExists" {
						return true, nil
					}
					fmt.Fprintf(GinkgoWriter, "role assignment attempt failed (retrying): %v\n", assignErr)
					return false, nil
				}
				return true, nil
			})
			Expect(err).NotTo(HaveOccurred(), "failed to create role assignment")

			DeferCleanup(func(ctx context.Context) {
				By("Cleaning up role assignment")
				_, err := authClient.Delete(ctx, dnsZoneScope, roleAssignmentName, nil)
				if err != nil {
					fmt.Fprintf(GinkgoWriter, "failed to delete role assignment during cleanup: %v\n", err)
				}
			})

			By("creating Federated Identity Credential for cert-manager ServiceAccount")
			ficClient, err := armmsi.NewFederatedIdentityCredentialsClient(clusterSubscriptionID, cred, nil)
			Expect(err).NotTo(HaveOccurred(), "failed to create Federated Identity Credentials client")

			_, err = ficClient.CreateOrUpdate(ctx, clusterResourceGroup, identityName, "cert-manager", armmsi.FederatedIdentityCredential{
				Properties: &armmsi.FederatedIdentityCredentialProperties{
					Issuer:    &oidcIssuer,
					Subject:   to.Ptr("system:serviceaccount:cert-manager:cert-manager"),
					Audiences: []*string{to.Ptr("api://AzureADTokenExchange")},
				},
			}, nil)
			Expect(err).NotTo(HaveOccurred(), "failed to create Federated Identity Credential")

			DeferCleanup(func(ctx context.Context) {
				By("Cleaning up Federated Identity Credential")
				_, err := ficClient.Delete(ctx, clusterResourceGroup, identityName, "cert-manager", nil)
				if err != nil {
					fmt.Fprintf(GinkgoWriter, "failed to delete Federated Identity Credential during cleanup: %v\n", err)
				}
			})
		})

		It("should obtain a valid certificate using ambient credentials through AAD Workload Identity", func() {

			By("adding 'azure.workload.identity/use' label to cert-manager controller pods")
			err := addOverrideLabels(certmanageroperatorclient, certmanagerControllerDeployment, map[string]string{
				"azure.workload.identity/use": "true",
			})
			Expect(err).NotTo(HaveOccurred(), "failed to add workload identity label to cert-manager controller")

			DeferCleanup(func(ctx context.Context) {
				By("Removing workload identity label from cert-manager controller pods")
				if err := addOverrideLabels(certmanageroperatorclient, certmanagerControllerDeployment, nil); err != nil {
					fmt.Fprintf(GinkgoWriter, "failed to remove workload identity label during cleanup: %v\n", err)
				}
			})

			By("waiting for cert-manager deployment to rollout with workload identity label")
			err = waitForDeploymentPodLabelAndRollout(ctx, "cert-manager", "cert-manager", "azure.workload.identity/use", "true", 2*time.Minute)
			Expect(err).NotTo(HaveOccurred(), "timeout waiting for cert-manager deployment rollout with workload identity label")

			By("creating ACME ClusterIssuer with AzureDNS DNS-01 solver using managed identity")
			clusterIssuerName := "letsencrypt-dns01-azuredns-wi"
			solver := acmev1.ACMEChallengeSolver{
				DNS01: &acmev1.ACMEChallengeSolverDNS01{
					AzureDNS: &acmev1.ACMEIssuerDNS01ProviderAzureDNS{
						SubscriptionID:    subscriptionID,
						ResourceGroupName: dnsResourceGroupName,
						HostedZoneName:    hostedZoneName,
						ManagedIdentity: &acmev1.AzureManagedIdentity{
							ClientID: identityClientID,
						},
					},
				},
			}
			clusterIssuer := createACMEClusterIssuer(clusterIssuerName, solver)
			_, err = certmanagerClient.CertmanagerV1().ClusterIssuers().Create(ctx, clusterIssuer, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create ClusterIssuer")
			DeferCleanup(func(ctx context.Context) {
				certmanagerClient.CertmanagerV1().ClusterIssuers().Delete(ctx, clusterIssuerName, metav1.DeleteOptions{})
			})

			By("waiting for ClusterIssuer to become ready")
			err = waitForClusterIssuerReadiness(ctx, clusterIssuerName)
			Expect(err).NotTo(HaveOccurred(), "timeout waiting for ClusterIssuer to become Ready")

			// Create and verify certificate
			certName := "letsencrypt-cert-azuredns-wi"
			dnsName := fmt.Sprintf("adazwi-%s.%s", randomStr(3), appsDomain) // acronym for "ACME DNS01 AzureDNS Workload Identity"
			createAndVerifyACMECertificate(ctx, certName, ns.Name, dnsName, clusterIssuerName, "ClusterIssuer")
		})
	})

	Context("with IBM Cloud Internet Service Webhook", Label("Platform:IBM"), func() {

		// This test uses IBM Cloud Internet Services (CIS) for the DNS-01 challenge.
		// It works with both UPI / IPI installations by passing in the CRN of your CIS instance on IBM Cloud.
		It("should obtain a valid certificate using explicit credentials", func() {

			By("checking for IBM Cloud CIS CRN environment variable")
			cisCRN, isCisCRN := os.LookupEnv(cisCRNEnvironmentVar)
			if targetPlatform, ok := os.LookupEnv(targetPlatformEnvironmentVar); ok && targetPlatform == "ibmcloud-upi" {
				if !isCisCRN || cisCRN == "" {
					Fail("cisCRN is required for IBM Cloud platform")
				}
			} else {
				Skip("Test requires IBM Cloud CIS enabled")
			}

			By("creating ClusterIssuer with IBM Cloud CIS webhook solver")
			clusterIssuerName := "letsencrypt-dns01-explicit-ic"
			solver := acmev1.ACMEChallengeSolver{
				DNS01: &acmev1.ACMEChallengeSolverDNS01{
					Webhook: &acmev1.ACMEIssuerDNS01ProviderWebhook{
						GroupName:  "acme.borup.work",
						SolverName: "ibmcis",
						Config: &apiextensionsv1.JSON{
							Raw: []byte(fmt.Sprintf(`{
								"apiKeySecretRef": {
									"name": "ibmcis-credentials",
									"key": "api-token"
								},
								"cisCRN": ["%s"]
							}`, cisCRN)),
						},
					},
				},
			}
			clusterIssuer := createACMEClusterIssuer(clusterIssuerName, solver)
			_, err := certmanagerClient.CertmanagerV1().ClusterIssuers().Create(ctx, clusterIssuer, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred(), "failed to create ClusterIssuer")

			DeferCleanup(func(cleanupCtx context.Context) {
				certmanagerClient.CertmanagerV1().ClusterIssuers().Delete(cleanupCtx, clusterIssuerName, metav1.DeleteOptions{})
			})

			By("waiting for ClusterIssuer to become ready")
			err = waitForClusterIssuerReadiness(ctx, clusterIssuerName)
			Expect(err).NotTo(HaveOccurred(), "timeout waiting for ClusterIssuer to become Ready")

			// Create and verify certificate
			certName := "letsencrypt-cert-ic"
			dnsName := fmt.Sprintf("adwicis-%s.%s", randomStr(3), appsDomain) // acronym for "ACME DNS01 Webhook IBM Cloud Internet Service"
			createAndVerifyACMECertificate(ctx, certName, ns.Name, dnsName, clusterIssuerName, "ClusterIssuer")
		})
	})
})
