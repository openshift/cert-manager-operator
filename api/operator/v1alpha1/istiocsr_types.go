package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
)

func init() {
	SchemeBuilder.Register(&IstioCSR{}, &IstioCSRList{})
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true

// IstioCSRList is a list of IstioCSR objects.
type IstioCSRList struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is the standard list's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	metav1.ListMeta `json:"metadata"`
	Items           []IstioCSR `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=istiocsrs,scope=Namespaced,categories={cert-manager-operator, istio-csr, istiocsr},shortName=istiocsr;icsr
// +kubebuilder:printcolumn:name="GRPC Endpoint",type="string",JSONPath=".status.istioCSRGRPCEndpoint"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].message"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:metadata:labels={"app.kubernetes.io/name=istiocsr", "app.kubernetes.io/part-of=cert-manager-operator"}

// IstioCSR describes the configuration and information about the managed istio-csr agent.
// The name must be `default` to make IstioCSR a singleton that is, to allow only one instance of IstioCSR per namespace.
//
// When an IstioCSR is created, istio-csr agent is deployed in the IstioCSR-created namespace.
//
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default'",message="istiocsr is a singleton, .metadata.name must be 'default'"
// +operator-sdk:csv:customresourcedefinitions:displayName="IstioCSR"
type IstioCSR struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is the standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec is the specification of the desired behavior of the IstioCSR.
	// +kubebuilder:validation:Required
	// +required
	Spec IstioCSRSpec `json:"spec"`

	// status is the most recently observed status of the IstioCSR.
	// +kubebuilder:validation:Optional
	// +optional
	Status IstioCSRStatus `json:"status,omitempty"`
}

// IstioCSRSpec is the specification of the desired behavior of the IstioCSR.
type IstioCSRSpec struct {
	// istioCSRConfig configures the istio-csr agent's behavior.
	// +kubebuilder:validation:Required
	// +required
	IstioCSRConfig IstioCSRConfig `json:"istioCSRConfig"`

	// controllerConfig configures the controller for setting up defaults to enable the istio-csr agent.
	// +kubebuilder:validation:Optional
	// +optional
	ControllerConfig *ControllerConfig `json:"controllerConfig,omitempty"`
}

// IstioCSRConfig configures the istio-csr agent's behavior.
type IstioCSRConfig struct {
	// logLevel supports a value range as per [Kubernetes logging guidelines](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-instrumentation/logging.md#what-method-to-use).
	// +kubebuilder:default:=1
	// +kubebuilder:validation:Minimum:=1
	// +kubebuilder:validation:Maximum:=5
	// +kubebuilder:validation:Optional
	// +optional
	LogLevel int32 `json:"logLevel,omitempty"`

	// logFormat specifies the output format for istio-csr agent logging.
	// Supported log formats are text and json.
	// +kubebuilder:validation:Enum:="text";"json"
	// +kubebuilder:default:="text"
	// +kubebuilder:validation:Optional
	// +optional
	LogFormat string `json:"logFormat,omitempty"`

	// Istio-csr creates a ConfigMap named `istio-ca-root-cert` containing the root CA certificate, which the Istio data plane uses to verify server certificates. Its default behavior is to create and monitor ConfigMaps in all namespaces.
	// The istioDataPlaneNamespaceSelector restricts the namespaces where the ConfigMap is created by using label selectors, such as maistra.io/member-of=istio-system. This selector is also attached to all desired namespaces that are part of the data plane.
	// This field can have a maximum of 4096 characters.
	// +kubebuilder:example:="maistra.io/member-of=istio-system"
	// +kubebuilder:validation:MinLength:=0
	// +kubebuilder:validation:MaxLength:=4096
	// +kubebuilder:validation:Optional
	// +optional
	IstioDataPlaneNamespaceSelector string `json:"istioDataPlaneNamespaceSelector,omitempty"`

	// certManager is for configuring cert-manager specifics.
	// +kubebuilder:validation:Required
	// +required
	CertManager CertManagerConfig `json:"certManager"`

	// istiodTLSConfig is for configuring istiod certificate specifics.
	// +kubebuilder:validation:Required
	// +required
	IstiodTLSConfig IstiodTLSConfig `json:"istiodTLSConfig"`

	// server is for configuring the server endpoint used by istio for obtaining the certificates.
	// +kubebuilder:validation:Optional
	// +optional
	Server *ServerConfig `json:"server,omitempty"`

	// istio is for configuring the istio specifics.
	// +kubebuilder:validation:Required
	// +required
	Istio IstioConfig `json:"istio"`

	// resources is for defining the resource requirements.
	// Cannot be updated.
	// ref: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/
	// +kubebuilder:validation:Optional
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// affinity is for setting scheduling affinity rules.
	// ref: https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/
	// +kubebuilder:validation:Optional
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// tolerations is for setting the pod tolerations.
	// This field can have a maximum of 50 entries.
	// ref: https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/
	// +listType=atomic
	// +kubebuilder:validation:MinItems:=0
	// +kubebuilder:validation:MaxItems:=50
	// +kubebuilder:validation:Optional
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// nodeSelector is for defining the scheduling criteria using node labels.
	// This field can have a maximum of 50 entries.
	// ref: https://kubernetes.io/docs/concepts/configuration/assign-pod-node/
	// +mapType=atomic
	// +kubebuilder:validation:MinProperties:=0
	// +kubebuilder:validation:MaxProperties:=50
	// +kubebuilder:validation:Optional
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
}

// CertManagerConfig is for configuring cert-manager specifics.
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.issuerRef) && !has(self.issuerRef) || has(oldSelf.issuerRef) && has(self.issuerRef)",message="issuerRef may only be configured during creation"
type CertManagerConfig struct {
	// issuerRef contains details of the referenced object used for obtaining certificates.
	// When `issuerRef.Kind` is `Issuer`, it must exist in the `.spec.istioCSRConfig.istio.namespace`.
	// This field is immutable once set.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="issuerRef is immutable once set"
	// +kubebuilder:validation:XValidation:rule="self.kind.lowerAscii() == 'issuer' || self.kind.lowerAscii() == 'clusterissuer'",message="kind must be either 'Issuer' or 'ClusterIssuer'"
	// +kubebuilder:validation:XValidation:rule="self.group.lowerAscii() == 'cert-manager.io'",message="group must be 'cert-manager.io'"
	// +kubebuilder:validation:Required
	// +required
	IssuerRef certmanagerv1.ObjectReference `json:"issuerRef"`

	// istioCACertificate when provided, the operator will use the CA certificate from the specified ConfigMap.
	// If empty, the operator will automatically extract the CA certificate from the Secret containing the istiod certificate obtained from cert-manager.
	// +kubebuilder:validation:Optional
	// +optional
	IstioCACertificate *ConfigMapReference `json:"istioCACertificate,omitempty"`
}

// IstiodTLSConfig is for configuring istiod certificate specifics.
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.privateKeyAlgorithm) && !has(self.privateKeyAlgorithm) || has(oldSelf.privateKeyAlgorithm) && has(self.privateKeyAlgorithm)",message="privateKeyAlgorithm may only be configured during creation"
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.privateKeySize) && !has(self.privateKeySize) || has(oldSelf.privateKeySize) && has(self.privateKeySize)",message="privateKeySize may only be configured during creation"
// +kubebuilder:validation:XValidation:rule="(!has(self.privateKeyAlgorithm) || self.privateKeyAlgorithm == 'RSA') ? (self.privateKeySize in [2048,4096,8192]) : (self.privateKeySize in [256,384])",message="privateKeySize must match with configured privateKeyAlgorithm"
type IstiodTLSConfig struct {
	// commonName is the common name to be set in the cert-manager.io Certificate created for istiod.
	// The commonName will be of the form istiod.<istio_namespace>.svc when not set.
	// This field can have a maximum of 64 characters.
	// +kubebuilder:validation:MinLength:=0
	// +kubebuilder:validation:MaxLength:=64
	// +kubebuilder:example:="istiod.istio-system.svc"
	// +kubebuilder:validation:Optional
	// +optional
	CommonName string `json:"commonName,omitempty"`

	// trustDomain is the Istio cluster's trust domain, which will also be used for deriving the SPIFFE URI.
	// This field can have a maximum of 63 characters.
	// +kubebuilder:validation:MinLength:=1
	// +kubebuilder:validation:MaxLength:=63
	// +kubebuilder:validation:Required
	// +required
	TrustDomain string `json:"trustDomain"`

	// certificateDNSNames contains the additional DNS names to be added to the istiod certificate SAN.
	// This field can have a maximum of 25 entries.
	// +kubebuilder:validation:MinItems:=0
	// +kubebuilder:validation:MaxItems:=25
	// +kubebuilder:validation:items:MinLength:=1
	// +kubebuilder:validation:items:MaxLength:=253
	// +listType=set
	// +kubebuilder:validation:Optional
	// +optional
	CertificateDNSNames []string `json:"certificateDNSNames,omitempty"`

	// certificateDuration is the validity period for the istio-csr and istiod certificates.
	// +kubebuilder:default:="1h"
	// +kubebuilder:validation:Optional
	// +optional
	CertificateDuration *metav1.Duration `json:"certificateDuration,omitempty"`

	// certificateRenewBefore is the time before expiry to renew the istio-csr and istiod certificates.
	// +kubebuilder:default:="30m"
	// +kubebuilder:validation:Optional
	// +optional
	CertificateRenewBefore *metav1.Duration `json:"certificateRenewBefore,omitempty"`

	// privateKeySize is the key size for the istio-csr and istiod certificates. Allowed values when privateKeyAlgorithm is RSA are 2048, 4096, 8192; and for ECDSA, they are 256, 384.
	// This field is immutable once set.
	// +kubebuilder:validation:Enum:=256;384;2048;4096;8192
	// +kubebuilder:default:=2048
	// +kubebuilder:validation:XValidation:rule="oldSelf == 0 || self == oldSelf",message="privateKeySize is immutable once set"
	// +kubebuilder:validation:Optional
	// +optional
	PrivateKeySize int32 `json:"privateKeySize,omitempty"`

	// privateKeyAlgorithm is the algorithm to use when generating private keys. Allowed values are RSA, and ECDSA.
	// This field is immutable once set.
	// +kubebuilder:default:="RSA"
	// +kubebuilder:validation:Enum:="RSA";"ECDSA"
	// +kubebuilder:validation:XValidation:rule="oldSelf == '' || self == oldSelf",message="privateKeyAlgorithm is immutable once set"
	// +kubebuilder:validation:Optional
	// +optional
	PrivateKeyAlgorithm string `json:"privateKeyAlgorithm,omitempty"`

	// MaxCertificateDuration is the maximum validity duration that can be requested for a certificate.
	// +kubebuilder:default:="1h"
	// +kubebuilder:validation:Optional
	// +optional
	MaxCertificateDuration *metav1.Duration `json:"maxCertificateDuration,omitempty"`
}

// ServerConfig is for configuring the server endpoint used by istio
// for obtaining the certificates.
type ServerConfig struct {
	// clusterID is the Istio cluster ID used to verify incoming CSRs.
	// This field can have a maximum of 253 characters.
	// +kubebuilder:default:="Kubernetes"
	// +kubebuilder:validation:MinLength:=0
	// +kubebuilder:validation:MaxLength:=253
	// +kubebuilder:validation:Optional
	// +optional
	ClusterID string `json:"clusterID,omitempty"`

	// port to serve the istio-csr gRPC service.
	// +kubebuilder:default:=443
	// +kubebuilder:validation:Minimum:=1
	// +kubebuilder:validation:Maximum:=65535
	// +kubebuilder:validation:XValidation:rule="oldSelf == 0 || self == oldSelf",message="port is immutable once set"
	// +kubebuilder:validation:Optional
	// +optional
	Port int32 `json:"port,omitempty"`
}

// IstioConfig is for configuring the istio specifics.
type IstioConfig struct {
	// revisions are the Istio revisions that are currently installed in the cluster.
	// Changing this field will modify the DNS names that will be requested for the istiod certificate.
	// This field can have a maximum of 25 entries.
	// +listType=set
	// +kubebuilder:default:={"default"}
	// +kubebuilder:validation:MinItems=0
	// +kubebuilder:validation:MaxItems=25
	// +kubebuilder:validation:items:MinLength:=1
	// +kubebuilder:validation:items:MaxLength:=63
	// +kubebuilder:validation:Optional
	// +optional
	Revisions []string `json:"revisions,omitempty"`

	// namespace of the Istio control plane.
	// This field can have a maximum of 63 characters.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="namespace is immutable once set"
	// +kubebuilder:validation:MinLength:=1
	// +kubebuilder:validation:MaxLength:=63
	// +kubebuilder:validation:Required
	// +required
	Namespace string `json:"namespace"`
}

// ControllerConfig configures the controller for setting up defaults to
// enable the istio-csr agent.
type ControllerConfig struct {
	// labels to apply to all resources created for the istio-csr agent deployment.
	// This field can have a maximum of 20 entries.
	// +mapType=granular
	// +kubebuilder:validation:MinProperties:=0
	// +kubebuilder:validation:MaxProperties:=20
	// +kubebuilder:validation:Optional
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
}

// IstioCSRStatus is the most recently observed status of the IstioCSR.
type IstioCSRStatus struct {
	// conditions holds information about the current state of the istio-csr agent deployment.
	ConditionalStatus `json:",inline,omitempty"`

	// istioCSRImage is the name of the image and the tag used for deploying istio-csr.
	IstioCSRImage string `json:"istioCSRImage,omitempty"`

	// istioCSRGRPCEndpoint is the service endpoint of istio-csr, made available for users to configure in the istiod config to enable Istio to use istio-csr for certificate requests.
	IstioCSRGRPCEndpoint string `json:"istioCSRGRPCEndpoint,omitempty"`

	// serviceAccount created by the controller for the istio-csr agent.
	ServiceAccount string `json:"serviceAccount,omitempty"`

	// clusterRole created by the controller for the istio-csr agent.
	ClusterRole string `json:"clusterRole,omitempty"`

	// clusterRoleBinding created by the controller for the istio-csr agent.
	ClusterRoleBinding string `json:"clusterRoleBinding,omitempty"`
}
