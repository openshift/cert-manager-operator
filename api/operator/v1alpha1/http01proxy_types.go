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

// HTTP01Proxy enables HTTP-01 ACME challenges for API endpoint certificates on BareMetal
// by applying nftables DNAT/SNAT rules via MachineConfig on control plane nodes.
// The name must be `cluster` to make HTTP01Proxy a singleton.
//
// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=http01proxies,scope=Cluster,categories={cert-manager-operator},shortName=http01proxy
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].message"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:metadata:labels={"app.kubernetes.io/name=http01proxy", "app.kubernetes.io/part-of=cert-manager-operator"}
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'cluster'",message="http01proxy is a singleton, .metadata.name must be 'cluster'"
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

// HTTP01ProxySpec is the specification of the desired behavior of the HTTP01Proxy.
// Spec is intentionally empty; presence of the CR enables the MachineConfig deployment.
type HTTP01ProxySpec struct{}

// HTTP01ProxyStatus is the most recently observed status of the HTTP01Proxy.
type HTTP01ProxyStatus struct {
	// conditions holds information about the current state of the HTTP01 proxy deployment.
	ConditionalStatus `json:",inline,omitempty"`
}
