---
status: accepted
applies_to:
  - pkg/controller/certmanager/
  - pkg/controller/istiocsr/
  - pkg/controller/trustmanager/
  - pkg/controller/common/
---

# 2. Per-Controller Resource Apply Strategy

Date: 2026-03-11

## Status

Accepted

## Context

CertManager uses library-go `resourceapply` / DeploymentController. IstioCSR (earlier ctrl-runtime operand) uses imperative **Create + UpdateWithRetry**. TrustManager (newest) uses **Server-Side Apply** with field owner `trust-manager-controller` and managed-field-aware skip helpers.

Documenting a single “we use SSA everywhere” claim is a known hallucination failure mode for this repo.

## Decision

1. CertManager continues with library-go apply helpers.
2. IstioCSR keeps Create+Update until explicitly migrated.
3. **New controller-runtime operand reconcilers must follow TrustManager SSA** (`Patch` + `client.Apply` + `FieldOwner` + `ForceOwnership`).
4. Status updates for IstioCSR/TrustManager may still use `UpdateWithRetry` on the CR object.

## Consequences

- Positive: Clear field ownership for TrustManager-managed resources.
- Positive: Shared `common.FromClientError` / `HandleReconcileResult` still apply.
- Negative: Three apply idioms in one repo.
- Negative: IstioCSR and TrustManager diverge — copy-paste across packages is unsafe.

## Alternatives considered

- Retrofit SSA onto IstioCSR immediately — not done at TrustManager introduction.
- Strategic merge patches only — rejected for TrustManager in favor of SSA.

## References

- TrustManager: `pkg/controller/trustmanager/services.go`, `deployments.go`, `constants.go` (`fieldOwner`)
- IstioCSR: `pkg/controller/istiocsr/services.go`, `deployments.go`
- CertManager: `pkg/controller/certmanager/cert_manager_networkpolicy.go` (`resourceapply.ApplyNetworkPolicy`)
