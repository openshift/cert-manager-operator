package istiocsr

import (
	"os"
	"strings"
	"time"

	certmanagerapi "github.com/cert-manager/cert-manager/pkg/apis/certmanager"
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
)

const (
	// istiocsrCommonName is the name commonly used for naming resources.
	istiocsrCommonName = "cert-manager-istio-csr"

	// ControllerName is the name of the controller used in logs and events.
	ControllerName = istiocsrCommonName + "-controller"

	// controllerProcessedAnnotation is the annotation added to istiocsr resource once after
	// successful reconciliation by the controller.
	controllerProcessedAnnotation = "operator.openshift.io/istio-csr-processed"

	// finalizer name for istiocsr.openshift.operator.io resource.
	finalizer = "istiocsr.openshift.operator.io/finalizer"

	// defaultRequeueTime is the default reconcile requeue time.
	defaultRequeueTime = time.Second * 30

	// istiocsrObjectName is the name of the istio-csr resource created by user.
	// istio-csr CRD enforces name to be `default`.
	istiocsrObjectName = "default"

	// istiocsrContainerName is the name of the container created for istiocsr.
	istiocsrContainerName = istiocsrCommonName

	// istiocsrImageNameEnvVarName is the environment variable key name
	// containing the image name of the istiocsr as value.
	istiocsrImageNameEnvVarName = "RELATED_IMAGE_CERT_MANAGER_ISTIOCSR"

	// istiocsrImageVersionEnvVarName is the environment variable key name
	// containing the image version of the istiocsr as value.
	istiocsrImageVersionEnvVarName = "ISTIOCSR_OPERAND_IMAGE_VERSION"

	// istiocsrGRPCEndpointFmt is the format string for the istiocsr GRPC service endpoint.
	istiocsrGRPCEndpointFmt = "%s.%s.svc:%d"

	// istiodCertificateCommonNameFmt is the format string for deriving the istiod certificate common name.
	istiodCertificateCommonNameFmt = "istiod.%s.svc"

	// istiodCertificateDefaultDNSName is the format string for deriving the istiod certificate default DNS name.
	istiodCertificateDefaultDNSName = "istiod.%s.svc"

	// istiodCertificateRevisionBasedDNSName is the format string for deriving the istiod certificate DNS name
	// for each defined revision.
	istiodCertificateRevisionBasedDNSName = "istiod-%s.%s.svc"

	// istiodCertificateSpiffeURIFmt is the format string for deriving the istiod certificate URI.
	istiodCertificateSpiffeURIFmt = "spiffe://%s/ns/%s/sa/istiod-service-account"

	// istiocsrNamespaceMappingLabelName is the label name for identifying the cluster resources or resources
	// created in other namespaces by the controller.
	istiocsrNamespaceMappingLabelName = "cert-manager-istio-csr-namespace"

	// istiocsrResourceWatchLabelName is the label name for identifying the resources of interest for the
	// controller but does not create or manage the resource.
	istiocsrResourceWatchLabelName = "istiocsr.openshift.operator.io/watched-by"

	// istiocsrResourceWatchLabelName is the value format assigned to istiocsrResourceWatchLabelName label, which
	// will be of the form <istiocsr_namespace>/<istiocsr_instance-Name>
	istiocsrResourceWatchLabelValueFmt = "%s_%s"

	// istiocsrCAConfigMapName is the name o the configmap which is mounted in istiocsr container, containing the
	// CA certificate configured in the secret referenced in the issuer.
	istiocsrCAConfigMapName = istiocsrCommonName + "-issuer-ca-copy"

	// istiocsrCAKeyName is the key name holding the CA certificate in the issuer secret or the controller
	// CA configmap.
	istiocsrCAKeyName = "ca.crt"
)

var (
	controllerDefaultResourceLabels = map[string]string{
		"app":                          istiocsrCommonName,
		"app.kubernetes.io/name":       istiocsrCommonName,
		"app.kubernetes.io/instance":   istiocsrCommonName,
		"app.kubernetes.io/version":    os.Getenv(istiocsrImageVersionEnvVarName),
		"app.kubernetes.io/managed-by": "cert-manager-operator",
		"app.kubernetes.io/part-of":    "cert-manager-operator",
	}
)

var (
	clusterIssuerKind = strings.ToLower(certmanagerv1.ClusterIssuerKind)
	issuerKind        = strings.ToLower(certmanagerv1.IssuerKind)
	issuerGroup       = strings.ToLower(certmanagerapi.GroupName)
)

// asset names are the files present in the root bindata/ dir. Which are then loaded
// and made available by the pkg/operator/assets package.
const (
	certificateAssetName        = "istio-csr/certificate.yaml"
	clusterRoleAssetName        = "istio-csr/clusterrole.yaml"
	clusterRoleBindingAssetName = "istio-csr/clusterrolebinding.yaml"
	deploymentAssetName         = "istio-csr/deployment.yaml"
	roleAssetName               = "istio-csr/role.yaml"
	roleLeasesAssetName         = "istio-csr/role_leases.yaml"
	roleBindingAssetName        = "istio-csr/rolebinding.yaml"
	roleBindingLeasesAssetName  = "istio-csr/rolebinding_leases.yaml"
	serviceAssetName            = "istio-csr/service.yaml"
	serviceAccountAssetName     = "istio-csr/serviceaccount.yaml"
)

const (
	DefaultCertificateDuration            = time.Hour
	DefaultCertificateRenewBeforeDuration = time.Minute * 30
	DefaultSignatureAlgorithm             = certmanagerv1.RSAKeyAlgorithm
	DefaultRSAPrivateKeySize              = 2048
	DefaultECDSA256PrivateKeySize         = 256
	DefaultECDSA384PrivateKeySize         = 384
)
