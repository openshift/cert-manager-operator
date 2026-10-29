# TrustManager

**API Group**: `operator.openshift.io/v1alpha1`  
**Kind**: `TrustManager`  
**Scope**: Cluster (singleton name **`cluster`**)

**API Definition**: [api/operator/v1alpha1/trustmanager_types.go](https://github.com/openshift/cert-manager-operator/blob/master/api/operator/v1alpha1/trustmanager_types.go)

## Purpose

Deploys and configures the **trust-manager** operand, which reconciles cluster-scoped `Bundle` (`trust.cert-manager.io/v1alpha1`) resources to distribute CA bundles into ConfigMaps/Secrets.

**Key Principle**: TechPreview feature gate `TrustManager` (default **false**). Reference controller-runtime implementation using **Server-Side Apply** with field owner `trust-manager-controller`.

## Spec Structure

Verified in `trustmanager_types.go`:

```go
type TrustManagerSpec struct {
    TrustManagerConfig TrustManagerConfig             `json:"trustManagerConfig"`
    ControllerConfig   TrustManagerControllerConfig   `json:"controllerConfig,omitempty"` // Labels, Annotations
}

type TrustManagerConfig struct {
    LogLevel                     int32
    LogFormat                    string
    TrustNamespace               string
    SecretTargets                SecretTargetsConfig          // Policy; AuthorizedSecrets
    FilterExpiredCertificates    FilterExpiredCertificatesPolicy
    DefaultCAPackage             DefaultCAPackageConfig       // Policy
    Resources                    corev1.ResourceRequirements
    Affinity                     *corev1.Affinity
    Tolerations                  []corev1.Toleration
    NodeSelector                 map[string]string
}
```

Policy enums: `Enabled` / `Disabled`. SecretTargets policies include `Disabled` / `Custom` (see types for exact consts).

## Status

```go
type TrustManagerStatus struct {
    ConditionalStatus `json:",inline,omitempty"`
    TrustManagerImage                   string
    TrustNamespace                      string
    SecretTargetsPolicy                 SecretTargetsPolicy
    DefaultCAPackagePolicy              DefaultCAPackagePolicy
    FilterExpiredCertificatesPolicy     FilterExpiredCertificatesPolicy
}
```

Same `Ready` / `Degraded` condition types as IstioCSR (`conditions.go`).

## Lifecycle

1. **Enable**: `--unsupported-addon-features=TrustManager=true` → `IsTrustManagerFeatureGateEnabled()` → `setupTrustManagerController` (`setup_manager.go`).
2. **Install pipeline** (`install_trustmanager.go`): validate → CA ConfigMap → SA → RBAC → Services → Issuer → Certificate → Deployment → ValidatingWebhook → status observed fields.
3. **Delete**: Finalizer TODO / warn-only cleanup (same class of gap as IstioCSR).

## Component-Specific Behavior

| Concern | Behavior | Code |
|---------|----------|------|
| Image | `RELATED_IMAGE_CERT_MANAGER_TRUST_MANAGER` | `trustmanager/constants.go` |
| Apply | `common.ApplyResource` → `CtrlClient.Apply(..., FieldOwner("trust-manager-controller"), ForceOwnership)` | `services.go`, `deployments.go`, `common/applier.go` |
| Skip when unchanged | Managed-field-aware `*Modified` helpers before apply | `utils.go` / resource files |
| Trusted CA | Watches CNO-injected ConfigMap `cert-manager-operator-trusted-ca-bundle` (separate from CertManager `--trusted-ca-configmap`) | `controller.go`, `configmaps.go` |
| SecretTargets | `Disabled` (default) or `Custom`. Custom requires non-empty `authorizedSecrets`; write RBAC scoped via `ResourceNames` (cluster-wide read is intentional). Sort secrets before ClusterRole apply for deterministic diffs. | `rbacs.go` `appendSecretTargetRules` |
| Bundle CRD | Shipped in `config/crd/`; reconciled by trust-manager operand, not operator primary loop | `config/crd/bases/customresourcedefinition_bundles...` |
| Finalizer | `trustmanager.openshift.operator.io/cert-manager-trust-manager-controller` | `constants.go` |

**Cluster FeatureSet**: Previously required for TechPreview; **removed** (CM-1141 / `pkg/features` comments). Enablement is internal gate only.

## Common Mistakes

1. Documenting “all controllers use SSA” — false; CertManager uses library-go, and IstioCSR still uses Create+Update for some resources.
2. Expecting Bundle reconciliation in the operator — operator deploys trust-manager; Bundle is operand API.
3. Hand-editing `bindata/trust-manager` YAML without `make update-manifests`.
4. Granting wildcard Secret **write** for SecretTargets — keep write limited to `authorizedSecrets` ResourceNames.

## Related Concepts

- [CertManager](./certmanager.md) | [IstioCSR](./istiocsr.md) | [Enhancement](https://github.com/openshift/enhancements/blob/master/enhancements/cert-manager/trust-manager-controller.md)
