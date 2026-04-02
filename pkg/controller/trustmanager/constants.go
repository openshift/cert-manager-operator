package trustmanager

import (
	"os"
	"time"

	"k8s.io/apimachinery/pkg/util/validation/field"
)

const (
	// trustManagerCommonName is the name commonly used for naming resources.
	trustManagerCommonName = "cert-manager-trust-manager"

	// ControllerName is the name of the controller used in logs and events.
	ControllerName = trustManagerCommonName + "-controller"

	// finalizer name for trustmanager.openshift.operator.io resource.
	finalizer = "trustmanager.openshift.operator.io/" + ControllerName

	// defaultRequeueTime is the default reconcile requeue time.
	defaultRequeueTime = time.Second * 30

	// trustManagerObjectName is the name of the trust-manager resource created by user.
	// trust-manager CRD enforces name to be `cluster`.
	trustManagerObjectName = "cluster"

	// trustManagerImageNameEnvVarName is the environment variable key name
	// containing the image name of the trust-manager as value.
	trustManagerImageNameEnvVarName = "RELATED_IMAGE_CERT_MANAGER_TRUST_MANAGER"

	// trustManagerImageVersionEnvVarName is the environment variable key name
	// containing the image version of the trust-manager as value.
	trustManagerImageVersionEnvVarName = "TRUSTMANAGER_OPERAND_IMAGE_VERSION"

	// defaultTrustNamespace is the default namespace where trust-manager looks for trust sources.
	defaultTrustNamespace = "cert-manager"

	// operandNamespace is the namespace where trust-manager operand is deployed.
	operandNamespace = "cert-manager"

	// fieldOwner is the field manager name used for Server-Side Apply operations.
	// All resource reconcilers should use this to identify ownership of fields.
	fieldOwner = "trust-manager-controller"

	// trustManagerContainerName is the name of the trust-manager container in the deployment.
	trustManagerContainerName = "trust-manager"

	// roleBindingSubjectKind is the kind used in RBAC binding subjects.
	roleBindingSubjectKind = "ServiceAccount"
)

// DefaultCAPackage constants.
const (
	// defaultCAPackageConfigMapName is the ConfigMap in the operand namespace that
	// contains the formatted JSON CA package for trust-manager.
	defaultCAPackageConfigMapName = "trust-manager-default-ca-package"

	// defaultCAPackageName is the package name used in the JSON CA package.
	defaultCAPackageName = "cert-manager-package-openshift"

	// defaultCAPackageFilename is the filename used for the JSON package inside the ConfigMap.
	defaultCAPackageFilename = defaultCAPackageName + ".json"

	// defaultCAPackageVolumeName is the volume name used in the deployment.
	defaultCAPackageVolumeName = "packages"

	// defaultCAPackageMountPath is where the package volume is mounted in the container.
	defaultCAPackageMountPath = "/packages"

	// defaultCAPackageLocation is the full path to the JSON package file inside the container.
	defaultCAPackageLocation = defaultCAPackageMountPath + "/" + defaultCAPackageFilename

	// defaultCAPackageHashAnnotation is the pod template annotation that tracks the CA bundle hash.
	defaultCAPackageHashAnnotation = "operator.openshift.io/default-ca-package-hash"
)

// Resource names used for creating resources and cross-referencing between them.
// These must be set explicitly on each resource's .metadata.name and on every
// field in other resources that references them.
const (
	trustManagerCommonResourceName = "trust-manager"

	trustManagerServiceAccountName = trustManagerCommonResourceName
	trustManagerDeploymentName     = trustManagerCommonResourceName

	trustManagerServiceName        = trustManagerCommonResourceName
	trustManagerMetricsServiceName = trustManagerCommonResourceName + "-metrics"

	trustManagerClusterRoleName        = trustManagerCommonResourceName
	trustManagerClusterRoleBindingName = trustManagerCommonResourceName

	trustManagerRoleName        = trustManagerCommonResourceName
	trustManagerRoleBindingName = trustManagerCommonResourceName

	trustManagerLeaderElectionRoleName        = trustManagerCommonResourceName + ":leaderelection"
	trustManagerLeaderElectionRoleBindingName = trustManagerCommonResourceName + ":leaderelection"

	trustManagerIssuerName      = trustManagerCommonResourceName
	trustManagerCertificateName = trustManagerCommonResourceName
	trustManagerTLSSecretName   = trustManagerCommonResourceName + "-tls"

	trustManagerWebhookConfigName = trustManagerCommonResourceName
)

var (
	trustManagerConfigFieldPath     = field.NewPath("spec", "trustManagerConfig")
	controllerConfigFieldPath       = field.NewPath("spec", "controllerConfig")
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
	serviceAccountAssetName = "trust-manager/resources/serviceaccount_trust-manager.yml"

	deploymentAssetName = "trust-manager/resources/deployment_trust-manager.yml"

	serviceAssetName        = "trust-manager/resources/service_trust-manager.yml"
	metricsServiceAssetName = "trust-manager/resources/service_trust-manager-metrics.yml"

	clusterRoleAssetName        = "trust-manager/resources/clusterrole_trust-manager.yml"
	clusterRoleBindingAssetName = "trust-manager/resources/clusterrolebinding_trust-manager.yml"

	roleAssetName        = "trust-manager/resources/role_trust-manager.yml"
	roleBindingAssetName = "trust-manager/resources/rolebinding_trust-manager.yml"

	roleLeaderElectionAssetName        = "trust-manager/resources/role_trust-manager:leaderelection.yml"
	roleBindingLeaderElectionAssetName = "trust-manager/resources/rolebinding_trust-manager:leaderelection.yml"

	issuerAssetName      = "trust-manager/resources/issuer_trust-manager.yml"
	certificateAssetName = "trust-manager/resources/certificate_trust-manager.yml"

	validatingWebhookConfigAssetName = "trust-manager/resources/validatingwebhookconfiguration_trust-manager.yml"
)
