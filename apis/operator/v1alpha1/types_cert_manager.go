package v1alpha1

import (
	apiv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// CertManager provides information to configure an operator to manage certmanager.
type CertManager struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:validation:Required
	// +required
	Spec CertManagerSpec `json:"spec,omitempty"`
	// +optional
	Status CertManagerStatus `json:"status,omitempty"`
}

type CertManagerSpec struct {
	apiv1.OperatorSpec `json:",inline"`
}

type CertManagerStatus struct {
	apiv1.OperatorStatus `json:",inline"`
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

// CertManagerList is a collection of items
type CertManagerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []CertManager `json:"items"`
}
