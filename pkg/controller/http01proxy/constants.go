package http01proxy

import (
	"time"

	configv1 "github.com/openshift/api/config/v1"
)

var (
	infrastructureGVK = configv1.SchemeGroupVersion.WithKind("Infrastructure")
)

const (
	http01proxyCommonName = "cert-manager-http01-proxy"
	ControllerName        = http01proxyCommonName + "-controller"

	controllerProcessedAnnotation = "operator.openshift.io/http01-proxy-processed"
	finalizer                     = "http01proxy.openshift.operator.io/" + ControllerName
	defaultRequeueTime            = time.Second * 30

	http01proxyObjectName = "cluster"

	machineConfigName = "98-nftables-crtmgr-http01-dnat"

	nftRulesAssetName      = "http01proxy/nftables-rules.tmpl"
	machineConfigAssetName = "http01proxy/machineconfig.yaml.tmpl"
)
