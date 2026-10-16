---
status: accepted
applies_to:
  - pkg/operator/
  - pkg/controller/certmanager/
  - pkg/controller/istiocsr/
  - pkg/controller/trustmanager/
---

# 1. Dual Controller Frameworks (library-go + controller-runtime)

Date: 2025-02-01

## Status

Accepted

## Context

Core CertManager operand management was built on OpenShift **library-go** (`staticresourcecontroller`, `deploymentcontroller`, `OperatorClient` / `OperatorStatus`). Newer optional operands (IstioCSR, TrustManager) were added as feature-gated controllers on a unified **controller-runtime** manager (`pkg/operator/setup_manager.go`) with custom `ConditionalStatus`.

Migrating CertManager off library-go was not required for shipping optional operands; mixing frameworks avoids a large rewrite while allowing modern ctrl-runtime patterns for new CRDs.

## Decision

Keep **library-go** for CertManager (always-on). Use **controller-runtime** for IstioCSR and TrustManager, started when each gate's **resolved** value is enabled (`--unsupported-addon-features` / defaults — IstioCSR GA default true; TrustManager TechPreview default false).

## Consequences

- Positive: Incremental delivery of IstioCSR/TrustManager without rewriting CertManager.
- Positive: Shared ctrl-runtime helpers in `pkg/controller/common` (errors, status, client retry).
- Negative: Agents and contributors must not assume one framework or apply method.
- Negative: Two status models (`OperatorStatus` vs `ConditionalStatus`).
- Negative: Duplicate concepts (deployment overrides, image env) expressed differently per stack.

## Alternatives considered

- Rewrite CertManager on controller-runtime — high risk / deferred.
- Pure library-go for optional operands — poorer fit for multi-watch CR-centric install pipelines.

## References

- `pkg/operator/starter.go`, `pkg/operator/setup_manager.go`
- `pkg/controller/certmanager/`, `pkg/controller/istiocsr/`, `pkg/controller/trustmanager/`
- Enhancements: [istio-csr-controller](https://github.com/openshift/enhancements/blob/master/enhancements/cert-manager/istio-csr-controller.md), [trust-manager-controller](https://github.com/openshift/enhancements/blob/master/enhancements/cert-manager/trust-manager-controller.md)
