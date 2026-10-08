# Operator Controllers Guidelines

Contributor guide for `pkg/controller/{certmanager,istiocsr,trustmanager,common}`. This
repo runs **two controller frameworks side by side** — read this before adding or
modifying reconciliation logic. See `architecture/components.md` and
[`docs/adr/0001-dual-controller-frameworks.md`](../../docs/adr/0001-dual-controller-frameworks.md) / [`0002-apply-strategies.md`](../../docs/adr/0002-apply-strategies.md)
for the full rationale; this doc is the actionable "how to work in this code" summary.

## 1. Dual Framework — know which one you're in

| | `pkg/controller/certmanager` | `pkg/controller/istiocsr`, `pkg/controller/trustmanager` |
|---|---|---|
| Framework | OpenShift **library-go** (`staticresourcecontroller`, `deploymentcontroller`, `factory.Controller`) | **controller-runtime** (`sigs.k8s.io/controller-runtime`) |
| Wired from | `pkg/controller/certmanager/cert_manager_controller_set.go` + `pkg/operator/starter.go` | `pkg/operator/setup_manager.go` (`NewControllerManager`) |
| Started | Always, unconditionally | Only if feature gate enabled (see §9) |
| Status type | `operatorv1.OperatorStatus` (via `OperatorClient`) | Custom `ConditionalStatus` |
| CR scope | Cluster-scoped `cluster` | IstioCSR: namespaced `default`; TrustManager: cluster-scoped `cluster` |

**Rule**: Never assume the two stacks share helpers, lifecycle, or status shape. Code
written against one is not portable to the other without adaptation — this has been a
recurring source of incorrect PRs and hallucinated docs (ADR-0001).

## 2. Per-Controller Apply Methods — do not assume uniformity

Each package uses a **different** resource-apply idiom. Match the existing idiom in the
package you're editing; do not "fix" one to look like another without an ADR update.

| Package | Apply method | Evidence |
|---|---|---|
| `certmanager` | library-go `resourceapply.*` / `staticresourcecontroller` / `DeploymentController` | `cert_manager_networkpolicy.go` (`resourceapply.ApplyNetworkPolicy`) |
| `istiocsr` | Imperative **Create, then `UpdateWithRetry`** on diff | `services.go`, `deployments.go`: `r.Create(...)` / `r.UpdateWithRetry(...)` |
| `trustmanager` | **Server-Side Apply (SSA)**: `client.Patch(obj, client.Apply, client.FieldOwner(fieldOwner), client.ForceOwnership)` | `services.go`, `deployments.go`; field owner `trust-manager-controller` in `constants.go` |

Do **not** claim "we use SSA everywhere" — this is a documented hallucination failure
mode (ADR-0002). IstioCSR remains Create+Update until explicitly migrated; there is no
committed timeline for that migration.

## 3. Greenfield Rule: New ctrl-runtime Operand → Copy TrustManager

When adding a **new** feature-gated operand controller under `pkg/controller/`:

1. **Copy the TrustManager package structure**, not IstioCSR:
   `controller.go` (Reconcile/SetupWithManager), `deployments.go`, `services.go`,
   `serviceaccounts.go`, `rbacs.go`, `webhooks.go`, `constants.go`, `install_*.go`, `utils.go`.
2. Apply resources via `client.Patch(desired, client.Apply, client.FieldOwner(<name>-controller), client.ForceOwnership)`.
   Pick a unique `fieldOwner` constant per controller.
3. Diff only fields you manage via SSA (see `serviceModified` pattern in
   `trustmanager/services.go`) — do not `reflect.DeepEqual` the whole object.
4. Use `common.FromClientError` / `HandleReconcileResult` for error → status translation
   (§5), and `r.UpdateWithRetry` for status/finalizer updates on the CR itself (SSA does
   not apply to the CR's own status subresource pattern used here).
5. Register the controller in `pkg/operator/setup_manager.go`
   (`setupTrustManagerController`-style function) and wire a feature gate (§9).

IstioCSR is a **reference for Reconcile-loop shape and finalizer plumbing only** —
not for apply method.

## 4. Cache Constraints (`pkg/operator/setup_manager.go`)

The unified manager builds **one shared cache** across all enabled controllers via
`newUnifiedCacheBuilder` / `buildCacheObjectList`. Hard constraints:

- A `cache.ByObject` label selector is **one `labels.Selector` per GVK**. Kubernetes
  selectors AND requirements across different keys — there is no way to express
  `app in (...) OR watched-by exists` in one selector.
- **ConfigMaps must not use a label-filtered cache.** TrustManager needs its managed
  ConfigMaps (labeled) *and* the unlabeled `cert-manager-operator-trusted-ca-bundle`
  ConfigMap; IstioCSR needs its managed ConfigMaps *and* user ConfigMaps carrying a
  different label key (`istiocsr.openshift.operator.io/watched-by`). ConfigMaps use the
  default unfiltered informer; each controller applies **predicate-level filtering**
  instead.
- **Issuer/ClusterIssuer must not use a managed-label cache filter.** IstioCSR reconciles
  user-created Issuers referenced from its spec, which are never labeled by the operator.
- When two controllers share a GVK that *is* safely label-filterable (e.g.
  `appsv1.Deployment`), `addControllerCacheConfig` merges values into a single
  `labelKey In (value1, value2)` requirement — do not add a second, conflicting selector
  for the same type.

If you add a new watched type, first ask: "could any controller need to watch instances
of this type that lack the managed-resource label?" If yes, leave it off the managed
lists (`istioCSRManagedResources` / `trustManagerManagedResources`) and filter in a
predicate instead of a cache selector.

## 5. Errors & Status (ctrl-runtime path)

`pkg/controller/common/errors.go` defines `ReconcileError` with three reasons:

- `IrrecoverableError` — no requeue, sets Degraded (via `HandleReconcileResult`).
- `RetryRequiredError` — requeues with backoff.
- `MultipleInstanceError` — singleton-CR violation.

`FromClientError` classifies apiserver errors: `Unauthorized` / `Forbidden` / `Invalid` /
`BadRequest` / `ServiceUnavailable` → irrecoverable; everything else → retry-required.
Always wrap client errors through `common.FromClientError(err, "message %s", args...)`
rather than returning raw errors from Reconcile.

## 6. Finalizers — Add/Remove Implemented, Cleanup is TODO

Both IstioCSR and TrustManager **do** add/remove their own CR finalizer correctly
(`addFinalizer` / `removeFinalizer` in each package's `utils.go`, using
`controllerutil.AddFinalizer` + `r.UpdateWithRetry`). What is **not** implemented:

```go
// pkg/controller/{istiocsr,trustmanager}/controller.go — cleanUp()
// TODO: For GA, handle cleaning up of resources created for installing the operand.
// As per Non-Goals in the enhancement, removing the CR does NOT remove the
// Deployment or its associated resources.
r.eventRecorder.Eventf(cr, corev1.EventTypeWarning, "RemoveDeployment",
    "... remove all resources created for deployment manually")
return false, nil // never blocks finalizer removal
```

**Do not** assume deleting an IstioCSR/TrustManager CR garbage-collects the operand
Deployment, RBAC, Services, etc. — it does not, by design (Non-Goal in the enhancement),
and `cleanUp` is warn-only. If you're asked to implement real cleanup, this is GA-scoped
work requiring SME review (owner references vs. explicit delete vs. a validating webhook
for IstioCSR's GRPC-endpoint-in-use case — see the comment in `istiocsr/controller.go`).

## 7. `certmanager_controller.go` is a Placeholder — Do Not Extend

```go
// TODO: This is just a placeholder controller to contain all the required rbac
// in a single place. Needs to be deleted later.
type CertManagerReconciler struct{ ... }
func (r *CertManagerReconciler) Reconcile(...) (ctrl.Result, error) {
    return ctrl.Result{}, nil // no-op
}
```

`CertManagerReconciler` in `pkg/controller/certmanager/certmanager_controller.go` exists
**only** to host `+kubebuilder:rbac` markers for manifest generation; its `Reconcile` is a
no-op and it is never registered with any manager in `starter.go`. Real CertManager
reconciliation lives in the library-go controller set
(`cert_manager_controller_set.go` → `default_cert_manager_controller.go`,
`cert_manager_*_deployment.go`, `cert_manager_networkpolicy.go`). **Never** point new
CertManager logic at this file or assume it runs.

## 8. Shared `pkg/controller/common` Utilities — Reuse, Don't Reimplement

- **Client**: `CtrlClient`, `NewClient`, `UpdateWithRetry` (CR status/finalizer updates
  with conflict retry).
- **Errors**: `ReconcileError`, `NewIrrecoverableError`/`NewRetryRequiredError`/
  `NewMultipleInstanceError`, `FromClientError`, `FromError`, `HandleReconcileResult`.
- **Constants**: `ManagedResourceLabelKey` (`app`), `OperatorNamespace`,
  `TrustedCABundleConfigMapName`, `TrustedCABundleKey`.
- **Args**: `MergeContainerArgs`, `ParseArgMap`, `StripArgsByKeys`, `ArgKeysSet`.
- **TLS**: `WithClusterTLSProfileFromAPIServer` (applies APIServer TLS profile as operand
  args; do not set explicit cipher-suite args for TLS 1.3).
- **Metadata**: `UpdateName`/`UpdateNamespace`/`UpdateResourceLabels`,
  `ObjectMetadataModified` — use these when decoding bindata assets into objects
  (`common.DecodeObjBytes[*T](codecs, gv, assets.MustAsset(name))`) instead of hand-rolling
  metadata mutation.
- **Validation**: `pkg/controller/common/validation.go`, `core_validation_helpers.go`.

Before writing a new helper for arg merging, label diffing, or client retry, check
`pkg/controller/common` first — both IstioCSR and TrustManager already depend on it, and
duplicating logic per-package is an anti-pattern.

## 9. Feature-Gate Wiring — the full chain

1. Define in `api/operator/v1alpha1/features.go` (`IstioCSR` GA/default-true,
   `TrustManager` TechPreview/default-false).
2. Register in `pkg/features` (`SetupWithFlagValue`, `NewFeatureGateState`).
3. `pkg/operator/starter.go`: `setupFeatureGates` parses `--unsupported-addon-features`
   and (best-effort, fail-open with retries) reads `featuregates/cluster`; never aborts
   startup on persistent failure.
4. Runtime checks are **internal-only** — `features.IsIstioCSRFeatureGateEnabled()` and
   `(*FeatureGateState).IsTrustManagerFeatureGateEnabled()`. `passesClusterPreviewGating`
   in `features.go` still exists but is **unused** by the TrustManager enable path
   (cluster `FeatureSet` gating was removed, CM-1141) — don't resurrect it without
   confirming intent.
5. `starter.go` only calls `NewControllerManager` (§1) if **either** gate is enabled; each
   enabled gate adds its resource list to the shared cache (§4) and its reconciler to the
   manager via `setup{IstioCSR,TrustManager}Controller` in `setup_manager.go`.

Adding a fourth gated operand means touching all five steps above, plus the greenfield
checklist in §3.

## 10. Quick Checklist Before Sending a PR

- [ ] Confirmed which framework (§1) and which apply idiom (§2) the touched package uses.
- [ ] New ctrl-runtime operand copies TrustManager SSA, not IstioCSR Create+Update (§3).
- [ ] No new label-selector cache filter on ConfigMaps or Issuer/ClusterIssuer (§4).
- [ ] Errors routed through `common.FromClientError`/`ReconcileError` (§5).
- [ ] Did not assume finalizer removal cleans up operand resources (§6).
- [ ] Did not add logic to `certmanager_controller.go` (§7).
- [ ] Reused `pkg/controller/common` helpers instead of duplicating (§8).
- [ ] Feature-gate changes updated all five wiring points (§9).
