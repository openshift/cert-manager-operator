package v1alpha1

import (
	apiv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CertManagerSpec defines the desired state of CertManager
type CertManagerSpec struct {
	apiv1.OperatorSpec `json:",inline"`
}

// CertManagerStatus defines the observed state of CertManager
type CertManagerStatus struct {
	apiv1.OperatorStatus `json:",inline"`
}

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster

// CertManager is the Schema for the certmanagers API
type CertManager struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:validation:Required
	// +required
	Spec CertManagerSpec `json:"spec,omitempty"`
	// +optional
	Status CertManagerStatus `json:"status,omitempty"`
}

type UnsupportedConfigOverrides struct {
	Controller UnsupportedConfigOverridesForCertManagerController `json:"controller,omitempty"`
	Webhook    UnsupportedConfigOverridesForCertManagerWebhook    `json:"webhook,omitempty"`
	CAInjector UnsupportedConfigOverridesForCertManagerCAInjector `json:"cainjector,omitempty"`
}

type UnsupportedConfigOverridesForCertManagerController struct {
	Args []string `json:"args,omitempty"`
}

type UnsupportedConfigOverridesForCertManagerWebhook struct {
	Args []string `json:"args,omitempty"`
}

type UnsupportedConfigOverridesForCertManagerCAInjector struct {
	Args []string `json:"args,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
//+kubebuilder:object:root=true

// CertManagerList contains a list of CertManager
type CertManagerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []CertManager `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CertManager{}, &CertManagerList{})
}
