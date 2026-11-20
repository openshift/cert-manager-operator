# Cert Manager Operator - Agentic Documentation

**Component**: Cert Manager Operator for OpenShift  
**Repository**: [openshift/cert-manager-operator](https://github.com/openshift/cert-manager-operator)  
**Default branch**: `master` | **Go**: 1.26.0

> **Retrieval-first**: Prefer `harness-evals/harness-docs/` for architecture, ADRs, and coding playbooks. Platform hub: [openshift/enhancements/ai-docs](https://github.com/openshift/enhancements/tree/master/ai-docs).

## What is Cert Manager Operator?

Deploys/configures upstream **cert-manager** (and optionally **istio-csr** / **trust-manager**) on OpenShift. Operator NS `cert-manager-operator`; operand NS `cert-manager` (hardcoded).

**Key Principle**: Dual-stack — library-go for CertManager; controller-runtime for IstioCSR/TrustManager. **Apply methods differ** (do not assume SSA everywhere).

## Core Components

| CR | Scope / name | Stack | Gate |
|----|--------------|-------|------|
| [CertManager](harness-evals/harness-docs/domain/certmanager.md) | Cluster / `cluster` | library-go `resourceapply` | Always on |
| [IstioCSR](harness-evals/harness-docs/domain/istiocsr.md) | Namespaced / `default` | SSA + Create/Update (mixed) | `IstioCSR` GA default true |
| [TrustManager](harness-evals/harness-docs/domain/trustmanager.md) | Cluster / `cluster` | **SSA** | `TrustManager` TP default false |

**Operands**: Certificate/Issuer/… → cert-manager; Bundle → trust-manager.  
**Quick Start**: `oc get certmanager cluster -o yaml` | `make local-run`

## Critical Patterns

1. **Never assume uniform SSA** — CertManager library-go; IstioCSR mixed (SSA via `common.ApplyResource` for Service/SA/Certificate/Role/RoleBinding/NetworkPolicy; Create+`UpdateWithRetry` for Deployment, CA ConfigMap, ClusterRole/Binding); TrustManager all SSA + field owner `trust-manager-controller`. [components.md](harness-evals/harness-docs/architecture/components.md)
2. **Never hand-edit generated assets** — bindata.go, clients, regenerated `bindata/` / `bundle/` (`make generate`, `update-manifests`, `bundle`, `verify-bindata`).
3. **Greenfield = TrustManager** — copy SSA (`common.ApplyResource`) + `HandleReconcileResult`/`FromClientError` (30s requeue); not IstioCSR. [ADR-0002](harness-evals/harness-docs/decisions/adr-0002-apply-strategies.md)
4. **Feature gates** — `--unsupported-addon-features`; five touchpoints; no cluster FeatureSet for TrustManager (CM-1141). [ADR-0003](harness-evals/harness-docs/decisions/adr-0003-feature-gates.md)
5. **Ignore** `certmanager_controller.go` placeholder — RBAC markers only, never started.
6. **RELATED_IMAGE triple-sync** — manager.yaml env ↔ controller constants ↔ CSV `relatedImages` + `make bundle`.
7. **Cache** — no label-filtered ConfigMap/Issuer caches; use predicates when unlabeled watches are required.
8. **TLS 1.3** — strip cipher args via `StripArgsByKeys`; nil profile = Intermediate; Infrastructure discovery gates TLS + cloud-cred hooks (APIServer shares that factory, not discovered separately).

## Key Paths

| Area | Path |
|------|------|
| Startup | `pkg/operator/starter.go` → CertManager set → optional ctrl-runtime manager |
| Shared | `pkg/controller/common` (`CtrlClient`, errors, TLS/validation) |
| Features | `api/operator/v1alpha1/features.go`, `pkg/features` |
| Images | `RELATED_IMAGE_CERT_MANAGER_{CONTROLLER,WEBHOOK,CA_INJECTOR,ACMESOLVER,ISTIOCSR,TRUST_MANAGER}` |
| ADRs | [0001](harness-evals/harness-docs/decisions/adr-0001-dual-controller-frameworks.md) · [0002](harness-evals/harness-docs/decisions/adr-0002-apply-strategies.md) · [0003](harness-evals/harness-docs/decisions/adr-0003-feature-gates.md) |

## Documentation Map

```text
harness-evals/
  harness-docs/   # architecture, domain, ADRs, *-guidelines, DEVELOPMENT, TESTING
  evals/          # OpenSpec stage eval gates (repo-assessment/plan/tasks/code-generation)
docs/{proxy,cloud_credentials,operand_metrics}.md  # human product docs
```

| Need | Start here |
|------|------------|
| Spec / CEL / NetworkPolicy enum | `harness-evals/harness-docs/domain/*.md` |
| Errors, cache, TLS, cloud, FIPS | `harness-evals/harness-docs/architecture/components.md` |
| Controllers / apply / gates | `harness-evals/harness-docs/operator-controllers-guidelines.md` |
| OLM / relatedImages | `harness-evals/harness-docs/olm-packaging-guidelines.md` + DEVELOPMENT |
| Unit assert Patch vs Update | `harness-evals/harness-docs/CERT_MANAGER_OPERATOR_TESTING.md` |
| FIPS build rules | `harness-evals/harness-docs/fips-guidelines.md` |

**AI Agent Path**: domain → components.md → matching `*-guidelines.md` in harness-docs → DEVELOPMENT/TESTING

**Also**: [DEVELOPMENT](harness-evals/harness-docs/CERT_MANAGER_OPERATOR_DEVELOPMENT.md) · [TESTING](harness-evals/harness-docs/CERT_MANAGER_OPERATOR_TESTING.md) · [enhancements](harness-evals/harness-docs/references/enhancements.md) · [ecosystem](harness-evals/harness-docs/references/ecosystem.md)

**Guideline index**: `harness-evals/harness-docs/{operator-controllers,error-handling,api-contracts,olm-packaging,integration,security,testing,fips}-guidelines.md`

**Platform**: [hub](https://github.com/openshift/enhancements/tree/master/ai-docs) · [operator-patterns](https://github.com/openshift/enhancements/tree/master/ai-docs/platform/operator-patterns) · [status-conditions](https://github.com/openshift/enhancements/blob/master/ai-docs/platform/operator-patterns/status-conditions.md)

## External References

- [Product docs](https://docs.openshift.com/container-platform/latest/security/cert_manager_operator/index.html)
- [Upstream cert-manager](https://cert-manager.io/docs/)
- [README](README.md) · [docs/proxy.md](docs/proxy.md) · [docs/cloud_credentials.md](docs/cloud_credentials.md) · [docs/operand_metrics.md](docs/operand_metrics.md)

---

**Platform Documentation**: [openshift/enhancements/ai-docs](https://github.com/openshift/enhancements/tree/master/ai-docs)
