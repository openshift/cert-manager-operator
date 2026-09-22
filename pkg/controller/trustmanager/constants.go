package trustmanager

import (
	"os"
	"time"
)

const (
	// trustManagerCommonName is the name commonly used for naming resources.
	trustManagerCommonName = "trust-manager"

	// ControllerName is the name of the controller used in logs and events.
	ControllerName = trustManagerCommonName + "-controller"

	// controllerProcessedAnnotation is the annotation added to trustmanager resource once after
	// successful reconciliation by the controller.
	controllerProcessedAnnotation = "operator.openshift.io/trust-manager-processed"

	// finalizer name for trustmanagers.operator.openshift.io resource.
	finalizer = "trustmanagers.operator.openshift.io/" + ControllerName

	// defaultRequeueTime is the default reconcile requeue time.
	defaultRequeueTime = time.Second * 30

	// trustManagerObjectName is the name of the trust-manager resource created by user.
	// TrustManager CRD enforces name to be `cluster`.
	trustManagerObjectName = "cluster"

	// trustManagerContainerName is the name of the container created for trust-manager.
	trustManagerContainerName = trustManagerCommonName

	// trustManagerImageNameEnvVarName is the environment variable key name
	// containing the image name of the trust-manager as value.
	trustManagerImageNameEnvVarName = "RELATED_IMAGE_TRUST_MANAGER"

	// trustManagerImageVersionEnvVarName is the environment variable key name
	// containing the image version of the trust-manager as value.
	trustManagerImageVersionEnvVarName = "TRUST_MANAGER_OPERAND_IMAGE_VERSION"

	// defaultTrustNamespace is the default namespace where trust-manager looks for trust sources.
	defaultTrustNamespace = "cert-manager"

	// defaultCAPackageConfigMapName is the name of the ConfigMap created for the default CA package.
	defaultCAPackageConfigMapName = "trust-manager-default-ca-package"

	// defaultCAPackageKey is the key used in the default CA package ConfigMap.
	defaultCAPackageKey = "ca-certificates.json"

	// trustedCAAnnotation is the annotation used for OpenShift trusted CA bundle injection.
	trustedCAAnnotation = "config.openshift.io/inject-trusted-cabundle"

	// trustedCAConfigMapName is the name of the ConfigMap that receives injected trusted CA bundle.
	trustedCAConfigMapName = "trust-manager-openshift-ca-bundle"

	// trustedCABundleKey is the key in the injected CA bundle ConfigMap.
	trustedCABundleKey = "ca-bundle.crt"
)

var (
	controllerDefaultResourceLabels = map[string]string{
		"app":                          trustManagerCommonName,
		"app.kubernetes.io/name":       trustManagerCommonName,
		"app.kubernetes.io/instance":   trustManagerCommonName,
		"app.kubernetes.io/version":    os.Getenv(trustManagerImageVersionEnvVarName),
		"app.kubernetes.io/managed-by": "cert-manager-operator",
		"app.kubernetes.io/part-of":    "cert-manager-operator",
	}
)

// asset names are the files present in the root bindata/ dir. Which are then loaded
// and made available by the pkg/operator/assets package.
const (
	deploymentAssetName                      = "trust-manager/trust-manager-deployment.yaml"
	clusterRoleAssetName                     = "trust-manager/trust-manager-clusterrole.yaml"
	clusterRoleBindingAssetName              = "trust-manager/trust-manager-clusterrolebinding.yaml"
	roleAssetName                            = "trust-manager/trust-manager-role.yaml"
	roleLeasesAssetName                      = "trust-manager/trust-manager-leases-role.yaml"
	roleBindingAssetName                     = "trust-manager/trust-manager-rolebinding.yaml"
	roleBindingLeasesAssetName               = "trust-manager/trust-manager-leases-rolebinding.yaml"
	webhookServiceAssetName                  = "trust-manager/trust-manager-webhook-service.yaml"
	metricsServiceAssetName                  = "trust-manager/trust-manager-metrics-service.yaml"
	serviceAccountAssetName                  = "trust-manager/trust-manager-serviceaccount.yaml"
	webhookCertificateAssetName              = "trust-manager/trust-manager-webhook-certificate.yaml"
	webhookIssuerAssetName                   = "trust-manager/trust-manager-webhook-issuer.yaml"
	validatingWebhookConfigAssetName         = "trust-manager/trust-manager-validatingwebhookconfiguration.yaml"
	secretTargetsClusterRoleAssetName        = "trust-manager/trust-manager-secret-targets-clusterrole.yaml"
	secretTargetsClusterRoleBindingAssetName = "trust-manager/trust-manager-secret-targets-clusterrolebinding.yaml"
)

var trustManagerNetworkPolicyAssets = []string{
	"networkpolicies/trust-manager-deny-all-networkpolicy.yaml",
	"networkpolicies/trust-manager-allow-egress-to-api-server-networkpolicy.yaml",
	"networkpolicies/trust-manager-allow-ingress-to-metrics-networkpolicy.yaml",
	"networkpolicies/trust-manager-allow-ingress-to-webhook-networkpolicy.yaml",
}
