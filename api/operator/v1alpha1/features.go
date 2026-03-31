package v1alpha1

import (
	"k8s.io/component-base/featuregate"
)

var (
	// FeatureIstioCSR enables the controller for istiocsr.operator.openshift.io resource,
	// which extends cert-manager-operator to deploy and manage the istio-csr agent.
	// OpenShift Service Mesh facilitates the integration and istio-csr is an agent that
	// allows Istio workload and control plane components to be secured using cert-manager.
	//
	// For more details,
	// https://github.com/openshift/enhancements/blob/master/enhancements/cert-manager/istio-csr-controller.md
	FeatureIstioCSR featuregate.Feature = "IstioCSR"

	// FeatureTrustManager enables the controller for trustmanagers.operator.openshift.io resource,
	// which extends cert-manager-operator to deploy and manage the trust-manager operand.
	// trust-manager provides a way to manage trust bundles in OpenShift clusters.
	//
	// For more details,
	// https://github.com/openshift/enhancements/blob/master/enhancements/cert-manager/trust-manager-controller.md
	FeatureTrustManager featuregate.Feature = "TrustManager"
)

var OperatorFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{
	FeatureIstioCSR:     {Default: true, PreRelease: featuregate.GA},
	FeatureTrustManager: {Default: false, PreRelease: "TechPreview"},
}
