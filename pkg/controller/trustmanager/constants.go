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

	// finalizer name for trustmanagers.operator.openshift.io resource.
	finalizer = "trustmanagers.operator.openshift.io/" + ControllerName

	// defaultRequeueTime is the default reconcile requeue time.
	defaultRequeueTime = time.Second * 30

	// trustManagerObjectName is the name of the trust-manager resource created by user.
	// trust-manager CRD enforces name to be `cluster`.
	trustManagerObjectName = "cluster"

	// trustManagerContainerName is the name of the container created for trust-manager.
	trustManagerContainerName = "trust-manager"

	// trustManagerImageNameEnvVarName is the environment variable key name
	// containing the image name of the trust-manager as value.
	trustManagerImageNameEnvVarName = "RELATED_IMAGE_CERT_MANAGER_TRUST_MANAGER"

	// trustManagerImageVersionEnvVarName is the environment variable key name
	// containing the image version of the trust-manager as value.
	trustManagerImageVersionEnvVarName = "TRUST_MANAGER_OPERAND_IMAGE_VERSION"

	// operandNamespace is the namespace where trust-manager is deployed.
	operandNamespace = "cert-manager"

	// operatorNamespace is the namespace where the cert-manager operator is deployed.
	operatorNamespace = "cert-manager-operator"

	// defaultCAPackageConfigMapName is the name of the ConfigMap created by the controller
	// containing the formatted CA package for trust-manager.
	defaultCAPackageConfigMapName = "trust-manager-default-ca-package"

	// trustedCABundleConfigMapName is the name of the ConfigMap in the operator namespace
	// that receives the injected CA bundle from CNO.
	trustedCABundleConfigMapName = "cert-manager-operator-trusted-ca-bundle"

	// caPackageJSONName is the name of the JSON package file within the ConfigMap.
	caPackageJSONName = "cert-manager-package-openshift.json"

	// caPackageMountPath is the path where the default CA package is mounted in the trust-manager container.
	caPackageMountPath = "/packages"

	// defaultCAPackageHashAnnotation is the annotation on the Deployment storing the hash
	// of the default CA package content, used to trigger rolling restarts when the package changes.
	defaultCAPackageHashAnnotation = "operator.openshift.io/default-ca-package-hash"

	// webhookPort is the port used by the trust-manager webhook.
	webhookPort int32 = 6443

	// metricsPort is the port used by the trust-manager metrics endpoint.
	metricsPort int32 = 9402

	// readinessProbePort is the port used for the trust-manager readiness probe.
	readinessProbePort int32 = 6060
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
	deploymentAssetName         = "trust-manager/trust-manager-deployment.yaml"
	clusterRoleAssetName        = "trust-manager/trust-manager-clusterrole.yaml"
	clusterRoleBindingAssetName = "trust-manager/trust-manager-clusterrolebinding.yaml"
	roleAssetName               = "trust-manager/trust-manager-role.yaml"
	roleLeasesAssetName         = "trust-manager/trust-manager-leaderelection-role.yaml"
	roleBindingAssetName        = "trust-manager/trust-manager-rolebinding.yaml"
	roleBindingLeasesAssetName  = "trust-manager/trust-manager-leaderelection-rolebinding.yaml"
	serviceAssetName            = "trust-manager/trust-manager-service.yaml"
	metricsServiceAssetName     = "trust-manager/trust-manager-metrics-service.yaml"
	serviceAccountAssetName     = "trust-manager/trust-manager-serviceaccount.yaml"
	certificateAssetName        = "trust-manager/trust-manager-certificate.yaml"
	issuerAssetName             = "trust-manager/trust-manager-issuer.yaml"
	webhookAssetName            = "trust-manager/trust-manager-validatingwebhookconfiguration.yaml"
)
