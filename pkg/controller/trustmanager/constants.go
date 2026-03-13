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

// Resource names used for creating resources and cross-referencing between them.
// These must be set explicitly on each resource's .metadata.name and on every
// field in other resources that references them.
const (
	trustManagerServiceAccountName = "trust-manager"
	trustManagerDeploymentName     = "trust-manager"

	trustManagerServiceName        = "trust-manager"
	trustManagerMetricsServiceName = "trust-manager-metrics"

	trustManagerClusterRoleName        = "trust-manager"
	trustManagerClusterRoleBindingName = "trust-manager"

	trustManagerRoleName        = "trust-manager"
	trustManagerRoleBindingName = "trust-manager"

	trustManagerLeaderElectionRoleName        = "trust-manager:leaderelection"
	trustManagerLeaderElectionRoleBindingName = "trust-manager:leaderelection"

	trustManagerIssuerName      = "trust-manager"
	trustManagerCertificateName = "trust-manager"
	trustManagerTLSSecretName   = "trust-manager-tls"

	trustManagerWebhookConfigName = "trust-manager"
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
