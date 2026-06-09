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
	trustManagerContainerName = trustManagerCommonName

	// trustManagerImageNameEnvVarName is the environment variable key name
	// containing the image name of the trust-manager as value.
	trustManagerImageNameEnvVarName = "RELATED_IMAGE_TRUST_MANAGER"

	// trustManagerImageVersionEnvVarName is the environment variable key name
	// containing the image version of the trust-manager as value.
	trustManagerImageVersionEnvVarName = "TRUST_MANAGER_OPERAND_IMAGE_VERSION"

	// trustManagerDefaultNamespace is the default namespace where trust-manager
	// looks for trust sources and where it is deployed.
	trustManagerDefaultNamespace = "cert-manager"

	// roleBindingSubjectKind is the kind of subject in a RoleBinding or ClusterRoleBinding.
	roleBindingSubjectKind = "ServiceAccount"
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
	clusterRoleAssetName            = "trust-manager/trust-manager-clusterrole.yaml"
	clusterRoleBindingAssetName     = "trust-manager/trust-manager-clusterrolebinding.yaml"
	deploymentAssetName             = "trust-manager/trust-manager-deployment.yaml"
	roleLeasesAssetName             = "trust-manager/trust-manager-leases-role.yaml"
	roleBindingLeasesAssetName      = "trust-manager/trust-manager-leases-rolebinding.yaml"
	metricsServiceAssetName         = "trust-manager/trust-manager-metrics-service.yaml"
	serviceAccountAssetName         = "trust-manager/trust-manager-serviceaccount.yaml"
	secretTargetsRoleAssetName      = "trust-manager/trust-manager-secret-targets-role.yaml"
	secretTargetsRoleBindingName    = "trust-manager/trust-manager-secret-targets-rolebinding.yaml"
)
