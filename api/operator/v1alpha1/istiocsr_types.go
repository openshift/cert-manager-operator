package v1alpha1

import (
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func init() {
	SchemeBuilder.Register(&IstioCSR{}, &IstioCSRList{})
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
//+kubebuilder:object:root=true

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
// +kubebuilder:subresource:status

// IstioCSR describes configuration and information about the managed istio-csr
// agent. The name must be `default`.
//
// When an IstioCSR is created, a new deployment is created which manages the
// istio-csr agent and keeps it in the desired state.
// +kubebuilder:object:root=true
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="GRPC Endpoint",type="string",JSONPath=".status.istioCSRGRPCEndpoint"
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default'",message="istiocsr is a singleton, .metadata.name must be 'default'"
// +operator-sdk:csv:customresourcedefinitions:displayName="IstioCSR"
type IstioCSR struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is the standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec is the specification of the desired behavior of the IstioCSR.
	Spec IstioCSRSpec `json:"spec,omitempty"`

	// status is the most recently observed status of the IstioCSR.
	Status IstioCSRStatus `json:"status,omitempty"`
}

// IstioCSRSpec is the specification of the desired behavior of the IstioCSR.
type IstioCSRSpec struct {
	// istioCSRConfig is for configuring the istio-csr agent behavior.
	IstioCSRConfig *IstioCSRConfig `json:"istioCSRConfig,omitempty"`

	// controllerConfig is for configuring the controller for setting up
	// defaults to enable istio-csr agent.
	ControllerConfig *ControllerConfig `json:"controllerConfig,omitempty"`
}

// IstioCSRConfig is for configuring the istio-csr agent behavior.
type IstioCSRConfig struct {
	// logLevel is for setting verbosity of istio-csr agent logging.
	// Supported log levels: 1-5.
	// +kubebuilder:default:=1
	// +kubebuilder:validation:Minimum:=1
	// +kubebuilder:validation:Maximum:=5
	// +optional
	LogLevel int32 `json:"logLevel,omitempty"`

	// logFormat is for specifying the output format of istio-csr agent logging.
	// Support log formats are text and json.
	// +kubebuilder:default:=text
	// +optional
	LogFormat string `json:"logFormat,omitempty"`

	// certmanager is for configuring cert-manager specifics.
	// +required
	CertManager *CertManagerConfig `json:"certmanager,omitempty"`

	// istiodTLSConfig is for configuring istiod certificate specifics.
	// +required
	IstiodTLSConfig *IstiodTLSConfig `json:"istiodTLSConfig,omitempty"`

	// server is for configuring the server endpoint used by istio
	// for obtaining the certificates.
	// +optional
	Server *ServerConfig `json:"server,omitempty"`

	// istio is for configuring the istio specifics.
	// +required
	Istio *IstioConfig `json:"istio,omitempty"`

	// resources is for defining the resource requirements.
	// Cannot be updated.
	// ref: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// affinity is for setting scheduling affinity rules.
	// ref: https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// tolerations is for setting the pod tolerations.
	// ref: https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/
	// +optional
	// +listType=atomic
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// nodeSelector is for defining the scheduling criteria using node labels.
	// ref: https://kubernetes.io/docs/concepts/configuration/assign-pod-node/
	// +optional
	// +mapType=atomic
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
}

// CertManagerConfig is for configuring cert-manager specifics.
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.issuerRef) && !has(self.issuerRef) || has(oldSelf.issuerRef) && has(self.issuerRef)",message="issuerRef may only be configured during creation"
type CertManagerConfig struct {
	// issuerRef contains details to the referenced object used for
	// obtaining the certificates. When issuerRef.Kind is Issuer, it must exist in the
	// .spec.istioCSRConfig.istio.namespace.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="issuerRef is immutable once set"
	// +required
	IssuerRef certmanagerv1.ObjectReference `json:"issuerRef,omitempty"`
}

// IstiodTLSConfig is for configuring istiod certificate specifics.
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.signatureAlgorithm) && !has(self.signatureAlgorithm) || has(oldSelf.signatureAlgorithm) && has(self.signatureAlgorithm)",message="signatureAlgorithm may only be configured during creation"
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.privateKeySize) && !has(self.privateKeySize) || has(oldSelf.privateKeySize) && has(self.privateKeySize)",message="privateKeySize may only be configured during creation"
type IstiodTLSConfig struct {
	// commonName is the common name to be set in the certificate.cert-manager.io
	// created for istiod. CommonName will be of the form `istiod.<istio_namespace>.svc`
	// when not set.
	// +optional
	CommonName string `json:"commonName,omitempty"`

	// trustDomain is the istio cluster's trust domain, which will also be used for deriving
	// spiffe URI.
	// +required
	TrustDomain string `json:"trustDomain,omitempty"`

	// certificateDNSNames contains the additional DNS names to be added to the istiod certificate SAN.
	// +listType=set
	// +optional
	CertificateDNSNames []string `json:"certificateDNSNames,omitempty"`

	// certificateDuration is the istio-csr and the istiod certificates validity period.
	// +kubebuilder:default:="1h"
	// +optional
	CertificateDuration *metav1.Duration `json:"certificateDuration,omitempty"`

	// certificateRenewBefore is the ahead time to renew the istio-csr and the istiod certificates
	// before expiry.
	// +kubebuilder:default:="30m"
	// +optional
	CertificateRenewBefore *metav1.Duration `json:"certificateRenewBefore,omitempty"`

	// privateKeySize is the istio-csr and the istiod certificate's key size. When the SignatureAlgorithm
	// is RSA, must be >= 2048 and for ECDSA, can only be 256 or 384, corresponding to P-256 and P-384 respectively.
	// +kubebuilder:default:=2048
	// +kubebuilder:validation:XValidation:rule="oldSelf == 0 || self == oldSelf",message="privateKeySize is immutable once set"
	// +optional
	PrivateKeySize int `json:"privateKeySize,omitempty"`

	// signatureAlgorithm is the signature algorithm to use when generating
	// private keys. At present only RSA and ECDSA are supported.
	// +kubebuilder:default:="RSA"
	// +kubebuilder:validation:Enum:="RSA";"ECDSA"
	// +kubebuilder:validation:XValidation:rule="oldSelf == '' || self == oldSelf",message="signatureAlgorithm is immutable once set"
	// +optional
	SignatureAlgorithm string `json:"signatureAlgorithm,omitempty"`

	// MaxCertificateDuration is the maximum validity duration that can be
	// requested for a certificate.
	// +kubebuilder:default:="1h"
	// +optional
	MaxCertificateDuration *metav1.Duration `json:"maxCertificateDuration,omitempty"`
}

// ServerConfig is for configuring the server endpoint used by istio
// for obtaining the certificates.
type ServerConfig struct {
	// port to serve istio-csr gRPC service.
	// +kubebuilder:default:=443
	// +kubebuilder:validation:XValidation:rule="oldSelf == 0 || self == oldSelf",message="port is immutable once set"
	// +optional
	Port int32 `json:"port,omitempty"`
}

// IstioConfig is for configuring the istio specifics.
type IstioConfig struct {
	// revisions are the istio revisions that are currently installed in the cluster.
	// Changing this field will modify the DNS names that will be requested for
	// the istiod certificate.
	// +listType=atomic
	// +kubebuilder:default:={"default"}
	// +kubebuilder:validation:XValidation:rule="self.all(x, x in oldSelf) && oldSelf.all(x, x in self)",message="revisions is immutable once set"
	// +kubebuilder:validation:MaxItems=10
	// +optional
	Revisions []string `json:"revisions,omitempty"`

	// namespace of the istio control-plane.
	// +kubebuilder:validation:XValidation:rule="oldSelf == '' || self == oldSelf",message="namespace is immutable once set"
	// +required
	Namespace string `json:"namespace,omitempty"`
}

// ControllerConfig is for configuring the controller for setting up
// defaults to enable istio-csr agent.
type ControllerConfig struct {
	// labels to apply to all resources created for istio-csr agent deployment.
	// +mapType=granular
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
}

// IstioCSRStatus is the most recently observed status of the IstioCSR.
type IstioCSRStatus struct {
	// conditions holds information of the current state of the istio-csr agent deployment.
	ConditionalStatus `json:",inline,omitempty"`

	// istioCSRImage is the name of the image and the tag used for deploying istio-csr.
	IstioCSRImage string `json:"istioCSRImage,omitempty"`

	// istioCSRGRPCEndpoint is the service endpoint of istio-csr made available for user
	// to configure the same in istiod config to enable istio to use istio-csr for
	// certificate requests.
	IstioCSRGRPCEndpoint string `json:"istioCSRGRPCEndpoint,omitempty"`

	// serviceAccount created by the controller for the istio-csr agent.
	ServiceAccount string `json:"serviceAccount,omitempty"`

	// clusterRoleBinding created by the controller for the istio-csr agent.
	ClusterRoleBinding string `json:"clusterRoleBinding,omitempty"`
}

type ConditionalStatus struct {
	// conditions holds information of the current state of the istio-csr agent deployment.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}
