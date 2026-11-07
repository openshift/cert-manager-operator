# Testing Guidelines

Repo-specific testing reference for `cert-manager-operator`. For generic testing philosophy, see the platform docs linked from `CERT_MANAGER_OPERATOR_TESTING.md`.

## Test Suites at a Glance

| Suite | Location | Make target | Framework |
|-------|----------|-------------|-----------|
| Unit | `pkg/**/*_test.go` | `make test-unit` | stdlib `testing` (+ table tests) |
| API / envtest | `test/apis`, `api/operator/v1alpha1/tests/**` | `make test-apis` | Ginkgo + envtest |
| E2E | `test/e2e/*_test.go` (build tag `e2e`) | `make test-e2e` | Ginkgo (labeled specs) |

`make test` runs `manifests generate vet test-apis test-unit` — it does **not** run e2e. Run `make test-e2e` separately against a live cluster.

## When to Use Which Suite

- **Unit (`pkg/...`)**: default choice for controller logic — reconcile branches, resource-builder functions (`getDeploymentObject`, `getServiceObject`, etc.), validation, and drift detection. Fast, no cluster/envtest needed. Use the counterfeiter fake (`fakes.FakeCtrlClient`) to isolate a single reconciler function.
- **API tests (`test/apis`)**: use when you change CRD schema, defaults, or CEL `XValidation` rules under `api/operator/v1alpha1`. These run real API-server admission (via envtest) against fixtures in `api/operator/v1alpha1/tests/{certmanagers,istiocsrs,trustmanagers}.operator.openshift.io/`. A unit test cannot validate CEL/OpenAPI schema enforcement — only envtest can.
- **E2E (`test/e2e`)**: use for anything that requires a real OpenShift cluster and real operands — Route/Ingress integration, actual cert-manager `Certificate`/`Issuer` issuance, IstioCSR/TrustManager install and drift-repair against a live deployment, feature-gate behavior, ServiceMesh smoke tests. Requires a running cluster with the operator already installed and cert-manager operands `Available` (see `test-e2e-wait-for-stable-state`).

Rule of thumb: if you can fake the client and assert a call count/patch payload, write a unit test. If the assertion needs a real informer/cache, real admission webhook, or a real pod running, it belongs in `test/apis` (schema only) or `test/e2e` (behavior).

## Make Targets

```bash
make test               # manifests + generate + vet + test-apis + test-unit
make test-unit           # go test ./... (excludes test/e2e, test/apis, test/utils)
make test-apis           # envtest + ginkgo against api/operator/v1alpha1/tests fixtures
make test-e2e            # ginkgo e2e suite against a live cluster (requires oc login)
make test-e2e-debug-cluster  # dump operator/operand pod status, CSVs, CRDs on failure
```

Focused unit runs:

```bash
go test -count=1 ./pkg/controller/trustmanager/...
go test -count=1 ./pkg/controller/istiocsr/...
go test -count=1 ./pkg/controller/certmanager/...
go test -count=1 ./pkg/features/...
```

Focused e2e runs use the `TEST` variable (passed to `-run`) plus the label filter:

```bash
make test-e2e TEST='TestE2E' E2E_GINKGO_LABEL_FILTER='Feature:TrustManager'
```

`test-unit` filters packages with `grep -vE 'test/(e2e|apis|utils)(/|$)'` — new top-level test helper packages under `test/` should follow the existing `test/apis`, `test/e2e`, `test/utils` naming so they're auto-excluded from unit runs.

## Unit Test Patterns Per Controller

Apply strategy differs **per controller stack** — assert the call that actually matches the controller under test, don't assume one apply pattern everywhere:

| Controller | Stack | Apply call to assert | Fake |
|------------|-------|----------------------|------|
| `pkg/controller/certmanager` | library-go `resourceapply` | `resourceapply.ApplyDeployment`/equivalent return values and `ExpectedGeneration`/`forceRollout` diffs | plain `library-go` fake clientset (no counterfeiter) |
| `pkg/controller/istiocsr` | ctrl-runtime, imperative | `Create` then `UpdateWithRetry` (see `services.go`, `deployments.go`) | `fakes.FakeCtrlClient` — assert `CreateCallCount`/`UpdateWithRetryCallCount`, **not** `PatchCallCount` |
| `pkg/controller/trustmanager` | ctrl-runtime **SSA** | `Patch(ctx, obj, client.Apply, client.FieldOwner("trust-manager-controller"), client.ForceOwnership)` | `fakes.FakeCtrlClient` — assert `PatchCallCount`, **not** `Update`/`Create` |

A TrustManager test asserting `UpdateCallCount` (or an IstioCSR test asserting `PatchCallCount`) is testing the wrong call path even if it happens to pass — verify against the controller's actual client method before trusting a green test.

Common shape for TrustManager/IstioCSR unit tests (table-driven, one `Reconciler` + fresh fake per case):

```go
mock := &fakes.FakeCtrlClient{}
mock.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
    // return drifted / matching / not-found object depending on subtest
})
r := testReconciler(t)
err := r.createOrApplyDeployment(cr, labels, annotations, hash)
assertError(t, err, tt.wantErr)
if got := mock.PatchCallCount(); got != tt.wantPatchCount { ... } // trustmanager
```

CertManager tests are library-go-oriented and live beside the controllers (`*_test.go` in `pkg/controller/certmanager/`); they don't use the counterfeiter fake at all — use the existing `test_constants_test.go` / `deployment_helper_test.go` fixtures instead of inventing a new fake client for that package.

## Fake Client (counterfeiter)

`common.CtrlClient` is the mockable seam:

```go
//go:generate go run github.com/maxbrunsfeld/counterfeiter/v6 -generate
//counterfeiter:generate -o fakes . CtrlClient
type CtrlClient interface {
	Get(context.Context, client.ObjectKey, client.Object) error
	List(context.Context, client.ObjectList, ...client.ListOption) error
	StatusUpdate(context.Context, client.Object, ...client.SubResourceUpdateOption) error
	Update(context.Context, client.Object, ...client.UpdateOption) error
	UpdateWithRetry(context.Context, client.Object, ...client.UpdateOption) error
	Create(context.Context, client.Object, ...client.CreateOption) error
	Delete(context.Context, client.Object, ...client.DeleteOption) error
	Patch(context.Context, client.Object, client.Patch, ...client.PatchOption) error
	Exists(context.Context, client.ObjectKey, client.Object) (bool, error)
}
```

- Regenerate after changing the interface: `make generate-fakes` (wraps `go generate ./...`).
- The fake lives at `pkg/controller/common/fakes/fake_ctrl_client.go` — **never hand-edit it**; re-run generation instead.
- Only IstioCSR and TrustManager (ctrl-runtime stack) use `fakes.FakeCtrlClient`. CertManager (library-go stack) does not implement/consume `CtrlClient`.
- Use `<Method>Calls(func(...) {...})` to stub behavior per subtest and `<Method>CallCount()` to assert invocation counts — this is the standard pattern across `pkg/controller/{istiocsr,trustmanager}/*_test.go`.

## API Tests (`make test-apis`)

- Runs `hack/test-apis.sh`, which invokes `ginkgo -r -v --randomize-all --randomize-suites --keep-going --timeout=30m ./test/apis`.
- Fixtures under `api/operator/v1alpha1/tests/{certmanagers,istiocsrs,trustmanagers}.operator.openshift.io/` pair a manifest with expected accept/reject outcomes — add a new fixture pair when adding/changing a CRD field or `XValidation` rule.
- Uses envtest (`setup-envtest` + `KUBEBUILDER_ASSETS`), not a real cluster — no operand pods, no controllers running, purely API-server admission behavior.

## E2E Tests (`test/e2e`)

Build-tagged `e2e`; run via `go test -tags e2e ./test/e2e/...` (wrapped by `make test-e2e`).

### Ginkgo Label Conventions

Every top-level `Describe` carries at least a `Platform:*` label; feature-gated suites add `Feature:*` and `TechPreview`:

```go
var _ = Describe("TrustManager", Ordered, Label("Platform:Generic", "Feature:TrustManager", "TechPreview"), func() { ... })
var _ = Describe("TrustManager with operator feature gate disabled", Ordered, Label("Platform:Generic", "Feature:TrustManager", "TechPreview:Inverted"), func() { ... })
var _ = Describe("Istio-CSR", Ordered, Label("Platform:Generic", "Feature:IstioCSR"), func() { ... })
```

Cloud-provider-specific `Context`s inside ACME DNS-01 tests add `Platform:<Cloud>` + `CredentialsMode:*`:

```go
Context("with AWS Route53", Label("Platform:AWS", "CredentialsMode:Mint"), func() { ... })
Context("with AWS Route53 in STS environment", Label("Platform:AWS", "CredentialsMode:Manual"), func() { ... })
```

Individual `It`s may add case IDs (`Label("ISTIOCSR-001")`, `Label("OSM-SMOKE-TC-001")`) for traceability. `TechPreview:Inverted` labels a suite that verifies behavior when a feature gate is **disabled**, distinct from the normal `TechPreview` (gate enabled) suite for the same feature — don't merge these into one `Describe`.

### Default & Custom Label Filters

The Makefile default runs AWS/Generic, Mint-credentials, non-ServiceMesh specs:

```make
E2E_GINKGO_LABEL_FILTER ?= Platform: isSubsetOf {AWS,Generic} && CredentialsMode: isSubsetOf {Mint} && !Feature:ServiceMesh
```

passed through as:

```make
-ginkgo.label-filter='$(E2E_GINKGO_LABEL_FILTER)'
```

**Quoting matters**: the filter is a single shell argument containing spaces, `{}`, `&&`, `!`. Always single-quote the whole `-ginkgo.label-filter='...'` value (as the Makefile does) — don't split it across unquoted `make VAR=...` tokens, or the shell will word-split on spaces/braces and silently pass a truncated filter. When overriding from the CLI, quote the override too:

```bash
make test-e2e E2E_GINKGO_LABEL_FILTER='Feature:TrustManager && !TechPreview:Inverted'
```

### Component-Specific E2E Tips

1. TrustManager e2e assumes the `TrustManager` feature gate is enabled (`--unsupported-addon-features=TrustManager=true`) unless running the `TechPreview:Inverted` suite.
2. Create operator CRs with singleton names matching production (`cluster` for CertManager/TrustManager, `default` for IstioCSR) — non-standard names are rejected by controller logic, not just tested.
3. Operand pods live in `cert-manager` namespace; operator logs in `cert-manager-operator`. `make test-e2e-wait-for-stable-state` gates the run on `cert-manager{,-cainjector,-webhook}` deployments being `Available`.
4. ServiceMesh smoke specs (`Feature:IstioCSR-ServiceMesh`) are excluded by the default filter (`!Feature:ServiceMesh`) — they need `E2E_OSM_ISTIO_VERSION`/`E2E_OSM_OPERATOR_VERSION` and a real OSM install; run them explicitly.
5. On failure, run `make test-e2e-debug-cluster` (or let the wait-for-stable-state step auto-trigger it) to dump operator/operand pod state, `ClusterOperator`, CSVs, and CRDs.

## Coverage Builds

`image-build-coverage`, `image-push-coverage`, `e2e-coverage-collect` build/collect coverage from an instrumented operator image running e2e — only needed when measuring e2e coverage, not for routine unit/API test iteration.

## What Not to Duplicate Here

Generic envtest bootstrap theory and OpenShift e2e framework tutorials belong in the platform `ai-docs`, not this file or `CERT_MANAGER_OPERATOR_TESTING.md`.

## See Also

- `CERT_MANAGER_OPERATOR_TESTING.md`
- `architecture/components.md` (apply-strategy table)
- [`docs/adr/0002-apply-strategies.md`](../../docs/adr/0002-apply-strategies.md)
