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

	// finalizer name for trustmanagers.openshift.operator.io resource.
	finalizer = "trustmanagers.operator.openshift.io/" + ControllerName

	// defaultRequeueTime is the default reconcile requeue time.
	defaultRequeueTime = time.Second * 30

	// trustManagerContainerName is the name of the container created for trust-manager.
	trustManagerContainerName = trustManagerCommonName

	// trustManagerImageNameEnvVarName is the environment variable key name
	// containing the image name of the trust-manager as value.
	trustManagerImageNameEnvVarName = "RELATED_IMAGE_TRUST_MANAGER"

	// trustManagerImageVersionEnvVarName is the environment variable key name
	// containing the image version of the trust-manager as value.
	trustManagerImageVersionEnvVarName = "TRUST_MANAGER_OPERAND_IMAGE_VERSION"

	// trustManagerOperandNamespace is the namespace where trust-manager operand is deployed.
	trustManagerOperandNamespace = "cert-manager"

	// defaultCAPackageConfigMapName is the name of the ConfigMap used for the default CA package.
	defaultCAPackageConfigMapName = "trust-manager-default-ca-package"

	// defaultCAPackageKeyName is the key name for the CA bundle data in the default CA package ConfigMap.
	defaultCAPackageKeyName = "ca-certificates.crt"

	// cnoTrustedCAAnnotation is the annotation used to request CNO trusted CA bundle injection.
	cnoTrustedCAAnnotation = "config.openshift.io/inject-trusted-cabundle"
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
	clusterRoleAssetName                    = "trust-manager/trust-manager-clusterrole.yaml"
	clusterRoleBindingAssetName             = "trust-manager/trust-manager-clusterrolebinding.yaml"
	deploymentAssetName                     = "trust-manager/trust-manager-deployment.yaml"
	roleAssetName                           = "trust-manager/trust-manager-role.yaml"
	roleBindingAssetName                    = "trust-manager/trust-manager-rolebinding.yaml"
	metricsServiceAssetName                 = "trust-manager/trust-manager-metrics-service.yaml"
	webhookServiceAssetName                 = "trust-manager/trust-manager-webhook-service.yaml"
	serviceAccountAssetName                 = "trust-manager/trust-manager-serviceaccount.yaml"
	webhookCertificateAssetName             = "trust-manager/trust-manager-webhook-certificate.yaml"
	validatingWebhookConfigurationAssetName = "trust-manager/trust-manager-validatingwebhookconfiguration.yaml"
)
