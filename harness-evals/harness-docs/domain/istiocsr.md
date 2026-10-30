# IstioCSR

**API Group**: `operator.openshift.io/v1alpha1`  
**Kind**: `IstioCSR`  
**Scope**: Namespaced (singleton name **`default`** in the IstioCSR namespace)

**API Definition**: [api/operator/v1alpha1/istiocsr_types.go](https://github.com/openshift/cert-manager-operator/blob/master/api/operator/v1alpha1/istiocsr_types.go)

## Purpose

Deploys and configures the **cert-manager-istio-csr** operand so Istio mesh workloads can obtain certificates via cert-manager issuers.

**Key Principle**: Enabled by operator feature gate `IstioCSR` (GA, default **true**) via `--unsupported-addon-features`. Controller uses **Create + UpdateWithRetry**, not SSA.

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
| Apply | Exists? update via `UpdateWithRetry`; else Create | e.g. `services.go`, `deployments.go` |
| Singleton | Disallows multiple IstioCSR instances | `disallowMultipleIstioCSRInstances` |
| Watches | Certificate, Deployment, RBAC, Service, SA, ConfigMap, Secret (metadata), NetworkPolicy, Issuer, ClusterIssuer | `controller.go` |
| Labels | Managed label value `cert-manager-istio-csr`; watch label `istiocsr.openshift.operator.io/watched-by` | `constants.go` |
| Finalizer | `istiocsr.openshift.operator.io/cert-manager-istio-csr-controller` | `constants.go` |

## Reserved ConfigMap `app` labels

The operator uses the **`app`** label key (`common.ManagedResourceLabelKey`) so ConfigMaps can share a filtered controller-runtime cache. Do **not** set these values on ConfigMaps you reference in `spec.istioCSRConfig.certManager.istioCACertificate`:

| Reserved `app` value | Used for |
| -------------------- | -------- |
| `cert-manager-trust-manager` | TrustManager managed ConfigMaps |
| `cert-manager-istio-csr` | IstioCSR managed ConfigMaps and user CA ConfigMaps after reconcile (`updateWatchLabel`) |
| `cert-manager-operator` | Operator-level shared ConfigMaps (e.g. CNO-injected `cert-manager-operator-trusted-ca-bundle`) |

When IstioCSR references a user ConfigMap, the controller adds `app: cert-manager-istio-csr` and `istiocsr.openshift.operator.io/watched-by` in the same update. If the ConfigMap already has a **different** `app` value, the operator **overwrites** it with `cert-manager-istio-csr` (documented behavior). Labels are removed when the IstioCSR CR is deleted and the watch label matches that instance.

Routing for watched user ConfigMaps still uses the `watched-by` label value in the ConfigMap watch `mapFunc`; cache membership uses the `app` label after the first successful reconcile.

## Common Mistakes

1. Copying IstioCSR’s Create+Update path for new features — prefer TrustManager SSA.
2. Assuming cluster FeatureSet is required — IstioCSR is internal-gate only (`pkg/features/features.go`).
3. Filtering Issuer/ClusterIssuer cache by managed labels — `setup_manager.go` forbids this.

## Related Concepts

- [CertManager](./certmanager.md) | [TrustManager](./trustmanager.md) | [Enhancement](https://github.com/openshift/enhancements/blob/master/enhancements/cert-manager/istio-csr-controller.md)
