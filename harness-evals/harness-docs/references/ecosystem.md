# Platform Ecosystem References

Links to generic OpenShift/Kubernetes patterns in the Platform hub ([openshift/enhancements/ai-docs](https://github.com/openshift/enhancements/tree/master/ai-docs)). Component-specific patterns live in this repository’s `harness-evals/harness-docs/`.

## Operator Patterns

**Location**: [ai-docs/platform/operator-patterns/](https://github.com/openshift/enhancements/tree/master/ai-docs/platform/operator-patterns)

- [Status Conditions](https://github.com/openshift/enhancements/blob/master/ai-docs/platform/operator-patterns/status-conditions.md)

**Component usage**:
- CertManager embeds `operatorv1.OperatorStatus` via library-go `OperatorClient`.
- IstioCSR/TrustManager use custom `ConditionalStatus` (`Ready`/`Degraded`) via `common.HandleReconcileResult` — outcome table in [architecture/components.md](../architecture/components.md).
- Deep rules: [operator-controllers-guidelines.md](../operator-controllers-guidelines.md), [error-handling-guidelines.md](../error-handling-guidelines.md).

## Testing Practices

**Location**: Platform practices tree is still sparse in the hub; prefer component guides.

**Component usage**: [CERT_MANAGER_OPERATOR_TESTING.md](../CERT_MANAGER_OPERATOR_TESTING.md) + [testing-guidelines.md](../testing-guidelines.md) (apply-path assertions, Ginkgo label quoting).

## Security Practices

**Location**: Prefer OpenShift product security docs + component network-policy enhancement.

**Component usage**:
- NetworkPolicy CEL: `defaultNetworkPolicy` ratchet; user `componentName` Enum = **CoreController only**.
- Trusted CA / TLS 1.3 strip / cloud-cred mount contracts — [security-guidelines.md](../security-guidelines.md), [integration-guidelines.md](../integration-guidelines.md).
- FIPS — [fips-guidelines.md](../fips-guidelines.md).

## Agent Guideline Index (repo)

| Domain | File |
|--------|------|
| Controllers / apply / cache | [operator-controllers-guidelines.md](../operator-controllers-guidelines.md) |
| Errors / status | [error-handling-guidelines.md](../error-handling-guidelines.md) |
| API / CEL | [api-contracts-guidelines.md](../api-contracts-guidelines.md) |
| OLM packaging | [olm-packaging-guidelines.md](../olm-packaging-guidelines.md) |
| Integration hooks | [integration-guidelines.md](../integration-guidelines.md) |
| Security | [security-guidelines.md](../security-guidelines.md) |
| Testing | [testing-guidelines.md](../testing-guidelines.md) |
| FIPS | [fips-guidelines.md](../fips-guidelines.md) |

## Reliability / Observability

**Component usage**: CSV enables cluster monitoring; operand metrics documented in [docs/operand_metrics.md](../../docs/operand_metrics.md).

## Kubernetes Fundamentals

**Location**: [ai-docs/domain/kubernetes/](https://github.com/openshift/enhancements/tree/master/ai-docs/domain/kubernetes)

- [CRDs](https://github.com/openshift/enhancements/blob/master/ai-docs/domain/kubernetes/crds.md)
- [Pod](https://github.com/openshift/enhancements/blob/master/ai-docs/domain/kubernetes/pod.md)
- [Service](https://github.com/openshift/enhancements/blob/master/ai-docs/domain/kubernetes/service.md)

**Component usage**: Operator ships upstream cert-manager + Bundle CRDs as operands; operator-owned CRDs are under `operator.openshift.io`.

## OpenShift Fundamentals

**Location**: [ai-docs/domain/openshift/](https://github.com/openshift/enhancements/tree/master/ai-docs/domain/openshift)

- [ClusterOperator](https://github.com/openshift/enhancements/blob/master/ai-docs/domain/openshift/clusteroperator.md)
- [ClusterVersion](https://github.com/openshift/enhancements/blob/master/ai-docs/domain/openshift/clusterversion.md)
- [Upgrade strategies](https://github.com/openshift/enhancements/blob/master/ai-docs/platform/openshift-specifics/upgrade-strategies.md)

**Component usage**: Optional Infrastructure/APIServer discovery for TLS profile and cloud platform; OLM lifecycle (`replaces`/`skipRange`) rather than CVO-managed ClusterOperator for this optional operator.

## Cross-Repository ADRs

**Location**: Platform `ai-docs/decisions/` may be incomplete; use hub [DESIGN_PHILOSOPHY.md](https://github.com/openshift/enhancements/blob/master/ai-docs/DESIGN_PHILOSOPHY.md) and [KNOWLEDGE_GRAPH.md](https://github.com/openshift/enhancements/blob/master/ai-docs/KNOWLEDGE_GRAPH.md).

**Component-specific ADRs**: [docs/adr/](../../../docs/adr/)

---

**Last Updated**: 2026-07-31
