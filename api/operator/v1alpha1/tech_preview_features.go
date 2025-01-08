package v1alpha1

// FeatureName represent name of a feature
// +kubebuilder:validation:Enum=IstioCSR
type FeatureName string

var (
	// IstioCSR enables the controller for istiocsr.operator.openshift.io resource,
	// which extends cert-manager-operator to deploy and manage the istio-csr agent.
	// OpenShift Service Mesh facilitates the integration and istio-csr is an agent that
	// allows Istio workload and control plane components to be secured using cert-manager.
	//
	// For more details,
	// https://github.com/openshift/enhancements/blob/master/enhancements/cert-manager/istio-csr-controller.md
	FeatureIstioCSR FeatureName = "IstioCSR"
)
