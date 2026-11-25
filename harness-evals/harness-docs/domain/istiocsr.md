# IstioCSR

**API Group**: `operator.openshift.io/v1alpha1`  
**Kind**: `IstioCSR`  
**Scope**: Namespaced (singleton name **`default`** in the IstioCSR namespace)

**API Definition**: [api/operator/v1alpha1/istiocsr_types.go](https://github.com/openshift/cert-manager-operator/blob/master/api/operator/v1alpha1/istiocsr_types.go)

## Purpose

Deploys and configures the **cert-manager-istio-csr** operand so Istio mesh workloads can obtain certificates via cert-manager issuers.

**Key Principle**: Enabled by operator feature gate `IstioCSR` (GA, default **true**) via `--unsupported-addon-features`. Controller is **mixed**: SSA via `common.ApplyResource` for most resources, Create + `UpdateWithRetry` for the rest (see Apply below).

## Spec Structure

Verified in `istiocsr_types.go`:

```go
type IstioCSRSpec struct {
    IstioCSRConfig   IstioCSRConfig    `json:"istioCSRConfig"`
    ControllerConfig *ControllerConfig `json:"controllerConfig,omitempty"` // Labels map
}

type IstioCSRConfig struct {
    LogLevel                           int32
    LogFormat                          string
    IstioDataPlaneNamespaceSelector    string
    CertManager                        CertManagerConfig   // IssuerRef; optional IstioCACertificate ConfigMapReference
    IstiodTLSConfig                    IstiodTLSConfig     // CN, TrustDomain, DNSNames, durations, key algo/size, MaxCertificateDuration
    Server                             *ServerConfig       // ClusterID, Port
    Istio                              IstioConfig         // Revisions, Namespace
    Resources                          corev1.ResourceRequirements
    Affinity                           *corev1.Affinity
    Tolerations                        []corev1.Toleration
    NodeSelector                       map[string]string
}
```

`CertManagerConfig.IssuerRef` uses cert-manager `ObjectReference` / `IssuerReference` (`Name`, `Kind`, `Group`).  
`ConfigMapReference`: `Name`, `Namespace`, `Key` (`meta.go`).

## Status

```go
type IstioCSRStatus struct {
    ConditionalStatus `json:",inline,omitempty"` // Conditions []metav1.Condition
    IstioCSRImage         string
    IstioCSRGRPCEndpoint  string
    ServiceAccount        string
    ClusterRole           string
    ClusterRoleBinding    string
}
```

Condition types (`conditions.go`): `Ready`, `Degraded`. Reasons: `Failed`, `Ready`, `Progressing`.

## Lifecycle

1. **Enable**: Feature gate on → `setup_manager.go` registers `pkg/controller/istiocsr` on the unified controller-runtime manager.
2. **Reconcile**: Get CR → finalizer → install pipeline (SA, RBAC, Services, Certificates, Deployment, NetworkPolicies, …) → `common.HandleReconcileResult`.
3. **Delete**: Finalizer present; GA cleanup of operands is **TODO** (warn-only) — see `istiocsr/controller.go`.

## Component-Specific Behavior

| Concern | Behavior | Code |
|---------|----------|------|
| Image | `RELATED_IMAGE_CERT_MANAGER_ISTIOCSR` | `istiocsr/constants.go` |
| Apply | SSA via `common.ApplyResource` (field owner `istio-csr-controller`) for Service, ServiceAccount, Certificate, Role/RoleBinding, NetworkPolicy; Create + `UpdateWithRetry` for Deployment, CA ConfigMap, ClusterRole/ClusterRoleBinding | SSA: `services.go`, `serviceaccounts.go`, `certificates.go`, `rbacs.go`, `networkpolicies.go`; Create+Update: `deployments.go`, `rbacs.go` |
| Singleton | Disallows multiple IstioCSR instances | `disallowMultipleIstioCSRInstances` |
| Watches | Certificate, Deployment, RBAC, Service, SA, ConfigMap, Secret (metadata), NetworkPolicy, Issuer, ClusterIssuer | `controller.go` |
| Labels | Managed label value `cert-manager-istio-csr`; watch label `istiocsr.openshift.operator.io/watched-by` | `constants.go` |
| Finalizer | `istiocsr.openshift.operator.io/cert-manager-istio-csr-controller` | `constants.go` |

## Common Mistakes

1. Copying IstioCSR’s Create+Update path for new features — prefer TrustManager SSA.
2. Assuming cluster FeatureSet is required — IstioCSR is internal-gate only (`pkg/features/features.go`).
3. Filtering Issuer/ClusterIssuer cache by managed labels — `setup_manager.go` forbids this.

## Related Concepts

- [CertManager](./certmanager.md) | [TrustManager](./trustmanager.md) | [Enhancement](https://github.com/openshift/enhancements/blob/master/enhancements/cert-manager/istio-csr-controller.md)
