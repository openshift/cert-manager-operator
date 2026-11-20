# ADR-0003: Operator Feature Gates via `--unsupported-addon-features`

**Status**: Accepted  
**Date**: 2026-03 (TrustManager cluster FeatureSet requirement removed)  
**Deciders**: cert-manager-operator maintainers

## Context

Optional controllers (IstioCSR, TrustManager) need runtime enablement without always-on cost. Gates are defined in `api/operator/v1alpha1/features.go` and applied through the operator flag `--unsupported-addon-features` (`pkg/features.SetupWithFlagValue`).

TrustManager was TechPreview and previously interacted with cluster `featuregates/cluster` / FeatureSet allow-listing. That cluster FeatureSet gating was **dropped**; leftover discovery/`FeatureGateState`/`passesClusterPreviewGating` plumbing was later removed so enablement is internal-gate only for all optional controllers.

## Decision

- Expose operator-local gates: `IstioCSR` (GA, default true), `TrustManager` (GA, default true).  
- Wire enablement through `--unsupported-addon-features` (and CSV/deployment args in OLM).  
- Do **not** require or consult cluster FeatureSet for optional controllers.  
- `setupFeatureGates` only parses `--unsupported-addon-features`; invalid syntax fails startup.
- CRDs always install; only controllers are gated.

**Five-touchpoint wiring for any new gate**:

1. `api/operator/v1alpha1/features.go` — `Default` + `PreRelease`  
2. `pkg/features` — register / `SetupWithFlagValue` / package-level `Is*Enabled`  
3. `pkg/operator/starter.go` — parse flag via `setupFeatureGates`  
4. Runtime `Is*Enabled` check  
5. `setup_manager.go` — shared cache object list + reconciler registration  

Do not reintroduce cluster FeatureSet gating without a new ADR.

## Consequences

**Positive**:
- TrustManager and IstioCSR are both on by default as GA; users can still disable either via `--unsupported-addon-features`.
- No cluster FeatureSet flip is required for TrustManager.
- No unused cluster-preview helpers left in `pkg/features`.

**Negative / Trade-offs**:
- Name `unsupported-addon-features` is easy to miss when searching for “featuregate”.

## Alternatives Considered

- Cluster FeatureSet-only gating — removed for TrustManager.  
- Separate Deployment for each optional operand — higher OLM complexity.

## References

- `api/operator/v1alpha1/features.go`
- `pkg/features/features.go`
- `pkg/operator/starter.go` (`setupFeatureGates`)
- TrustManager cluster FeatureSet gate removal (see git history on `pkg/features`)

## SME Review Recommended

Operational guidance for catalog/CSV arg defaults.
