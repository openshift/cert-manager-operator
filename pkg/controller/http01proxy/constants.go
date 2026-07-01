package http01proxy

import (
	"os"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/openshift/cert-manager-operator/pkg/controller/common"
)

var (
	// infrastructureGVK is the GVK for the OpenShift Infrastructure resource.
	infrastructureGVK = schema.GroupVersionKind{
		Group:   "config.openshift.io",
		Version: "v1",
		Kind:    "Infrastructure",
	}
)

const (
	http01proxyCommonName = "cert-manager-http01-proxy"
	ControllerName        = http01proxyCommonName + "-controller"

	controllerProcessedAnnotation = "operator.openshift.io/http01-proxy-processed"
	finalizer                     = "http01proxy.openshift.operator.io/" + ControllerName
	defaultRequeueTime            = time.Second * 30

	// CRD enforces singleton name "default".
	http01proxyObjectName = "default"

	http01proxyImageNameEnvVarName    = "RELATED_IMAGE_CERT_MANAGER_HTTP01PROXY"
	http01proxyImageVersionEnvVarName = "HTTP01PROXY_OPERAND_IMAGE_VERSION"

	defaultInternalPort int32 = 8888
	proxyPortName             = "proxy"
	proxyPortEnvVar           = "PROXY_PORT"
)

var (
	controllerDefaultResourceLabels = map[string]string{
		common.ManagedResourceLabelKey: http01proxyCommonName,
		"app.kubernetes.io/name":       http01proxyCommonName,
		"app.kubernetes.io/instance":   http01proxyCommonName,
		"app.kubernetes.io/version":    os.Getenv(http01proxyImageVersionEnvVarName),
		"app.kubernetes.io/managed-by": "cert-manager-operator",
		"app.kubernetes.io/part-of":    "cert-manager-operator",
	}
)

// asset names are the files present in the root bindata/ dir.
const (
	daemonsetAssetName          = "http01-proxy/cert-manager-http01-proxy-daemonset.yaml"
	serviceAccountAssetName     = "http01-proxy/cert-manager-http01-proxy-serviceaccount.yaml"
	clusterRoleAssetName        = "http01-proxy/cert-manager-http01-proxy-clusterrole.yaml"
	clusterRoleBindingAssetName = "http01-proxy/cert-manager-http01-proxy-clusterrolebinding.yaml"
	sccRoleBindingAssetName     = "http01-proxy/cert-manager-http01-proxy-scc-rolebinding.yaml"
)

var http01ProxyNetworkPolicyAssets = []string{
	"networkpolicies/http01-proxy-deny-all-networkpolicy.yaml",
	"networkpolicies/http01-proxy-allow-egress-networkpolicy.yaml",
}
