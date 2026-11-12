# Cert Manager Operator - Testing Guide

> **Generic Testing Practices**: See [Platform ai-docs](https://github.com/openshift/enhancements/tree/master/ai-docs). Deep playbook: [testing-guidelines.md](./testing-guidelines.md).

## Test Layout

| Suite | Location | How to run |
|-------|----------|------------|
| Unit | `pkg/**/*_test.go` | `make test-unit` |
| API / envtest | `test/apis`, `api/operator/v1alpha1/tests/**` | `make test-apis` |
| E2E | `test/e2e/` (build tag `e2e`, Ginkgo) | `make test-e2e` |

`make test` = manifests + generate + vet + test-apis + test-unit — **not** e2e.

## Unit Tests — assert the real apply path

| Controller | Assert | Fake |
|------------|--------|------|
| CertManager | library-go `resourceapply` / expected generation / rollout | library-go fake clientset — **no** `FakeCtrlClient` |
| IstioCSR | `CreateCallCount` / `UpdateWithRetryCallCount` | `fakes.FakeCtrlClient` — **not** `PatchCallCount` |
| TrustManager | `PatchCallCount` with `client.Apply` + FieldOwner `trust-manager-controller` | `fakes.FakeCtrlClient` — **not** Update/Create |

A TrustManager test asserting `UpdateCallCount` (or IstioCSR asserting `PatchCallCount`) is the wrong path even if green. Regenerate fakes with `make generate-fakes` after changing `CtrlClient` — never hand-edit `pkg/controller/common/fakes/`.

```bash
make test-unit
go test -count=1 ./pkg/controller/trustmanager/...
go test -count=1 ./pkg/controller/istiocsr/...
go test -count=1 ./pkg/controller/certmanager/...
```

## API Tests (`make test-apis`)

envtest + Ginkgo against CEL/`XValidation` fixtures under `api/operator/v1alpha1/tests/{certmanagers,istiocsrs,trustmanagers}.operator.openshift.io/`. Required after Spec validation changes — unit tests cannot enforce CEL.

## E2E Tests

```bash
make test-e2e
# Default label filter (Makefile) — always single-quote overrides:
# Platform: isSubsetOf {AWS,Generic} && CredentialsMode: isSubsetOf {Mint} && !Feature:ServiceMesh
make test-e2e E2E_GINKGO_LABEL_FILTER='Feature:TrustManager'
```

| Area | Labels |
|------|--------|
| TrustManager | `Feature:TrustManager` |
| TLS profile | `Feature:TLSProfile`, `TechPreview` |
| IstioCSR | `Feature:IstioCSR` |
| ApproverPolicyManager | `Feature:ApproverPolicyManager`, `TechPreview` |
| Gate-disabled ApproverPolicyManager | `TechPreview:Inverted` (do **not** merge with enabled suite) |
| ServiceMesh smoke | `Feature:IstioCSR-ServiceMesh` (excluded by default `!Feature:ServiceMesh`) |

Tips: singleton CR names (`cluster` / `default`); operands in `cert-manager` NS; `make test-e2e-debug-cluster` on failure.

## See Also

- [testing-guidelines.md](./testing-guidelines.md)
- [CERT_MANAGER_OPERATOR_DEVELOPMENT.md](./CERT_MANAGER_OPERATOR_DEVELOPMENT.md)
- [architecture/components.md](./architecture/components.md)
