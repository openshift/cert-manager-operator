package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	certmanagermetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
)

func init() {
	SchemeBuilder.Register(&TrustManager{}, &TrustManagerList{})
}

// TrustManagerList contains a list of TrustManager resources.
//
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
type TrustManagerList struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is the standard list's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	metav1.ListMeta `json:"metadata"`
	Items           []TrustManager `json:"items"`
}

// TrustManager describes the configuration and information about the managed trust-manager deployment.
// The name must be `cluster` to make TrustManager a singleton, allowing only one instance per cluster.
// When a TrustManager CR is created, trust-manager operand is deployed in the cert-manager namespace.
//
// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=trustmanagers,scope=Cluster,categories={cert-manager-operator, trust-manager},shortName=trustmanager;tm
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].message"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:metadata:labels={"app.kubernetes.io/name=trustmanager", "app.kubernetes.io/part-of=cert-manager-operator"}
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'cluster'",message="TrustManager is a singleton, .metadata.name must be 'cluster'"
// +operator-sdk:csv:customresourcedefinitions:displayName="TrustManager"
type TrustManager struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is the standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +required
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec is the specification of the desired behavior of the TrustManager.
	// +kubebuilder:validation:Required
	// +required
	Spec TrustManagerSpec `json:"spec"`

	// status is the most recently observed status of the TrustManager.
	// +kubebuilder:validation:Optional
	// +optional
	Status TrustManagerStatus `json:"status,omitempty"`
}

// TrustManagerSpec defines the desired state of TrustManager.
// Note: trust-manager operand is always deployed in the cert-manager namespace.
type TrustManagerSpec struct {
	// trustManagerConfig configures the trust-manager operand's behavior.
	// +kubebuilder:validation:Required
	// +required
	TrustManagerConfig TrustManagerConfig `json:"trustManagerConfig"`

	// controllerConfig configures the operator's behavior for resource creation.
	// +kubebuilder:validation:Optional
	// +optional
	ControllerConfig TrustManagerControllerConfig `json:"controllerConfig,omitempty"`
}

// TrustManagerConfig configures the trust-manager operand's behavior.
type TrustManagerConfig struct {
	// logLevel configures the verbosity of trust-manager logging.
	// Follows [Kubernetes logging guidelines](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-instrumentation/logging.md#what-method-to-use).
	// +kubebuilder:default:=1
	// +kubebuilder:validation:Minimum:=1
	// +kubebuilder:validation:Maximum:=5
	// +kubebuilder:validation:Optional
	// +optional
	LogLevel int32 `json:"logLevel,omitempty"`

	// logFormat specifies the output format for trust-manager logging.
	// Supported formats are "text" and "json".
	// +kubebuilder:validation:Enum:="text";"json"
	// +kubebuilder:default:="text"
	// +kubebuilder:validation:Optional
	// +optional
	LogFormat string `json:"logFormat,omitempty"`

	// trustNamespace is the namespace where trust-manager looks for trust sources
	// (ConfigMaps and Secrets containing CA certificates).
	// Defaults to "cert-manager" if not specified.
	// This field is immutable once set.
	// This field can have a maximum of 63 characters.
	// +kubebuilder:default:="cert-manager"
	// +kubebuilder:validation:MinLength:=1
	// +kubebuilder:validation:MaxLength:=63
	// +kubebuilder:validation:XValidation:rule="oldSelf == '' || self == oldSelf",message="trustNamespace is immutable once set"
	// +kubebuilder:validation:Optional
	// +optional
	TrustNamespace string `json:"trustNamespace,omitempty"`

	// secretTargets configures whether trust-manager can write trust bundles to Secrets.
	// +kubebuilder:validation:Optional
	// +optional
	SecretTargets SecretTargetsConfig `json:"secretTargets,omitempty"`

	// filterExpiredCertificates controls whether trust-manager filters out
	// expired certificates from trust bundles before distributing them.
	// When set to "Enabled", expired certificates are removed from bundles.
	// When set to "Disabled", expired certificates are included (default behavior).
	// +kubebuilder:default:="Disabled"
	// +kubebuilder:validation:Enum:=Enabled;Disabled
	// +kubebuilder:validation:Optional
	// +optional
	FilterExpiredCertificates Mode `json:"filterExpiredCertificates,omitempty"`

	// filterNonCACerts controls whether trust-manager filters out
	// non-CA certificates from trust bundles before distributing them.
	// When set to "Enabled", only certificates with the X.509 basicConstraints
	// CA bit set are included in bundles.
	// When set to "Disabled", non-CA certificates are included (default behavior).
	// +kubebuilder:default:="Disabled"
	// +kubebuilder:validation:Enum:=Enabled;Disabled
	// +kubebuilder:validation:Optional
	// +optional
	FilterNonCACerts Mode `json:"filterNonCACerts,omitempty"`

	// defaultCAPackage configures the default CA package for trust-manager.
	// When enabled, the operator will use OpenShift's trusted CA bundle injection mechanism.
	// +kubebuilder:validation:Optional
	// +optional
	DefaultCAPackage DefaultCAPackageConfig `json:"defaultCAPackage,omitempty"`

	// resources defines the compute resource requirements for the trust-manager pod.
	// ref: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/
	// +kubebuilder:validation:Optional
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// affinity defines scheduling constraints for the trust-manager pod.
	// ref: https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/
	// +kubebuilder:validation:Optional
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// tolerations allows the trust-manager pod to be scheduled on tainted nodes.
	// ref: https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/
	// +listType=atomic
	// +kubebuilder:validation:MinItems:=0
	// +kubebuilder:validation:MaxItems:=50
	// +kubebuilder:validation:Optional
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// nodeSelector restricts which nodes the trust-manager pod can be scheduled on.
	// ref: https://kubernetes.io/docs/concepts/configuration/assign-pod-node/
	// +mapType=atomic
	// +kubebuilder:validation:MinProperties:=0
	// +kubebuilder:validation:MaxProperties:=50
	// +kubebuilder:validation:Optional
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// webhookTLS configures the cert-manager Certificate used for the
	// trust-manager validating webhook serving certificate.
	// +kubebuilder:validation:Optional
	// +optional
	WebhookTLS WebhookTLSConfig `json:"webhookTLS,omitempty"`
}

// SecretTargetsConfig configures whether and how trust-manager can write
// trust bundles to Secrets.
//
// +kubebuilder:validation:XValidation:rule="self.policy != 'Custom' || (has(self.authorizedSecrets) && size(self.authorizedSecrets) > 0)",message="authorizedSecrets must not be empty when policy is Custom"
// +kubebuilder:validation:XValidation:rule="self.policy == 'Custom' || !has(self.authorizedSecrets) || size(self.authorizedSecrets) == 0",message="authorizedSecrets must be empty when policy is not Custom"
type SecretTargetsConfig struct {
	// policy controls whether and how trust-manager can write trust bundles to Secrets.
	// Allowed values are "Disabled" or "Custom".
	// "Disabled" means trust-manager cannot write trust bundles to Secrets (default behavior).
	// "Custom" grants trust-manager read access  to all secrets cluster-wide,
	// and write access only to the secrets listed in authorizedSecrets.
	// +kubebuilder:default:="Disabled"
	// +kubebuilder:validation:Enum:=Disabled;Custom
	// +kubebuilder:validation:Optional
	// +optional
	Policy SecretTargetsPolicy `json:"policy,omitempty"`

	// authorizedSecrets is a list of specific secret names that trust-manager
	// is authorized to create and update. This field is only valid when policy is "Custom".
	// +listType=set
	// +kubebuilder:validation:MinItems:=0
	// +kubebuilder:validation:items:MinLength:=1
	// +kubebuilder:validation:Optional
	// +optional
	AuthorizedSecrets []string `json:"authorizedSecrets,omitempty"`
}

// WebhookTLSConfig configures the trust-manager webhook TLS certificate.
type WebhookTLSConfig struct {
	// certManager configures the cert-manager Certificate issued for the webhook.
	// When unset, the operator uses its self-signed Issuer and cert-manager defaults.
	// +kubebuilder:validation:Optional
	// +optional
	CertManager TrustManagerCertConfig `json:"certManager,omitempty"`

	// approverPolicy configures a CertificateRequestPolicy so that
	// cert-manager-approver-policy can auto-approve the webhook CertificateRequest.
	// Resources are created when bootstrapResources is Enabled and removed when it is Disabled.
	// If Enabled while the CertificateRequestPolicy CRD is not installed, reconciliation fails until
	// approver-policy is installed or bootstrapResources is set to Disabled.
	// +kubebuilder:validation:Optional
	// +optional
	ApproverPolicy ApproverPolicyConfig `json:"approverPolicy,omitempty"`
}

// TrustManagerCertConfig configures the cert-manager Certificate used for the
// trust-manager webhook serving certificate.
// +kubebuilder:validation:XValidation:rule="!has(self.privateKeySize) || self.privateKeySize == 0 || !has(self.privateKeyAlgorithm) || self.privateKeyAlgorithm == \"Ed25519\" || (self.privateKeyAlgorithm == \"RSA\" && self.privateKeySize in [2048, 4096, 8192]) || (self.privateKeyAlgorithm == \"ECDSA\" && self.privateKeySize in [256, 384, 521])",message="privateKeySize must match privateKeyAlgorithm"
type TrustManagerCertConfig struct {
	// certificateDuration is the requested validity period of the webhook TLS certificate.
	// When unset, cert-manager's default certificate duration is used.
	// Example: "8760h" for one year.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern=`^[-+]?(([0-9]+(\.[0-9]*)?|\.[0-9]+)(ns|us|µs|μs|ms|h|m|s))+$|^[-+]?0$`
	// +kubebuilder:validation:Optional
	// +optional
	CertificateDuration *metav1.Duration `json:"certificateDuration,omitempty"`

	// certificateRenewBefore is how long before expiry cert-manager renews the webhook certificate.
	// When unset, cert-manager renews at one third of the certificate lifetime.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern=`^[-+]?(([0-9]+(\.[0-9]*)?|\.[0-9]+)(ns|us|µs|μs|ms|h|m|s))+$|^[-+]?0$`
	// +kubebuilder:validation:Optional
	// +optional
	CertificateRenewBefore *metav1.Duration `json:"certificateRenewBefore,omitempty"`

	// issuerRef overrides the Issuer used for the webhook certificate.
	// When unset, the operator uses the self-signed Issuer it creates.
	// kind must be Issuer or ClusterIssuer. group must be cert-manager.io.
	// +kubebuilder:validation:XValidation:rule="self.kind.lowerAscii() == 'issuer' || self.kind.lowerAscii() == 'clusterissuer'",message="kind must be either 'Issuer' or 'ClusterIssuer'"
	// +kubebuilder:validation:XValidation:rule="self.group.lowerAscii() == 'cert-manager.io'",message="group must be 'cert-manager.io'"
	// +kubebuilder:validation:Optional
	// +optional
	IssuerRef *certmanagermetav1.ObjectReference `json:"issuerRef,omitempty"`

	// privateKeyAlgorithm is the private key algorithm for the webhook certificate.
	// Allowed values are RSA, ECDSA, and Ed25519.
	// +kubebuilder:validation:Enum=RSA;ECDSA;Ed25519
	// +kubebuilder:validation:Optional
	// +optional
	PrivateKeyAlgorithm string `json:"privateKeyAlgorithm,omitempty"`

	// privateKeyRotationPolicy controls whether a new private key is generated on re-issuance.
	// Allowed values are Always and Never.
	// +kubebuilder:validation:Enum=Always;Never
	// +kubebuilder:validation:Optional
	// +optional
	PrivateKeyRotationPolicy string `json:"privateKeyRotationPolicy,omitempty"`

	// privateKeySize is the private key size for the webhook certificate.
	// RSA allows 2048, 4096, and 8192. ECDSA allows 256, 384, and 521. Ed25519 ignores this field.
	// +kubebuilder:validation:Enum=256;384;521;2048;4096;8192
	// +kubebuilder:validation:Optional
	// +optional
	PrivateKeySize int32 `json:"privateKeySize,omitempty"`

	// certificateSignatureAlgorithm is the signature algorithm for the webhook certificate.
	// +kubebuilder:validation:Enum=SHA256WithRSA;SHA384WithRSA;SHA512WithRSA;ECDSAWithSHA256;ECDSAWithSHA384;ECDSAWithSHA512;PureEd25519
	// +kubebuilder:validation:Optional
	// +optional
	CertificateSignatureAlgorithm string `json:"certificateSignatureAlgorithm,omitempty"`

	// propagateMetadataToSecret copies the labels and annotations the operator
	// sets on the webhook Certificate onto that Certificate's Secret.
	// "Enabled" copies them. "Disabled" does not (default).
	// +kubebuilder:validation:Enum=Enabled;Disabled
	// +kubebuilder:validation:Optional
	// +optional
	PropagateMetadataToSecret Mode `json:"propagateMetadataToSecret,omitempty"`
}

// ApproverPolicyConfig controls creation and removal of a CertificateRequestPolicy for the
// trust-manager webhook certificate.
type ApproverPolicyConfig struct {
	// bootstrapResources controls whether the operator provisions the CertificateRequestPolicy
	// and associated RBAC required for approver-policy to approve the webhook certificate.
	//
	// Enabled creates the CertificateRequestPolicy, ClusterRole, and ClusterRoleBinding.
	// Disabled (default) does not create them. Flipping from Enabled to Disabled deletes those
	// resources. A missing CertificateRequestPolicy CRD is ignored while this is Disabled.
	//
	// Setting this to "Enabled" requires cert-manager approver-policy to be installed.
	// +kubebuilder:default:="Disabled"
	// +kubebuilder:validation:Enum:=Enabled;Disabled
	// +kubebuilder:validation:Optional
	// +optional
	BootstrapResources Mode `json:"bootstrapResources,omitempty"`
}

// DefaultCAPackageConfig configures the default CA package feature for trust-manager.
type DefaultCAPackageConfig struct {
	// policy controls whether the default CA package feature is enabled.
	// When set to "Enabled", the operator will inject OpenShift's trusted CA bundle
	// into trust-manager, enabling the "useDefaultCAs: true" source in Bundle resources.
	// When set to "Disabled", no default CA package is configured and Bundles cannot use useDefaultCAs (default behavior).
	// +kubebuilder:default:="Disabled"
	// +kubebuilder:validation:Enum:=Enabled;Disabled
	// +kubebuilder:validation:Optional
	// +optional
	Policy Mode `json:"policy,omitempty"`
}

// TrustManagerControllerConfig configures the operator's behavior for
// creating trust-manager resources.
type TrustManagerControllerConfig struct {
	// labels to apply to all resources created for the trust-manager deployment.
	// +mapType=granular
	// +kubebuilder:validation:MinProperties:=0
	// +kubebuilder:validation:MaxProperties:=25
	// +kubebuilder:validation:Optional
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// annotations to apply to all resources created for the trust-manager deployment.
	// +mapType=granular
	// +kubebuilder:validation:MinProperties:=0
	// +kubebuilder:validation:MaxProperties:=25
	// +kubebuilder:validation:Optional
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// SecretTargetsPolicy defines the policy for writing trust bundles to Secrets.
type SecretTargetsPolicy string

const (
	// SecretTargetsPolicyDisabled means trust-manager cannot write trust bundles to Secrets.
	SecretTargetsPolicyDisabled SecretTargetsPolicy = "Disabled"
	// SecretTargetsPolicyCustom grants trust-manager read access to all cluster secrets
	// and write access to only the secrets listed in authorizedSecrets.
	SecretTargetsPolicyCustom SecretTargetsPolicy = "Custom"
)

// TrustManagerStatus defines the observed state of TrustManager.
type TrustManagerStatus struct {
	// conditions holds information about the current state of the trust-manager deployment.
	ConditionalStatus `json:",inline,omitempty"`

	// trustManagerImage is the container image (name:tag) used for trust-manager.
	TrustManagerImage string `json:"trustManagerImage,omitempty"`

	// certificateRequestPolicy is the name of the CertificateRequestPolicy created
	// for the webhook certificate when webhookTLS.approverPolicy.bootstrapResources
	// is Enabled. Empty when bootstrapResources is Disabled.
	CertificateRequestPolicy string `json:"certificateRequestPolicy,omitempty"`
}
