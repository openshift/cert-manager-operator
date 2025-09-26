package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Mode indicates the operational state of the optional features.
type Mode string

const (
	// Enabled indicates the optional configuration is enabled.
	Enabled Mode = "Enabled"

	// Disabled indicates the optional configuration is disabled.
	Disabled Mode = "Disabled"

	// DisabledAndCleanup indicates the optional configuration is disabled and
	// created resources are automatically removed.
	DisabledAndCleanup Mode = "DisabledAndCleanup"
)

// PurgePolicy defines the policy for purging default resources.
type PurgePolicy string

const (
	// PurgeAll indicates to purge all the created resources.
	PurgeAll PurgePolicy = "PurgeAll"

	// PurgeNone indicates to purge none of the created resources.
	PurgeNone PurgePolicy = "PurgeNone"

	// PurgeExceptCertificates indicates to purge all the created resources except the Certificate resource.
	PurgeExceptCertificates PurgePolicy = "PurgeExceptCertificates"
)

// ConfigMapReference holds the details of a configmap.
type ConfigMapReference struct {
	// name of the ConfigMap.
	// +kubebuilder:validation:MinLength:=1
	// +kubebuilder:validation:MaxLength:=253
	// +kubebuilder:validation:XValidation:rule="!format.dns1123Subdomain().validate(self).hasValue()",message="name must consist of lowercase alphanumeric characters, hyphens ('-'), and periods ('.'). Each block, separated by periods, must start and end with an alphanumeric character. Hyphens are not allowed at the start or end of a block, and consecutive periods are not permitted."
	// +kubebuilder:validation:Required
	// +required
	Name string `json:"name"`

	// namespace in which the ConfigMap exists. If empty, ConfigMap will be looked up in IstioCSR created namespace.
	// +kubebuilder:validation:MinLength:=0
	// +kubebuilder:validation:MaxLength:=63
	// +kubebuilder:validation:XValidation:rule=`size(self) == 0 || !format.dns1123Label().validate(self).hasValue()`,message="namespace must consist of only lowercase alphanumeric characters and hyphens, and must start with an alphabetic character and end with an alphanumeric character."
	// +kubebuilder:validation:Optional
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// key name holding the required data.
	// +kubebuilder:validation:MinLength:=1
	// +kubebuilder:validation:MaxLength:=253
	// +kubebuilder:validation:Pattern:=^[-._a-zA-Z0-9]+$
	// +kubebuilder:validation:Required
	// +required
	Key string `json:"key"`
}

type ConditionalStatus struct {
	// conditions holds information about the current state of the istio-csr agent deployment.
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}
