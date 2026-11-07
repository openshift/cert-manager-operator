# Enhancement Proposals & Design Docs

Catalog of design documentation. Enhancements are the source of truth; this file is an index only.

## OpenShift Enhancements (`openshift/enhancements`)

| Title | Status | Link |
|-------|--------|------|
| Cert-manager Network Policies | Implemented (API + controllers in-tree) | [cert-manager-network-policies.md](https://github.com/openshift/enhancements/blob/master/enhancements/cert-manager/cert-manager-network-policies.md) |
| Istio CSR Controller | Implemented (GA feature gate default on) | [istio-csr-controller.md](https://github.com/openshift/enhancements/blob/master/enhancements/cert-manager/istio-csr-controller.md) |
| Trust Manager Controller | Implemented (TechPreview, default off) | [trust-manager-controller.md](https://github.com/openshift/enhancements/blob/master/enhancements/cert-manager/trust-manager-controller.md) |

Related diagrams (IstioCSR): `istio-csr-create.puml` / `istio-csr-delete.puml` under the same enhancements directory.

## In-repo design / ops docs

| Doc | Purpose |
|-----|---------|
| [docs/proxy.md](../../docs/proxy.md) | Cluster proxy injection into operands |
| [docs/cloud_credentials.md](../../docs/cloud_credentials.md) | Cloud credential secret mounting (not CredentialsRequest creation) |
| [docs/operand_metrics.md](../../docs/operand_metrics.md) | Operand metrics / monitoring notes |
| [README.md](../../README.md) | Architecture assumptions, local-run, image build |

## Notes

- ADRs in [`docs/adr/`](../../../docs/adr/) capture **component architectural** choices (framework split, apply strategy, feature gates). Do not duplicate enhancement prose here.
- Cross-component platform proposals mentioning cert-manager (OLM platform operators, Route external certs, etc.) are out of scope for this index unless they change this operator’s API or controllers.
