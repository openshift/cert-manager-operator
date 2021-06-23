package v1alpha1

import (
	apiv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Certmanager provides information to configure an operator to manage certmanager.
type Certmanager struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:validation:Required
	// +required
	Spec CertmanagerSpec `json:"spec,omitempty"`
	// +optional
	Status CertmanagerStatus `json:"status,omitempty"`
}

type CertmanagerSpec struct {
	apiv1.OperatorSpec `json:",inline"`
}

type CertmanagerStatus struct {
	apiv1.OperatorStatus `json:",inline"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// CertmanagerList is a collection of items
type CertmanagerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Certmanager `json:"items"`
}
