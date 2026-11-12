# ADR-0003: Operator Feature Gates via `--unsupported-addon-features`

**Status**: Accepted  
**Date**: 2026-03 (TrustManager cluster FeatureSet requirement removed — CM-1141)  
**Deciders**: cert-manager-operator maintainers

## Context

Optional controllers (IstioCSR, TrustManager) need runtime enablement without always-on cost. Gates are defined in `api/operator/v1alpha1/features.go` and applied through the operator flag `--unsupported-addon-features` (`pkg/features.SetupWithFlagValue`).

TrustManager was TechPreview and previously interacted with cluster `featuregates/cluster` / FeatureSet allow-listing. CM-1141 **dropped cluster FeatureSet gating** for TrustManager; enablement is internal gate only. `FeatureGateState` still reads cluster FeatureGate for discovery/error handling and retains unused `passesClusterPreviewGating`.

## Decision

- Expose operator-local gates: `IstioCSR` (GA, default true), `TrustManager` (GA, default true).  
- Wire enablement through `--unsupported-addon-features` (and CSV/deployment args in OLM).  
- Do **not** require cluster FeatureSet for TrustManager after CM-1141.  
- Retry FeatureGate discovery failures in `setupFeatureGates`; on persistent failure keep the operator running and gate optional controllers from `--unsupported-addon-features` only (cluster FeatureGate errors do not disable them).
- CRDs always install; only controllers are gated.

**Five-touchpoint wiring for any new gate**:

1. `api/operator/v1alpha1/features.go` — `Default` + `PreRelease`  
2. `pkg/features` — register / `SetupWithFlagValue`  
3. `pkg/operator/starter.go` — parse flag + optional FeatureGateState  
4. Runtime `Is*Enabled` check  
5. `setup_manager.go` — shared cache object list + reconciler registration  

Do not resurrect unused `passesClusterPreviewGating` without a new ADR.

## Consequences

**Positive**:
- TrustManager and IstioCSR are both on by default as GA; users can still disable either via `--unsupported-addon-features`.
- No cluster FeatureSet flip is required for TrustManager (CM-1141).

**Negative / Trade-offs**:
- Name `unsupported-addon-features` is easy to miss when searching for “featuregate”.
- Dead/unused cluster-preview helper may confuse readers of `features.go`.

## Alternatives Considered

- Cluster FeatureSet-only gating — removed for TrustManager (CM-1141).  
- Separate Deployment for each optional operand — higher OLM complexity.

## References

- `api/operator/v1alpha1/features.go`
- `pkg/features/features.go`
- `pkg/operator/starter.go` (`setupFeatureGates`)
- Commit/message: CM-1141 drops cluster FeatureSet gate for TP TrustManager

## SME Review Recommended

Operational guidance for catalog/CSV arg defaults; whether `passesClusterPreviewGating` should be deleted or reused.
