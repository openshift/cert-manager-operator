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
// TODO: Add more asset names as resources are implemented
const (
	serviceAccountAssetName = "trust-manager/resources/serviceaccount_trust-manager.yml"
)
