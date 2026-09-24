package trustmanager

import (
	"os"
	"time"
)

const (
	// trustManagerCommonName is the name commonly used for naming resources.
	trustManagerCommonName = "cert-manager-trust-manager"

	// ControllerName is the name of the controller used in logs and events.
	ControllerName = trustManagerCommonName + "-controller"

	// controllerProcessedAnnotation is the annotation added to trustmanager resource once after
	// successful reconciliation by the controller.
	controllerProcessedAnnotation = "operator.openshift.io/trust-manager-processed"

	// controllerProcessingRejectedAnnotation is the annotation added to trustmanager resource when multiple
	// instances of trustmanager resource is created.
	controllerProcessingRejectedAnnotation = "operator.openshift.io/trust-manager-reject-multiple-instance"

	// finalizer name for trustmanagers.operator.openshift.io resource.
	finalizer = "trustmanagers.operator.openshift.io/" + ControllerName

	// defaultRequeueTime is the default reconcile requeue time.
	defaultRequeueTime = time.Second * 30

	// trustManagerObjectName is the name of the trust-manager resource created by user.
	// TrustManager CRD enforces name to be `cluster`.
	trustManagerObjectName = "cluster"

	// trustManagerContainerName is the name of the container created for trust-manager.
	trustManagerContainerName = "trust-manager"

	// trustManagerImageNameEnvVarName is the environment variable key name
	// containing the image name of the trust-manager as value.
	trustManagerImageNameEnvVarName = "RELATED_IMAGE_TRUST_MANAGER"

	// trustManagerImageVersionEnvVarName is the environment variable key name
	// containing the image version of the trust-manager as value.
	trustManagerImageVersionEnvVarName = "TRUST_MANAGER_OPERAND_IMAGE_VERSION"

	// operandNamespace is the namespace where trust-manager is deployed.
	operandNamespace = "cert-manager"
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
	serviceAccountAssetName     = "trust-manager/trust-manager-serviceaccount.yaml"
	clusterRoleAssetName        = "trust-manager/trust-manager-clusterrole.yaml"
	clusterRoleBindingAssetName = "trust-manager/trust-manager-clusterrolebinding.yaml"
	roleAssetName               = "trust-manager/trust-manager-role.yaml"
	roleBindingAssetName        = "trust-manager/trust-manager-rolebinding.yaml"
	roleLeasesAssetName         = "trust-manager/trust-manager-leases-role.yaml"
	roleBindingLeasesAssetName  = "trust-manager/trust-manager-leases-rolebinding.yaml"
	deploymentAssetName         = "trust-manager/trust-manager-deployment.yaml"
	serviceAssetName            = "trust-manager/trust-manager-service.yaml"
	metricsServiceAssetName     = "trust-manager/trust-manager-metrics-service.yaml"
	certificateAssetName        = "trust-manager/trust-manager-certificate.yaml"
	issuerAssetName             = "trust-manager/trust-manager-issuer.yaml"
	webhookAssetName            = "trust-manager/trust-manager-webhook.yaml"
)
