# Error Handling Guidelines

Scope: the controller-runtime (ctrl-runtime) stack — **IstioCSR** and **TrustManager**. CertManager
uses library-go and follows a different model (see [§7](#7-do-not-mix-library-go-and-ctrl-runtime-patterns)).
Core types live in `pkg/controller/common/errors.go` and `reconcile_result.go`.

## 1. `ReconcileError` — the only error type reconcile logic should return

```go
type ReconcileError struct {
    Reason  ErrorReason // IrrecoverableError | RetryRequiredError | MultipleInstanceError
    Message string
    Err     error
}
```

Business-logic reconcile functions (deployment/service/RBAC/certificate reconciliation, config
validation) should return a `*ReconcileError`, not a bare `error`. Construct one via a helper —
avoid building the struct literal directly:

| Helper | When to use |
|---|---|
| `NewIrrecoverableError(err, msg, args...)` | Config/spec is invalid, or the error can never be fixed by retrying (bad issuer kind, malformed PEM, missing required key). |
| `NewRetryRequiredError(err, msg, args...)` | Transient condition that a later reconcile could resolve. |
| `NewMultipleInstanceError(err)` | Singleton-CR invariant violated (see [§5](#5-multipleinstanceerror-is-not-a-normal-error)). |
| `FromClientError(err, msg, args...)` | Any error returned from a Kubernetes API call (Get/Create/Update/Patch/List). |
| `FromError(err, msg, args...)` | Rewrapping an error that **may already be** a classified `ReconcileError` (e.g. bubbling a helper's result up with extra context) — only `IrrecoverableError` is special-cased and preserved; any other input reason (including an existing `RetryRequiredError` or `MultipleInstanceError`) collapses to `RetryRequiredError`, since the fallback branch doesn't distinguish them. |

All constructors return `nil` for a `nil` input error, so `return NewIrrecoverableError(err, "...")`
is safe to use directly after an `if err != nil` check without an extra nil guard.

Check classification with `IsIrrecoverableError`, `IsRetryRequiredError`, `IsMultipleInstanceError`
(all use `errors.As`, so they work through wrapping) — never compare `.Reason` directly outside
`pkg/controller/common`.

## 2. `FromClientError` mapping (`errors.go`)

```go
apierrors.IsUnauthorized / IsForbidden / IsInvalid / IsBadRequest / IsServiceUnavailable
    → IrrecoverableError
everything else (NotFound, Conflict, AlreadyExists, timeouts, ...)
    → RetryRequiredError
```

Rule of thumb: permission/validation-shaped API errors are treated as unfixable by retrying;
existence/contention-shaped errors are treated as transient. Do not special-case `NotFound` or
`Conflict` yourself to invent a different severity than `FromClientError` would assign — route the
raw client error through it and let it classify.

`CtrlClient` methods (`client.go`) wrap errors with `fmt.Errorf("...: %w", err)` before returning
them; this preserves `apierrors` unwrapping, so `FromClientError` still classifies correctly even
though the error text no longer looks like a raw API error.

## 3. `HandleReconcileResult` — requeue vs. degrade

`HandleReconcileResult(status, reconcileErr, log, updateConditionFn, requeueDuration)` is the
**single point** that turns a `ReconcileError` into a `ctrl.Result` + condition update. Call it once,
at the end of `processReconcileRequest`, with the aggregate error from your reconcile chain — do not
call `SetCondition` ad hoc elsewhere in the "happy path" of a reconcile.

| `reconcileErr` | `Degraded` | `Ready` | `ctrl.Result` |
|---|---|---|---|
| `nil` (success) | `False` / `Ready` | `True` / `Ready` | `{}` (no requeue; next event-driven trigger) |
| `IsRetryRequiredError` | `False` / `Ready` | `False` / `Progressing` | `{RequeueAfter: requeueDuration}` |
| `IsIrrecoverableError` | `True` / `Failed` | `False` / `Failed` | `{}` (**no requeue** — waits for spec/resource change to re-trigger) |

Both controllers pass `defaultRequeueTime = 30s` for `requeueDuration` — keep new ctrl-runtime
operands consistent with this unless there's a documented reason to diverge.

If `updateConditionFn` itself fails, that error is returned instead of requeuing/succeeding (status
write failure always wins). This means an irrecoverable error whose status update also fails **will**
eventually get retried by ctrl-runtime's default error backoff, even though the intent was "no
requeue" — this is a known, accepted side effect, not a bug to route around.

## 4. What does *not* go through `ReconcileError`/`HandleReconcileResult`

Framework-level plumbing in `Reconcile()` — fetching the CR, finalizer add/remove, deletion
clean-up — is returned as a plain (often `fmt.Errorf`-wrapped) `error` straight to ctrl-runtime,
*before* `processReconcileRequest` is called. Only the actual reconcile-the-operand logic is routed
through `ReconcileError` + `HandleReconcileResult`. When adding a new operand, follow this same
split rather than routing every error through `HandleReconcileResult`.

`NotFound` on the initial `Get` of the CR is treated as "nothing to do" (`ctrl.Result{}, nil`), not
an error at all — the CR was deleted between enqueue and reconcile.

## 5. `MultipleInstanceError` is not a normal error

Only IstioCSR currently implements this. It's a *namespaced* singleton (CRD enforces name `default`
per namespace) — CEL can validate the name within one namespace but can't see sibling namespaces, so
`istiocsr/utils.go: disallowMultipleIstioCSRInstances` does the cross-namespace check at the
controller level:

1. Set `Ready=False/Failed` on the *rejected* instance directly via `status.SetCondition` +
   a manual `updateCondition` call (not via `HandleReconcileResult`).
2. Return `common.NewMultipleInstanceError(...)`.
3. In `processReconcileRequest`, check `common.IsMultipleInstanceError(err)` **before** calling
   `HandleReconcileResult`, emit a `Warning` event, and swallow the error (`err = nil`) so ctrl-runtime
   does not requeue a permanently-rejected instance:

```go
if err := r.disallowMultipleIstioCSRInstances(istiocsr); err != nil {
    if common.IsMultipleInstanceError(err) {
        r.eventRecorder.Eventf(istiocsr, corev1.EventTypeWarning, "MultiIstioCSRInstance", "...")
        err = nil
    }
    return ctrl.Result{}, err
}
```

TrustManager has no equivalent guard: it's a *cluster-scoped* singleton named `cluster`, and
Kubernetes' own object-name uniqueness combined with the CEL name-lock (see
[api-contracts-guidelines.md §4](api-contracts-guidelines.md)) already rules out a second instance,
so there's nothing for a controller-side check to catch.

Any new **namespaced** singleton (where CEL can't see sibling namespaces) should reuse this reason
and this "warn + swallow" handling rather than treating it as `IrrecoverableError` (it isn't a config
problem with *this* instance, it's a cluster-state problem). A new cluster-scoped singleton with a
CEL-locked name likely doesn't need this at all.

## 6. Status condition updates — things that will surprise you

- `ConditionalStatus.SetCondition(type, status, reason, msg)` returns `true` only if `Status` or
  `Reason` changed (or the condition didn't exist yet) — **a message-only change to an existing
  condition does not count as a change** and will not trigger a write. Don't rely on message text
  alone to force a status update.
- `HandleReconcileResult` only calls `updateConditionFn` when at least one of `Degraded`/`Ready`
  actually changed (`degradedChanged || readyChanged`) — avoid adding side effects inside
  `updateConditionFn` that assume it runs every reconcile.
- TrustManager's `updateConditionFn` closure (`updateCondition` in `trustmanager/utils.go`) prepends
  the *reconcile* error to any *status-update* error via
  `utilerrors.NewAggregate([]error{prependErr, errUpdate})`, so both are visible in logs/events even
  though only one `ctrl.Result` decision is made. Treat this as the reference pattern for a new
  `updateConditionFn`. IstioCSR's own `updateCondition` (`istiocsr/utils.go`) does **not** follow it —
  on a status-update failure it aggregates the raw and re-wrapped update errors together and drops
  `prependErr` entirely — so don't assume every `updateConditionFn` in this repo actually preserves
  the original reconcile error; check the specific controller's implementation before relying on it.
- Set both `Degraded` and `Ready` together before the single `updateConditionFn` call ("atomically"),
  as `HandleReconcileResult` does — don't split them across two separate status writes.

## 7. Do not mix library-go and ctrl-runtime patterns

| | CertManager (library-go) | IstioCSR / TrustManager (ctrl-runtime) |
|---|---|---|
| Error type returned from `sync`/`Reconcile` | plain `error` (`fmt.Errorf("...: %w", err)`) | `*common.ReconcileError` (business logic) / plain `error` (framework plumbing) |
| Who sets Degraded | `factory.Controller` automatically, from any non-nil `sync` error | Reconciler explicitly, via `HandleReconcileResult` |
| Retry-vs-permanent distinction | **None** — any error is retried with the factory's rate limiter | Explicit: `IrrecoverableError` never requeues, `RetryRequiredError` requeues after `requeueDuration` |
| Status model | `operatorv1.OperatorStatus` (`v1helpers`) | `v1alpha1.ConditionalStatus` (`common`/API package) |

Because library-go has no "irrecoverable, stop retrying" concept, do **not** import the
`ReconcileError`/`HandleReconcileResult` machinery from `pkg/controller/common` into
`pkg/controller/certmanager`, and do not assume a CertManager sync error will ever stop being
retried — that only holds for the ctrl-runtime path. (Non-error `pkg/controller/common` helpers like
`MergeContainerArgs`, `ParseArgMap`, and `WithClusterTLSProfileFromAPIServer` are shared with
`certmanager` today and are fine to use — the restriction is specifically about the error-
classification types.) Conversely, do not return a bare `error` from ctrl-runtime reconcile
*business logic* just because that's what CertManager does — wrap it so `HandleReconcileResult` can
classify it.

## 8. Adding a new ctrl-runtime operand

Per [ADR-0002](../../docs/adr/0002-apply-strategies.md) and the repo's greenfield rule,
copy **TrustManager**, not IstioCSR, for both apply strategy and error handling — IstioCSR's
Create+Update path and its `updateCondition` aggregation quirk (§6) are legacy, not the target
pattern. At minimum: wrap every client call in `FromClientError`, classify programmatic/validation
failures with `NewIrrecoverableError`, route the top-level reconcile error through
`HandleReconcileResult` with a `30s`-class `requeueDuration`, and keep CR-fetch/finalizer errors
outside that flow.
