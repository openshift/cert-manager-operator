package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func init() {
	SchemeBuilder.Register(&HTTP01Proxy{}, &HTTP01ProxyList{})
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true

// HTTP01ProxyList is a list of HTTP01Proxy objects.
type HTTP01ProxyList struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is the standard list's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	metav1.ListMeta `json:"metadata"`
	Items           []HTTP01Proxy `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=http01proxies,scope=Namespaced,categories={cert-manager-operator},shortName=http01proxy
// +kubebuilder:printcolumn:name="Mode",type="string",JSONPath=".spec.mode"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].message"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:metadata:labels={"app.kubernetes.io/name=http01proxy", "app.kubernetes.io/part-of=cert-manager-operator"}

// HTTP01Proxy describes the configuration for the HTTP01 challenge proxy
// that redirects traffic from the API endpoint on port 80 to ingress routers.
// This enables cert-manager to perform HTTP01 ACME challenges for API endpoint certificates.
// The name must be `default` to make HTTP01Proxy a singleton.
//
// When an HTTP01Proxy is created, the proxy DaemonSet is deployed on control plane nodes.
//
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default'",message="http01proxy is a singleton, .metadata.name must be 'default'"
// +operator-sdk:csv:customresourcedefinitions:displayName="HTTP01Proxy"
type HTTP01Proxy struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is the standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec is the specification of the desired behavior of the HTTP01Proxy.
	// +kubebuilder:validation:Required
	// +required
	Spec HTTP01ProxySpec `json:"spec"`

	// status is the most recently observed status of the HTTP01Proxy.
	// +kubebuilder:validation:Optional
	// +optional
	Status HTTP01ProxyStatus `json:"status,omitempty"`
}

// HTTP01ProxyMode controls how the HTTP01 challenge proxy is deployed.
// +kubebuilder:validation:Enum=DefaultDeployment;CustomDeployment
type HTTP01ProxyMode string

const (
	// HTTP01ProxyModeDefault enables the proxy with default configuration.
	HTTP01ProxyModeDefault HTTP01ProxyMode = "DefaultDeployment"

	// HTTP01ProxyModeCustom enables the proxy with user-specified configuration.
	HTTP01ProxyModeCustom HTTP01ProxyMode = "CustomDeployment"
)

// HTTP01ProxySpec is the specification of the desired behavior of the HTTP01Proxy.
// +kubebuilder:validation:XValidation:rule="self.mode == 'CustomDeployment' ? has(self.customDeployment) : !has(self.customDeployment)",message="customDeployment is required when mode is CustomDeployment and forbidden otherwise"
type HTTP01ProxySpec struct {
	// mode controls whether the HTTP01 challenge proxy is active and how it should be deployed.
	// DefaultDeployment enables the proxy with default configuration.
	// CustomDeployment enables the proxy with user-specified configuration.
	// +kubebuilder:validation:Required
	// +required
	Mode HTTP01ProxyMode `json:"mode"`

	// customDeployment contains configuration options when mode is CustomDeployment.
	// This field is only valid when mode is CustomDeployment.
	// +kubebuilder:validation:Optional
	// +optional
	CustomDeployment *HTTP01ProxyCustomDeploymentSpec `json:"customDeployment,omitempty"`
}

// HTTP01ProxyCustomDeploymentSpec contains configuration for custom proxy deployment.
type HTTP01ProxyCustomDeploymentSpec struct {
	// internalPort specifies the internal port used by the proxy service.
	// Valid values are 1024-65535.
	// +kubebuilder:validation:Minimum=1024
	// +kubebuilder:validation:Maximum=65535
	// +kubebuilder:default=8888
	// +optional
	InternalPort int32 `json:"internalPort,omitempty"`
}

// HTTP01ProxyStatus is the most recently observed status of the HTTP01Proxy.
type HTTP01ProxyStatus struct {
	// conditions holds information about the current state of the HTTP01 proxy deployment.
	ConditionalStatus `json:",inline,omitempty"`

	// proxyImage is the name of the image and the tag used for deploying the proxy.
	ProxyImage string `json:"proxyImage,omitempty"`
}
