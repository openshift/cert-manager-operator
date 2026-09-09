package http01proxy

import (
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	infrastructureGVK = schema.GroupVersionKind{
		Group:   "config.openshift.io",
		Version: "v1",
		Kind:    "Infrastructure",
	}

	machineConfigGVK = schema.GroupVersionKind{
		Group:   "machineconfiguration.openshift.io",
		Version: "v1",
		Kind:    "MachineConfig",
	}
)

const (
	http01proxyCommonName = "cert-manager-http01-proxy"
	ControllerName        = http01proxyCommonName + "-controller"

	controllerProcessedAnnotation = "operator.openshift.io/http01-proxy-processed"
	finalizer                     = "http01proxy.openshift.operator.io/" + ControllerName
	defaultRequeueTime            = time.Second * 30

	http01proxyObjectName = "default"

	machineConfigName = "98-nftables-crtmgr-http01-dnat"
)
