package v1alpha1

import (
	"k8s.io/component-base/featuregate"
)

var (
	// IstioCSR enables the controller for istiocsr.operator.openshift.io resource,
	// which extends cert-manager-operator to deploy and manage the istio-csr agent.
	// OpenShift Service Mesh facilitates the integration and istio-csr is an agent that
	// allows Istio workload and control plane components to be secured using cert-manager.
	//
	// For more details,
	// https://github.com/openshift/enhancements/blob/master/enhancements/cert-manager/istio-csr-controller.md
	FeatureIstioCSR featuregate.Feature = "IstioCSR"

	// FeatureTrustManager enables the controller for trustmanagers.operator.openshift.io resource,
	// which extends cert-manager-operator to deploy and manage the trust-manager operand.
	// trust-manager provides a way to manage trust bundles in Kubernetes and OpenShift
	// clusters by combining trusted certificate sources into bundles that applications
	// can trust directly.
	//
	// For more details,
	// https://github.com/openshift/enhancements/blob/master/enhancements/cert-manager/trust-manager-controller.md
	FeatureTrustManager featuregate.Feature = "TrustManager"
)

var OperatorFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{
	FeatureIstioCSR:     {Default: true, PreRelease: featuregate.GA},
	FeatureTrustManager: {Default: false, PreRelease: featuregate.Alpha},
}
