# ADR-0002: Per-Controller Resource Apply Strategy

**Status**: Accepted  
**Date**: 2026-03-11 (TrustManager SSA; IstioCSR remains Create+Update)  
**Amended**: CM-1040 — shared `common.ApplyResource` helper; IstioCSR namespaced resources migrated to SSA  
**Deciders**: cert-manager-operator maintainers

## Context

CertManager uses library-go `resourceapply` / DeploymentController. IstioCSR (earlier ctrl-runtime operand) uses imperative **Create + UpdateWithRetry**. TrustManager (newest) uses **Server-Side Apply** with field owner `trust-manager-controller` and managed-field-aware skip helpers.

Documenting a single “we use SSA everywhere” claim is a known hallucination failure mode for this repo.

## Decision

1. CertManager continues with library-go apply helpers.  
2. IstioCSR uses SSA via `common.ApplyResource` for Service, ServiceAccount, Certificate, Role/RoleBinding and NetworkPolicy. Deployment, CA ConfigMap and ClusterRole/ClusterRoleBinding (generated names tracked in status) keep Create+Update until explicitly migrated.  
3. **New controller-runtime operand reconcilers must follow TrustManager SSA** (`common.ApplyResource` → `CtrlClient.Apply` + `FieldOwner` + `ForceOwnership`, with a JSON merge-patch fallback when a drift-correcting apply fails).  
4. Status updates for IstioCSR/TrustManager may still use `UpdateWithRetry` on the CR object.

## Consequences

**Positive**:
- Clear field ownership for TrustManager-managed resources.
- Shared `common.FromClientError` / `HandleReconcileResult` still apply.

**Negative / Trade-offs**:
- Three apply idioms in one repo.
- IstioCSR and TrustManager diverge — copy-paste across packages is unsafe.

## Alternatives Considered

- Retrofit SSA onto IstioCSR immediately — not done at TrustManager introduction.
- Strategic merge patches only — rejected for TrustManager in favor of SSA.

## References

- Shared helper: `pkg/controller/common/applier.go` (`ApplyResource`), `client.go` (`CtrlClient.Apply`)
- TrustManager: `pkg/controller/trustmanager/services.go`, `deployments.go`, `constants.go` (`fieldOwner`)
- IstioCSR: `pkg/controller/istiocsr/services.go` (SSA), `deployments.go` (Create+Update), `constants.go` (`fieldOwner`)
- CertManager: `pkg/controller/certmanager/cert_manager_networkpolicy.go` (`resourceapply.ApplyNetworkPolicy`)

## SME Review Recommended

Whether/when the remaining IstioCSR Create+Update resources migrate to SSA; field-owner naming convention for future operands.
