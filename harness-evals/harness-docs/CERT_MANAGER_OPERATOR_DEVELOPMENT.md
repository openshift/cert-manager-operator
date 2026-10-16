# Cert Manager Operator - Development Guide

> Layout / controller comparison: [architecture/components.md](./architecture/components.md).  
> Agent playbooks: [*-guidelines.md](.) (operator-controllers, olm-packaging, fips, integration, …).

## Quick Start

- Go **1.26.0+** (`go.mod`), OpenShift + `KUBECONFIG`, container engine

```bash
make deploy
oc scale --replicas=0 deploy --all -n cert-manager-operator
make local-run   # RELATED_IMAGE_* + versions from Makefile
make build && make generate && make update-manifests && make verify
```

FIPS: `make build` sources `hack/go-fips.sh`. WARN (non-FIPS) builds are **local-only** — never CI/prod/image push. Keep `go.mod` replace `openshift/jetstack-cert-manager` lockstep with `CERT_MANAGER_VERSION`. See [fips-guidelines.md](./fips-guidelines.md).

## Common Tasks

### CertManager deployment overrides

1. Spec change → `certmanager_types.go` → `make manifests generate`
2. Wire in `deployment_*.go` / `deployment_overrides.go` (library-go — **not** TrustManager SSA)

### New controller-runtime operand (greenfield)

1. Copy **TrustManager** (SSA `Patch` + unique `FieldOwner`), not IstioCSR Create+Update
2. Feature gate — all five touchpoints ([ADR-0003](../../docs/adr/0003-feature-gates.md))
3. Image triple-sync (below) + bindata via `hack/update-*-manifests.sh` + `make update-manifests` / `verify-bindata`
4. RBAC via `+kubebuilder:rbac` → `make manifests` → `make bundle` (never hand-edit CSV RBAC)
5. Reuse `common.HandleReconcileResult` / `FromClientError` / validation helpers (`defaultRequeueTime=30s`)
6. E2E + Ginkgo labels under `test/e2e/`

### RELATED_IMAGE / relatedImages sync

| CSV `relatedImages.name` | Env | Consumer |
|--------------------------|-----|----------|
| `cert-manager-controller` / webhook / ca-injector / acmesolver | `RELATED_IMAGE_CERT_MANAGER_*` | `related_images.go` |
| `cert-manager-istiocsr` | `RELATED_IMAGE_CERT_MANAGER_ISTIOCSR` | `istiocsr/constants.go` |
| `cert-manager-trust-manager` | `RELATED_IMAGE_CERT_MANAGER_TRUST_MANAGER` | `trustmanager/constants.go` |

`RELATED_IMAGE_*` / `*_OPERAND_IMAGE_VERSION` literals in **`config/manager/manager.yaml` are hand-maintained** — bumping Makefile `CERT_*_VERSION` alone does **not** update them. Edit `manager.yaml` to match, then `make bundle` (auto-fills CSV `relatedImages` from `RELATED_IMAGE_*` env). New operand image also needs controller constants/map. See [olm-packaging-guidelines.md](./olm-packaging-guidelines.md).

### Bump operand versions

1. Makefile `CERT_MANAGER_VERSION` / `ISTIO_CSR_VERSION` / `TRUST_MANAGER_VERSION` (+ bundle version)
2. `make update-manifests` + keep jetstack replace version lockstep
3. **Manually** update matching `RELATED_IMAGE_*` / `*_OPERAND_IMAGE_VERSION` in `config/manager/manager.yaml`
4. `make bundle`; refresh CSV description links / RBAC if upstream changed; run `hack/verify-crds*.sh` directly if CRDs changed (not in `make verify-scripts`)

### Enable TrustManager locally

`--unsupported-addon-features=TrustManager=true`; create `TrustManager` named `cluster`.

## Common Mistakes

1. Hand-edit bindata / generated clients / generated `bundle/` CSV  
2. Assume SSA for IstioCSR or CertManager  
3. Logic in `certmanager_controller.go` placeholder  
4. Label-filtered cache for ConfigMaps or Issuer/ClusterIssuer  
5. Expect IstioCSR/TrustManager delete to GC operands (warn-only TODO)  
6. TLS 1.3 cipher-suite args (use `StripArgsByKeys`)  
7. Create CredentialsRequest in-operator (mount-only; controller Deployment only)  
8. Point cert-manager replace at upstream or ship non-FIPS image  

## Component-Specific Notes

| Topic | Detail |
|-------|--------|
| Namespaces | Operator `cert-manager-operator`; operands `cert-manager` |
| Cloud creds | AWS `/.aws` + `AWS_SDK_LOAD_CONFIG=1`; GCP ADC path — `../../docs/cloud_credentials.md` |
| Trusted CA | Fixed mount path; missing CM = retryable — `integration-guidelines.md` |
| Uninstall | Manual operand cleanup; `console.openshift.io/disable-operand-delete: "true"` |

## See Also

- [CERT_MANAGER_OPERATOR_TESTING.md](./CERT_MANAGER_OPERATOR_TESTING.md)
- [architecture/components.md](./architecture/components.md)
- [operator-controllers-guidelines.md](./operator-controllers-guidelines.md)
- [olm-packaging-guidelines.md](./olm-packaging-guidelines.md)
