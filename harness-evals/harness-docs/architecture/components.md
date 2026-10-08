# Architecture — Cert Manager Operator

Dual-stack OpenShift operator: **library-go** for core CertManager operand; **controller-runtime** manager for feature-gated IstioCSR and TrustManager.

## Repo Layout (source of truth)

```text
api/operator/v1alpha1/     # CertManager, IstioCSR, TrustManager types + feature gates
bindata/                   # Operand YAML (cert-manager, istio-csr, trust-manager, networkpolicies) — regenerated
bundle/                    # OLM CSV + CRDs
config/                    # Kustomize sources for manifests/bundle
docs/                      # Human product docs (proxy, cloud_credentials, operand_metrics)
harness-evals/harness-docs/ # Agentic docs + *-guidelines.md (OpenSpec /opsx-constitute)
hack/                      # update-*-manifests, go-fips, clientgen, verify scripts
pkg/
  cmd/operator/            # Cobra start + flags
  controller/
    certmanager/           # library-go controller set (always on)
    istiocsr/              # ctrl-runtime Create+Update
    trustmanager/          # ctrl-runtime SSA (reference)
    common/                # CtrlClient, ReconcileError, HandleReconcileResult, validation, TLS hook
  features/                # --unsupported-addon-features + FeatureGateState
  operator/                # RunOperator, setup_manager, bindata.go, generated clients
  tlsprofile/              # APIServer TLS profile → operand args
test/{apis,e2e}/           # envtest CRD suites + Ginkgo e2e
```

**DO NOT** hand-edit: `pkg/operator/assets/bindata.go`, `pkg/operator/{clientset,informers,listers,applyconfigurations}/**`, regenerated bindata YAML. Use `make generate`, `make update-manifests`, `make verify-bindata`.

**DO NOT** start from `pkg/controller/certmanager/certmanager_controller.go` — unused placeholder for RBAC annotations (“Needs to be deleted later”).

## Startup Sequence

`main.go` → `pkg/cmd/operator` → `controllercmd` → `operator.RunOperator` (`pkg/operator/starter.go`):

1. Clients + informer factories + `OperatorClient` (+ optional Infrastructure discovery)
2. **Construct** `NewCertManagerControllerSet` + `NewDefaultCertManagerController` (controllers not running yet)
3. **Start informers** (`informer.Start`, including optional infra factory when `Applicable()`)
4. **Run** each library-go controller (`go controller.Run`)
5. `setupFeatureGates` from `--unsupported-addon-features` + optional `featuregates/cluster` read (retries discovery failures; does not abort operator on persistent FeatureGate errors; optional controllers follow internal flags only)
6. If IstioCSR and/or TrustManager enabled → `NewControllerManager` → Start in goroutine
7. Block on `ctx.Done()`

Flags (`pkg/cmd/operator/cmd.go`): `--trusted-ca-configmap`, `--cloud-credentials-secret`, `--unsupported-addon-features`.

Note: Informers start **before** library-go controllers run — do not assume first reconcile races ahead of cache sync.

## Controller Comparison (critical)

| Dimension | CertManager | IstioCSR | TrustManager |
|-----------|-------------|----------|--------------|
| Framework | library-go | controller-runtime | controller-runtime |
| CR | Cluster `cluster` | Namespaced `default` | Cluster `cluster` |
| Apply | `resourceapply` / DeploymentController | Create + `UpdateWithRetry` | **SSA** `client.Apply` + `ForceOwnership` |
| Field owner | n/a (library-go) | n/a | `trust-manager-controller` |
| Status | `operatorv1.OperatorStatus` | `ConditionalStatus` | `ConditionalStatus` |
| Gate | Always on | `IstioCSR` GA default true | `TrustManager` TP default false |
| Finalizer cleanup | library-go | TODO warn-only | TODO warn-only |

Evidence: TrustManager `services.go` / `deployments.go` (`Patch` + `client.Apply`); IstioCSR `services.go` / `deployments.go` (`UpdateWithRetry`); CertManager `cert_manager_networkpolicy.go` (`resourceapply.ApplyNetworkPolicy`).

**Greenfield rule**: New controller-runtime operands → copy **TrustManager**, not IstioCSR.

## CertManager Controller Set

Wired in `cert_manager_controller_set.go` + `starter.go`:

1. Controller static resources + deployment  
2. Webhook static resources + deployment  
3. CAInjector static resources + deployment  
4. NetworkPolicy static + user-defined  
5. DefaultCertManager  

Shared deployment hooks (`deployment_overrides.go`, `generic_deployment_controller.go`): proxy env (`operator-lib/proxy`), trusted-CA volume, optional cloud-credentials mount, SA bound token, cluster TLS profile args — **only when** the optional Infrastructure informer factory is `Applicable()` (APIServer shares it; absence ⇒ skip those hooks, don’t crash).

## Feature Gates

Defined in `api/operator/v1alpha1/features.go`; registered in `pkg/features`.

| Gate | Default | PreRelease | Runtime check |
|------|---------|------------|---------------|
| `IstioCSR` | true | GA | `IsIstioCSRFeatureGateEnabled()` — internal only |
| `TrustManager` | false | TechPreview | `FeatureGateState.IsTrustManagerFeatureGateEnabled()` — internal only; cluster FeatureSet gating **removed** (CM-1141) |

`passesClusterPreviewGating` still exists in `features.go` but is **unused** — do not resurrect without an ADR.

**Wire a new gate (all five)**: (1) `features.go` Default+PreRelease → (2) `pkg/features` → (3) `starter.go` `--unsupported-addon-features` → (4) runtime `Is*Enabled` → (5) `setup_manager.go` cache + reconciler. CRDs ship unconditionally; only the controller is gated.

## Image Resolution

Makefile versions: `CERT_MANAGER_VERSION` (v1.20.3), `ISTIO_CSR_VERSION` (v0.16.0), `TRUST_MANAGER_VERSION` (v0.20.3).

OLM injects env (CSV / `config/manager/manager.yaml`):

- `RELATED_IMAGE_CERT_MANAGER_{WEBHOOK,CA_INJECTOR,CONTROLLER,ACMESOLVER,ISTIOCSR,TRUST_MANAGER}`
- `OPERAND_IMAGE_VERSION`, `ISTIOCSR_OPERAND_IMAGE_VERSION`, `TRUSTMANAGER_OPERAND_IMAGE_VERSION`, `OPERATOR_IMAGE_VERSION`

CertManager remaps quay substrings → RELATED_IMAGE_* in `related_images.go`.

## Errors & Status (ctrl-runtime path)

Use `pkg/controller/common` (`ReconcileError`, `FromClientError`, `HandleReconcileResult`) for **IstioCSR/TrustManager business logic only**. Call `HandleReconcileResult` once at the end of `processReconcileRequest` (both use `defaultRequeueTime = 30s`). Do **not** import these into library-go CertManager sync (any `error` is retried; no irrecoverable concept).

| `reconcileErr` | Degraded | Ready | `ctrl.Result` |
|----------------|----------|-------|---------------|
| `nil` | False / Ready | True / Ready | `{}` |
| `RetryRequiredError` | False / Ready | False / Progressing | `{RequeueAfter: 30s}` |
| `IrrecoverableError` | True / Failed | False / Failed | `{}` (no requeue until next event) |

**`FromClientError`**: Unauthorized/Forbidden/Invalid/BadRequest/ServiceUnavailable → Irrecoverable; **everything else** (incl. NotFound/Conflict) → Retry. Don’t special-case NotFound/Conflict in reconcile — wrap with `FromClientError`.

**`MultipleInstanceError`**: **IstioCSR only** (namespaced singleton; CEL can’t see siblings). Set Ready=False on the rejected instance, emit Warning, **`err = nil`** before returning — do **not** route through `HandleReconcileResult`. TrustManager is cluster-scoped `cluster` — Kubernetes name uniqueness + CEL name-lock already prevent duplicates.

**Status quirks**: `SetCondition` counts as changed only if Status/Reason change (message-only ≠ write). `HandleReconcileResult` skips status write if neither Degraded nor Ready changed. If status write fails on an irrecoverable path, ctrl-runtime may still retry — accepted side effect.

## Shared Utilities (exact symbols)

**`pkg/controller/common`**: `ManagedResourceLabelKey` (`app`), `OperatorNamespace`, `TrustedCABundleConfigMapName`, `TrustedCABundleKey`; `CtrlClient`, `NewClient`, `UpdateWithRetry`; error constructors/predicates; `HandleReconcileResult`; `MergeContainerArgs`, `ParseArgMap`, `StripArgsByKeys`, `ArgKeysSet`; `WithClusterTLSProfileFromAPIServer`; validation helpers; metadata helpers (`UpdateName`/`UpdateNamespace`/`UpdateResourceLabels`, `ObjectMetadataModified`, …).

**`pkg/features`**: `SetupWithFlagValue`, `NewFeatureGateState`, `IsIstioCSRFeatureGateEnabled`, `(*FeatureGateState).IsTrustManagerFeatureGateEnabled`, `DefaultFeatureGate`, `FeatureSetOKD`.

**`pkg/tlsprofile`**: `EffectiveSpec`, `CertManagerWebhookTLSArgs`, `CertManagerOperandMetricsTLSArgs`, `CertManagerCipherSuiteArgKeys`, `ClientTLSConfig`.

## OpenShift Integrations

| Integration | Mechanism |
|-------------|-----------|
| Proxy | `withProxyEnv` + CSV `proxy-aware: true`; see `../../../docs/proxy.md` |
| Trusted CA | `--trusted-ca-configmap` mounts admin-created CM at **`/etc/pki/tls/certs/cert-manager-tls-ca-bundle.crt`** (`subPath: ca-bundle.crt`). Missing CM → library-go sets **Degraded=True and retries** (not a permanent fail). TrustManager separately watches CNO CM `cert-manager-operator-trusted-ca-bundle` — **not** the CertManager flag. |
| TLS profile | Registered when **Infrastructure** informer is `Applicable()` (APIServer shares that factory; not separately discovered). Applies only when `APIServer.spec.tlsAdherence` is `StrictAllComponents` (unknown values are treated as strict). Nil profile → **Intermediate**. TLS 1.3 → strip cipher args via `StripArgsByKeys(..., CertManagerCipherSuiteArgKeys)`. Hook **before** `withUnsupportedArgsOverrideHook`. Missing `APIServer/cluster` object → Degraded+retry (not silent). Operator metrics on `:8443` are applied at process start; a later profile or adherence change restarts the operator container. |
| Cloud credentials | Mount **existing** Secret into **controller** Deployment only — **never** create CredentialsRequest. AWS: `/.aws` + `AWS_SDK_LOAD_CONFIG=1`. GCP: `service_account.json` → `/.config/gcloud/application_default_credentials.json`. Other platforms → hard error. Missing secret → **Degraded=True + retry** (library-go). See `../../../docs/cloud_credentials.md`. |
| Optional APIs | Discover Infrastructure first (`InitInformerIfAvailable` / `Applicable()`). NotFound ≠ error; skip cloud-cred + TLS hooks when absent. |
| Monitoring | CSV `operatorframework.io/cluster-monitoring: "true"`; operand Service labels in bindata; no operator-owned ServiceMonitor. See `../../../docs/operand_metrics.md`. |
| FIPS | `hack/go-fips.sh` WARN branch = **local-only**. `go.mod` replace → `openshift/jetstack-cert-manager` should stay lockstep with `CERT_MANAGER_VERSION`. Don’t retarget upstream or silence WARN. Flip CSV `fips-compliant` only with a real guarantee change. See `fips-guidelines.md`. |
| OLM | `replaces` / `skipRange`; uninstall requires manual operand cleanup; CSV `tls-profiles: "true"`. |

Detail playbooks: `{integration,security,fips,operator-controllers}-guidelines.md` in harness-docs.

## Cache Constraints (`setup_manager.go`)

- One `labels.Selector` per GVK — **no OR across different label keys**.
- ConfigMaps: never label-filter the cache (TrustManager needs unlabeled CA bundle; IstioCSR needs `watched-by` key). Filter in **predicates**.
- Issuer / ClusterIssuer: never managed-label cache filter (user Issuers aren’t labeled).
- Shared filterable GVKs (e.g. Deployment): merge values into one `labelKey In (v1, v2)`.
- Heuristic: if any controller must watch unlabeled instances → leave type off managed lists; use predicates.

## Data Flow (summary)

```text
CertManager/cluster ──library-go──► Deployments in cert-manager NS
                                      └─► cert-manager reconciles Certificate/Issuer/...

IstioCSR/default ──ctrl-runtime Create/Update──► istio-csr Deployment (+ certs/RBAC/...)

TrustManager/cluster ──ctrl-runtime SSA──► trust-manager Deployment (+ webhook/certs/...)
                                            └─► trust-manager reconciles Bundle
```

## Anti-Patterns Observed in Code

1. Assuming uniform apply method across packages.  
2. Hand-editing bindata / generated clients.  
3. Treating `CertManagerReconciler` as a real controller.  
4. Expecting automatic operand garbage collection on IstioCSR/TrustManager delete (TODO).  
5. Setting TLS 1.3 cipher-suite args.  
6. Shipping non-FIPS builds to CI/production (`go-fips.sh` warning).

## SME Review Recommended

- Exact uninstall / finalizer cleanup design intent for IstioCSR & TrustManager.  
- Whether IstioCSR will be migrated to SSA.  
- Operational gotchas not visible in code (Slack/Jira) — Phase 5.5 chai-bot skipped.  
- Recipe nuances for adding a fourth operand controller (CSV env, RBAC, relatedImages checklist).

**Last verified against**: repo tree on branch `master` (operand versions v1.20.3 / istio-csr v0.16.0 / trust-manager v0.20.3).
