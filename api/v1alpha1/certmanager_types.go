/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

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
