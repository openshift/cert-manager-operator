# PR #459 Deep Dive: HTTP01 Proxy with nftables MachineConfig

**PR**: [CM-716: HTTP01 Proxy with nftables MachineConfig](https://github.com/openshift/cert-manager-operator/pull/459)
**Author**: Brandon Palm (`sebrandon1`)
**Branch**: `experiment/dnat-snat-operator`
**Commit**: `5ea08400f`
**Jira**: [CM-716](https://issues.redhat.com/browse/CM-716)
**Enhancement**: [openshift/enhancements#1929](https://github.com/openshift/enhancements/pull/1929)

---

## Table of Contents

1. [The Networking Problem](#1-the-networking-problem)
2. [The API Layer](#2-the-api-layer)
3. [The Controller](#3-the-controller)
4. [Platform Discovery and Validation](#4-platform-discovery-and-validation)
5. [MachineConfig Rendering](#5-machineconfig-rendering)
6. [Finalizer Lifecycle](#6-finalizer-lifecycle)
7. [Status Condition Management](#7-status-condition-management)
8. [Wiring: Feature Gate and Startup](#8-wiring-feature-gate-and-startup)
9. [Generated Kubernetes Client Infrastructure](#9-generated-kubernetes-client-infrastructure)
10. [Config, RBAC, and Bundle Changes](#10-config-rbac-and-bundle-changes)
11. [Test Suite](#11-test-suite)
12. [End-to-End Data Flow](#12-end-to-end-data-flow)
13. [Key Design Decisions](#13-key-design-decisions)
14. [Files Changed Summary](#14-files-changed-summary)

---

## 1. The Networking Problem

### 1.1 What Are VIPs?

On a baremetal OpenShift cluster, there's no cloud load balancer. Instead, **keepalived** (implementing the VRRP protocol) manages **Virtual IPs** — IP addresses that "float" between physical nodes. If the node holding the VIP goes down, another node takes over within seconds.

There are two VIPs:

- **API VIP** (e.g., `10.46.97.32`): DNS for `api.cluster.example.com` points here. The kube-apiserver listens on port 6443. **Port 80 is unused.**
- **Ingress VIP** (e.g., `10.46.97.48`): DNS for `*.apps.cluster.example.com` points here. The OpenShift router (HAProxy) listens on ports 80 and 443.

### 1.2 The HTTP-01 Challenge Flow

When cert-manager requests a certificate from Let's Encrypt using HTTP-01:

1. cert-manager creates a **solver pod** that serves a token at `/.well-known/acme-challenge/<TOKEN>` on port 8089
2. cert-manager creates a **Service** and **Ingress** (or Route) pointing to the solver pod
3. The ACME server (Let's Encrypt) tries `http://<DOMAIN>:80/.well-known/acme-challenge/<TOKEN>`

If the domain is `api.cluster.example.com`, DNS resolves to the **API VIP**. But the solver pod is behind the **Ingress VIP**. The request hits the API VIP on port 80 and gets nothing — connection refused. The challenge fails.

### 1.3 DNAT and MASQUERADE

**DNAT (Destination NAT)**: The kernel rewrites the destination IP of incoming packets. A packet arriving at `10.46.97.32:80` gets its destination changed to `10.46.97.48:80` *before* the kernel makes a routing decision.

**MASQUERADE**: When the kernel forwards this DNAT'd packet to the Ingress VIP, the source IP is the external ACME server. The ingress node would try to send the response directly back to the ACME server — but the ACME server expects a response from the API VIP, not the Ingress VIP. **MASQUERADE** rewrites the source IP of the forwarded packet to the node's own IP, so the response comes back through the same node and the DNAT is "reversed" automatically by the kernel's connection tracking (conntrack) table.

```
ACME server --> API VIP:80 --> [nftables DNAT] --> Ingress VIP:80 --> router --> solver pod
                                                   [MASQUERADE ensures return traffic routes back]
```

### 1.4 Why Both nftables AND iptables?

OpenShift uses **iptables-nft** — iptables syntax running on top of the nftables kernel subsystem. The kernel maintains two parallel hook chains:

```
Packet arrives
  └─> nftables native hooks (our DNAT rules) ─> packet gets DNAT'd ✓
  └─> iptables-nft hooks (OpenShift's FORWARD chain, policy DROP) ─> packet dropped ✗
```

Both must ACCEPT the packet. So the PR adds:

1. **nftables rules** for DNAT + MASQUERADE (the NAT itself)
2. **iptables FORWARD rules** to ACCEPT the forwarded packets (so OpenShift's DROP policy doesn't kill them)

---

## 2. The API Layer

### 2.1 Feature Gate Definition

File: `api/operator/v1alpha1/features.go`

```go
FeatureHTTP01Proxy featuregate.Feature = "HTTP01Proxy"

var OperatorFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{
    FeatureIstioCSR:     {Default: true, PreRelease: featuregate.GA},
    FeatureTrustManager: {Default: false, PreRelease: "TechPreview"},
    FeatureHTTP01Proxy:  {Default: false, PreRelease: featuregate.Alpha},
}
```

Alpha, disabled by default. Must be explicitly enabled with `--unsupported-addon-features="HTTP01Proxy=true"`.

### 2.2 The Type Hierarchy

File: `api/operator/v1alpha1/http01proxy_types.go`

```
HTTP01Proxy
├── TypeMeta (apiVersion, kind)
├── ObjectMeta (name, namespace, labels, annotations, finalizers, deletionTimestamp...)
├── Spec: HTTP01ProxySpec
│   ├── Mode: "DefaultDeployment" | "CustomDeployment"
│   └── CustomDeployment: *HTTP01ProxyCustomDeploymentSpec (optional)
│       └── InternalPort: int32 (1024-65535, default 8888)
└── Status: HTTP01ProxyStatus
    ├── ConditionalStatus (embedded)
    │   └── Conditions: []metav1.Condition
    │       ├── {Type: "Degraded", Status: True/False, Reason: "Failed"/"Ready"}
    │       └── {Type: "Ready", Status: True/False, Reason: "Ready"/"Failed"/"Progressing"}
    └── ProxyImage: string
```

### 2.3 CEL Validation Rules

**Singleton enforcement** (on the object itself):

```go
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default'",message="http01proxy is a singleton, .metadata.name must be 'default'"
```

If you try `kubectl create` with `name: foo`, the API server rejects it with a 422 Unprocessable Entity. The controller never sees the request.

**Conditional field enforcement** (on the spec):

```go
// +kubebuilder:validation:XValidation:rule="self.mode == 'CustomDeployment' ? has(self.customDeployment) : !has(self.customDeployment)",message="customDeployment is required when mode is CustomDeployment and forbidden otherwise"
```

If mode is `CustomDeployment`, the `customDeployment` field must be present; otherwise, it must be absent.

### 2.4 Additional Printer Columns

```go
// +kubebuilder:printcolumn:name="Mode",type="string",JSONPath=".spec.mode"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].message"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
```

When you run `kubectl get http01proxy`:

```
NAME      MODE                READY   MESSAGE                    AGE
default   DefaultDeployment   True    reconciliation successful  5m
```

### 2.5 The ConditionalStatus Pattern

File: `api/operator/v1alpha1/meta.go`

```go
type ConditionalStatus struct {
    // +patchMergeKey=type
    // +patchStrategy=merge
    // +listType=map
    // +listMapKey=type
    Conditions []metav1.Condition `json:"conditions,omitempty"`
}
```

- `+listType=map` and `+listMapKey=type` tell Kubernetes this is a **map-keyed list** — can't have two conditions with the same `type`, and strategic merge patches work on the `type` field as the key.
- Shared across IstioCSR, TrustManager, and HTTP01Proxy.

The `SetCondition` method (in `conditions.go`) only returns `true` if the status or reason actually changed — preventing unnecessary API writes on no-op reconciliations.

---

## 3. The Controller

### 3.1 The Reconciler Struct

File: `pkg/controller/http01proxy/controller.go`

```go
type Reconciler struct {
    common.CtrlClient       // embedded client with retry helpers
    eventRecorder record.EventRecorder
    log           logr.Logger
    cachedPlatform *platformInfo   // cached platform discovery
    platformMu     sync.Mutex     // thread-safe cache access
}
```

- `common.CtrlClient`: Project-wide abstraction wrapping `client.Client` with `UpdateWithRetry` and `StatusUpdate`. Also enables testing via `fakes.FakeCtrlClient`.
- `cachedPlatform` + `platformMu`: Multiple reconciliation goroutines could call `getOrDiscoverPlatform` concurrently, so mutex protection is required.

### 3.2 Controller Registration (SetupWithManager)

```go
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
    // When Infrastructure CR changes, invalidate cache and enqueue reconcile
    infrastructureMapFunc := func(ctx context.Context, obj client.Object) []reconcile.Request {
        if obj.GetName() != "cluster" { return nil }
        r.platformMu.Lock()
        r.cachedPlatform = nil
        r.platformMu.Unlock()
        return []reconcile.Request{{NamespacedName: types.NamespacedName{
            Name: "default", Namespace: common.OperatorNamespace,
        }}}
    }

    builder := ctrl.NewControllerManagedBy(mgr).
        For(&v1alpha1.HTTP01Proxy{}).
        Named(ControllerName)

    // Runtime capability check — skip watch on MicroShift
    if _, err := mgr.GetRESTMapper().RESTMapping(infrastructureGVK.GroupKind(), infrastructureGVK.Version); err == nil {
        builder = builder.Watches(&configv1.Infrastructure{}, handler.EnqueueRequestsFromMapFunc(infrastructureMapFunc))
    }

    return builder.Complete(r)
}
```

Key points:
- `For(&v1alpha1.HTTP01Proxy{})` — primary watch: any create/update/delete of HTTP01Proxy triggers reconciliation.
- `Watches(&configv1.Infrastructure{}, ...)` — secondary watch: Infrastructure changes invalidate the platform cache and trigger re-reconciliation.
- The `RESTMapping` check prevents crashes on MicroShift where the Infrastructure CRD doesn't exist.

### 3.3 The Reconcile Method — Full Flow

```go
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
```

| Path | Condition | Action |
|------|-----------|--------|
| Wrong namespace | `req.Namespace != common.OperatorNamespace` | Silently ignore |
| Object not found | `errors.IsNotFound(err)` | Return empty result (already deleted) |
| Get error | Any other API error | Return error (controller-runtime retries with exponential backoff) |
| Deletion in progress | `!proxy.DeletionTimestamp.IsZero()` | Clean up MachineConfig → remove finalizer → return |
| Normal reconciliation | Object exists, not being deleted | Add finalizer → `processReconcileRequest` |

### 3.4 processReconcileRequest — Error Handling State Machine

```go
func (r *Reconciler) processReconcileRequest(...) (ctrl.Result, error) {
    reconcileErr := r.reconcileHTTP01ProxyDeployment(ctx, proxy)
    return common.HandleReconcileResult(
        &proxy.Status.ConditionalStatus,
        reconcileErr,
        r.log,
        func(prependErr error) error { return r.updateCondition(ctx, proxy, prependErr) },
        defaultRequeueTime, // 30 seconds
    )
}
```

`HandleReconcileResult` (in `pkg/controller/common/reconcile_result.go`) maps errors to status conditions:

| `reconcileErr` | Degraded | Ready | Reason | Return |
|----------------|----------|-------|--------|--------|
| `nil` (success) | False | True | Ready | No error, no requeue |
| `IrrecoverableError` | True | False | Failed | No error (**don't retry**) |
| `RetryRequiredError` | False | False | Progressing | `RequeueAfter: 30s` |

Unsupported platform → IrrecoverableError → won't keep retrying.
Transient API error → RetryRequiredError → tries again in 30 seconds.

### 3.5 reconcileHTTP01ProxyDeployment — The Core Logic

File: `pkg/controller/http01proxy/install_http01proxy.go`

```go
func (r *Reconciler) reconcileHTTP01ProxyDeployment(ctx context.Context, proxy *v1alpha1.HTTP01Proxy) error {
    // 1. Discover platform (cached, mutex-protected)
    info, err := r.getOrDiscoverPlatform(ctx)
    if err != nil {
        return common.NewRetryRequiredError(err, "failed to discover platform")
    }

    // 2. Validate platform
    if reason := validatePlatform(info); reason != "" {
        // Clean up any existing MachineConfig first (platform may have changed)
        if err := r.cleanUp(ctx, proxy); err != nil {
            return common.NewRetryRequiredError(err, "failed to clean up after platform validation failure")
        }
        return common.NewIrrecoverableError(fmt.Errorf("platform validation failed"), "%s", reason)
    }

    // 3. Create or update MachineConfig
    if err := r.createOrApplyMachineConfig(ctx, info); err != nil {
        return err
    }

    // 4. Mark as processed (for logging distinguishing new vs existing)
    if common.AddAnnotation(proxy, controllerProcessedAnnotation, "true") {
        if err := r.UpdateWithRetry(ctx, proxy); err != nil {
            return fmt.Errorf("failed to update processed annotation: %w", err)
        }
    }

    return nil
}
```

---

## 4. Platform Discovery and Validation

### 4.1 Cached Discovery

File: `pkg/controller/http01proxy/infrastructure.go`

```go
func (r *Reconciler) getOrDiscoverPlatform(ctx context.Context) (*platformInfo, error) {
    r.platformMu.Lock()
    defer r.platformMu.Unlock()
    if r.cachedPlatform != nil {
        return r.cachedPlatform, nil
    }
    info, err := r.discoverPlatform(ctx)
    if err != nil { return nil, err }
    r.cachedPlatform = info
    return info, nil
}
```

Cache is invalidated only when the Infrastructure CR changes (via the secondary watch in `SetupWithManager`).

### 4.2 Unstructured Objects

```go
func (r *Reconciler) discoverPlatform(ctx context.Context) (*platformInfo, error) {
    infra := &unstructured.Unstructured{}
    infra.SetGroupVersionKind(infrastructureGVK)
    // ...
    platformType, _, _ := unstructured.NestedString(infra.Object, "status", "platformStatus", "type")
}
```

Why `unstructured.Unstructured` instead of typed Go structs?

1. **No import dependency** on MachineConfig operator types — avoids `go.mod` coupling.
2. **Resilience** to API version changes — string-based nested field access works regardless of struct layout.
3. **`meta.IsNoMatchError(err)`** handles the case where the entire CRD doesn't exist on the API server (MicroShift).

### 4.3 Validation Logic

```go
func validatePlatform(info *platformInfo) string {
    // Must be BareMetal
    // Must have at least one API VIP
    // Must have at least one Ingress VIP
    // API VIP and Ingress VIP must not be the same
}
```

The overlapping VIP check is a **cross-product comparison** — checks every `(apiVIP, ingressVIP)` pair, not just the first. This handles dual-stack clusters with both IPv4 and IPv6 VIPs.

---

## 5. MachineConfig Rendering

### 5.1 What Is a MachineConfig?

An OpenShift-specific resource declaring OS-level configuration (files, systemd units) for nodes. The Machine Config Operator drains each node, applies the config, and reboots it.

### 5.2 The nftables Configuration

File: `pkg/controller/http01proxy/machineconfig.go`

```
table inet crtmgr_http01_dnat          ← create if not exists (inet = dual-stack)
delete table inet crtmgr_http01_dnat   ← idempotency: remove old rules
table inet crtmgr_http01_dnat {
    chain prerouting {
        type nat hook prerouting priority 0;
        ip daddr {{ .APIVIP }} tcp dport 80 dnat ip to {{ .IngressVIP }}:80
        ↑ match: dest=API VIP, port=80       ↑ action: rewrite dest to Ingress VIP
    }
    chain postrouting {
        type nat hook postrouting priority 100;
        ip daddr {{ .IngressVIP }} tcp dport 80 masquerade
        ↑ rewrite source IP so return traffic routes back through this node
    }
}
```

- The `ip` keyword in `dnat ip to` disambiguates IPv4 inside an `inet` table.
- The create-delete-create pattern ensures idempotent reloads.

### 5.3 The Systemd Service

```ini
[Unit]
Wants=network-pre.target
Before=network-pre.target       ← runs before networking is fully up

[Service]
Type=oneshot                    ← runs commands and exits
ProtectSystem=full              ← security: can't write to /usr, /boot
ProtectHome=true                ← security: can't access /home

ExecStartPre=/sbin/sysctl -w net.ipv4.ip_forward=1    ← enable IP forwarding
ExecStart=/sbin/nft -f /etc/sysconfig/nftables-crtmgr-http01.conf    ← load nftables
ExecStart=/bin/bash -c 'iptables -C FORWARD ... 2>/dev/null || iptables -I FORWARD 1 ...'
↑ check-then-insert pattern: add iptables FORWARD rule only if not present

ExecReload=/sbin/nft -f ...    ← allows systemctl reload

ExecStop=/sbin/nft 'add table ...; delete table ...'   ← atomic cleanup
ExecStop=/bin/bash -c 'iptables -D FORWARD ...; true'  ← remove FORWARD rule

RemainAfterExit=yes    ← crucial: keeps service "active" so ExecStop can fire
```

### 5.4 The createOrApplyMachineConfig Method

Three-way reconciliation:

1. **Pre-checks**: Both `apiVIPs` and `ingressVIPs` must be non-empty.
2. **Render desired state**: `renderMachineConfig(info.apiVIPs[0], info.ingressVIPs[0])`. Only first VIP used (IPv6 out of scope).
3. **Get existing state**: Fetch MachineConfig by name.
4. **Create if NotFound**.
5. **Compare specs**: `reflect.DeepEqual(desiredSpec, existingSpec)`. Skip if equal.
6. **Update if different**: Set existing `ResourceVersion` on desired object (required for optimistic concurrency).

MachineConfig name: `98-nftables-crtmgr-http01-dnat`. The `98` prefix is high priority (applied late), avoiding conflicts with lower-numbered system configs.

---

## 6. Finalizer Lifecycle

### 6.1 What Are Finalizers?

Strings stored in `metadata.finalizers[]`. When you delete a Kubernetes object with finalizers:

1. API server sets `metadata.deletionTimestamp`
2. Does NOT actually delete the object
3. Waits for all finalizers to be removed

The controller's finalizer: `"http01proxy.openshift.operator.io/cert-manager-http01-proxy-controller"`.

### 6.2 Add Finalizer

File: `pkg/controller/http01proxy/utils.go`

```go
func (r *Reconciler) addFinalizer(ctx context.Context, proxy *v1alpha1.HTTP01Proxy) error {
    if !controllerutil.ContainsFinalizer(proxy, finalizer) {
        controllerutil.AddFinalizer(proxy, finalizer)
        r.UpdateWithRetry(ctx, proxy)
        // Re-fetch to get updated ResourceVersion
        r.Get(ctx, namespacedName, updated)
        updated.DeepCopyInto(proxy)
    }
    return nil
}
```

The re-fetch after update is critical: `Update` changes the `ResourceVersion`, and the in-memory object becomes stale. Without the re-fetch, subsequent status updates would fail with a conflict error.

### 6.3 Remove Finalizer

```go
func (r *Reconciler) removeFinalizer(ctx context.Context, proxy *v1alpha1.HTTP01Proxy) error {
    if controllerutil.ContainsFinalizer(proxy, finalizer) {
        controllerutil.RemoveFinalizer(proxy, finalizer)
        r.UpdateWithRetry(ctx, proxy)
    }
    return nil
}
```

Once removed, Kubernetes garbage-collects the HTTP01Proxy object.

---

## 7. Status Condition Management

### 7.1 updateCondition with Error Aggregation

```go
func (r *Reconciler) updateCondition(ctx context.Context, proxy *v1alpha1.HTTP01Proxy, prependErr error) error {
    if err := r.updateStatus(ctx, proxy); err != nil {
        errUpdate := fmt.Errorf("failed to update status: %w", err)
        if prependErr != nil {
            return utilerrors.NewAggregate([]error{prependErr, errUpdate})
        }
        return errUpdate
    }
    return prependErr
}
```

Handles the case where both reconciliation AND status update fail — `NewAggregate` preserves both errors.

### 7.2 updateStatus with Retry

```go
func (r *Reconciler) updateStatus(ctx context.Context, changed *v1alpha1.HTTP01Proxy) error {
    return retry.RetryOnConflict(retry.DefaultRetry, func() error {
        current := &v1alpha1.HTTP01Proxy{}
        r.Get(ctx, namespacedName, current)          // 1. Read latest
        changed.Status.DeepCopyInto(&current.Status) // 2. Copy desired status
        r.StatusUpdate(ctx, current)                  // 3. Write back
    })
}
```

Read-modify-write with retry. The status subresource (`/status`) is separate from the main resource — updating status doesn't trigger a new reconciliation (preventing infinite loops).

---

## 8. Wiring: Feature Gate and Startup

### 8.1 Startup Flow

File: `pkg/operator/starter.go`

```go
// 1. Parse feature flags
features.SetupWithFlagValue(UnsupportedAddonFeatures)

// 2. Check if HTTP01Proxy is enabled
http01ProxyEnabled := features.DefaultFeatureGate.Enabled(v1alpha1.FeatureHTTP01Proxy)

// 3. Create unified controller manager if any addon is enabled
if istioCSREnabled || trustManagerEnabled || http01ProxyEnabled {
    manager, _ := NewControllerManager(ControllerConfig{
        EnableHTTP01Proxy: http01ProxyEnabled,
    })
    go manager.Start(ctx)
}
```

### 8.2 Controller Manager Setup

File: `pkg/operator/setup_manager.go`

```go
func setupHTTP01ProxyController(mgr ctrl.Manager) error {
    r, _ := http01proxy.New(mgr)
    return r.SetupWithManager(mgr)
}
```

The HTTP01Proxy controller is registered with an unfiltered cache entry (`cache.ByObject{}`), unlike IstioCSR and TrustManager which use label-selector-filtered caches for their managed sub-resources.

---

## 9. Generated Kubernetes Client Infrastructure

~800 lines of boilerplate generated by Kubernetes code generators:

| Package | Purpose | Used By |
|---------|---------|---------|
| `pkg/operator/clientset/.../http01proxy.go` | Typed CRUD client (Create, Get, List, Watch, Update, Delete, Patch, Apply) | E2E tests, operator startup |
| `pkg/operator/clientset/.../fake/fake_http01proxy.go` | In-memory fake client | Unit tests |
| `pkg/operator/informers/.../http01proxy.go` | Shared informer (watches API server, maintains local cache) | Operator event-driven architecture |
| `pkg/operator/listers/.../http01proxy.go` | Read-only cache accessor (List, Get by namespace) | Informer-based code |
| `pkg/operator/applyconfigurations/.../http01proxy.go` | Builder pattern for server-side apply | Declarative patching |
| `api/operator/v1alpha1/zz_generated.deepcopy.go` | DeepCopy implementations | All Kubernetes object handling |

---

## 10. Config, RBAC, and Bundle Changes

### 10.1 CRD Manifest

File: `config/crd/bases/operator.openshift.io_http01proxies.yaml`

- 179 lines of OpenAPI v3 schema
- Includes CEL validation rules, printer columns, status subresource
- Labels: `app.kubernetes.io/name=http01proxy`, `app.kubernetes.io/part-of=cert-manager-operator`

### 10.2 RBAC

File: `config/rbac/role.yaml`

New permissions added:

```yaml
- apiGroups: [machineconfiguration.openshift.io]
  resources: [machineconfigs]
  verbs: [create, delete, get, list, patch, update, watch]

- apiGroups: [operator.openshift.io]
  resources: [http01proxies]
  verbs: [get, list, patch, update, watch]

- apiGroups: [operator.openshift.io]
  resources: [http01proxies/status]
  verbs: [get, patch, update]

- apiGroups: [operator.openshift.io]
  resources: [http01proxies/finalizers]
  verbs: [update]

- apiGroups: [config.openshift.io]
  resources: [infrastructures]
  verbs: [get, list, watch]
```

### 10.3 Manager Deployment

File: `config/manager/manager.yaml`

New env vars:

```yaml
- name: RELATED_IMAGE_CERT_MANAGER_HTTP01PROXY
  value: quay.io/openshift/cert-manager-http01-proxy:v0.1.0
- name: HTTP01PROXY_OPERAND_IMAGE_VERSION
  value: 0.1.0
```

`RELATED_IMAGE_*` follows OLM convention for declaring operand images (enables disconnected registry mirroring).

### 10.4 Makefile

New targets: `build-http01-proxy`, `image-build-http01-proxy`, plus `HTTP01PROXY_VERSION` variable and `local-run` env var additions.

---

## 11. Test Suite

### 11.1 Controller Tests (`controller_test.go` — 7 tests)

| Test | Verifies |
|------|----------|
| `TestReconcileWrongNamespace` | Non-operator namespace requests silently ignored, no API calls |
| `TestReconcileNotFound` | Deleted objects return empty result, no error |
| `TestReconcileGetError` | API server errors propagate (triggers retry) |
| `TestReconcileDeletion` | Full deletion: Delete called (MachineConfig cleanup), finalizer removed |
| `TestCleanUpDeletesMachineConfig` | Cleanup issues exactly 1 Delete call |
| `TestCleanUpMachineConfigDeleteFails` | Delete failure propagates as error |
| `TestCleanUpMachineConfigAlreadyGone` | NotFound during cleanup is OK (idempotent) |
| `TestReconcileDeletionWithoutFinalizer` | No finalizer = no-op |

### 11.2 Infrastructure Tests (`infrastructure_test.go` — 13 tests)

**validatePlatform** (10 sub-tests):

| Sub-test | Input | Expected |
|----------|-------|----------|
| non-baremetal platform | `{platformType: "AWS"}` | "not supported" |
| baremetal no API VIPs | `{BareMetal, apiVIPs: nil}` | "no API server VIPs" |
| baremetal no ingress VIPs | `{BareMetal, ingressVIPs: nil}` | "no ingress VIPs" |
| baremetal overlapping VIPs | `{apiVIPs: ["10.0.0.1"], ingressVIPs: ["10.0.0.1"]}` | "are the same" |
| baremetal valid distinct VIPs | `{apiVIPs: ["10.0.0.1"], ingressVIPs: ["10.0.0.2"]}` | empty (valid) |
| baremetal multiple distinct VIPs | `{apiVIPs: ["10.0.0.1","fd00::1"], ingressVIPs: ["10.0.0.2","fd00::2"]}` | empty (valid) |
| one overlapping pair | `{apiVIPs: ["10.0.0.1","10.0.0.3"], ingressVIPs: ["10.0.0.2","10.0.0.1"]}` | "are the same" |
| empty platform type | `{platformType: ""}` | "not supported" |
| None platform type | `{platformType: "None"}` | "not supported" |
| empty VIP slices | `{BareMetal, apiVIPs: []}` | "no API server VIPs" |

**discoverPlatform** (4 sub-tests): Get error, missing field, non-BareMetal, BareMetal with/without VIPs.

**getOrDiscoverPlatform** (3 sub-tests): Cache hit (no API call), cache miss (API called then cached), error doesn't cache.

### 11.3 MachineConfig Tests (`machineconfig_test.go` — 14 tests)

| Test | Verifies |
|------|----------|
| `TestRenderMachineConfig` | Kind, name, role label, nftables content (VIP, DNAT, masquerade, table name, inet family, prerouting/postrouting), systemd unit content (nft load, sysctl, iptables check-and-insert, ExecStop cleanup, Type=oneshot, RemainAfterExit), unit name and enabled state |
| `TestRenderMachineConfigDifferentVIP` | Rules use actual VIP passed, not hardcoded |
| `TestRenderMachineConfigIgnitionVersion` | Ignition version is 3.4.0 |
| `TestRenderMachineConfigFilePermissions` | File path correct, mode is 0600 (384 decimal) |
| `TestCreateOrApplyMachineConfigNoVIPs` | Empty apiVIPs and ingressVIPs return specific errors, no API calls |
| `TestCreateOrApplyMachineConfigCreate` | NotFound → Create called with correct name |
| `TestCreateOrApplyMachineConfigCreateError` | Create failure propagates |
| `TestCreateOrApplyMachineConfigGetError` | Non-NotFound Get errors propagate, no Create |
| `TestCreateOrApplyMachineConfigUpdateWhenSpecDiffers` | Different spec → Update called, ResourceVersion preserved |
| `TestCreateOrApplyMachineConfigUpdateError` | Update failure propagates |
| `TestCreateOrApplyMachineConfigNoOpWhenUnchanged` | Same spec → no Update, no Create |
| `TestDeleteMachineConfigSuccess` | Delete called with correct name and GVK |
| `TestDeleteMachineConfigNotFound` | NotFound during delete is OK |
| `TestDeleteMachineConfigError` | Delete failure propagates |

### 11.4 Utils Tests (`utils_test.go` — 12 tests)

| Test | Verifies |
|------|----------|
| `TestUpdateCondition` | 4 combinations: nil/nil=OK, err/nil=err, nil/fail=fail, err/fail=both errors aggregated |
| `TestAddFinalizer` | Already has finalizer=no-op, adds successfully, update fails, re-fetch fails |
| `TestRemoveFinalizer` | No finalizer=no-op, removes successfully, update fails |
| `TestUpdateStatus` | Success, Get fails, StatusUpdate fails |

### 11.5 Install Tests (`install_http01proxy_test.go` — 5 tests)

| Test | Verifies |
|------|----------|
| `PlatformDiscoveryError` | Infrastructure unavailable → retryable error |
| `UnsupportedPlatform` | AWS → "not supported" + cleanup called |
| `HappyPath` | BareMetal + VIPs → MachineConfig created, annotation set |
| `MachineConfigError` | MachineConfig API failure → error propagated |
| `AnnotationUpdateError` | MachineConfig succeeds but annotation update fails → error propagated |

### 11.6 Feature Gate Tests (`features_test.go`)

- HTTP01Proxy listed in `expectedDefaultFeatureState[false]` (disabled by default)
- Test verifies all pre-GA features default to disabled
- Test verifies all operator features can be enabled at runtime

### 11.7 E2E Test (`test/e2e/http01proxy_test.go`)

Runs on CI (non-baremetal cluster):

1. Enables `HTTP01Proxy` feature gate via OLM subscription patch
2. Waits for operator rollout
3. Creates HTTP01Proxy CR → waits for `Degraded=True` with "not supported" message
4. Cleans up CR and restores original feature gates

---

## 12. End-to-End Data Flow

```
1. User enables feature gate:
   UNSUPPORTED_ADDON_FEATURES=HTTP01Proxy=true → operator restarts

2. User creates CR:
   kubectl apply -f http01proxy.yaml
   (name=default, mode=DefaultDeployment)

3. API server validates:
   CEL: name == "default" ✓
   CEL: mode != CustomDeployment → no customDeployment field ✓

4. Controller receives reconcile event:
   → Add finalizer to HTTP01Proxy
   → Discover platform (read Infrastructure CR, cache result)
   → Validate: BareMetal? VIPs present? VIPs different?
   → Render MachineConfig with nftables + systemd templates
   → Create/update MachineConfig in Kubernetes
   → Set status: Degraded=False, Ready=True

5. Machine Config Operator picks up MachineConfig:
   → Drains each control plane node
   → Writes /etc/sysconfig/nftables-crtmgr-http01.conf
   → Creates crtmgr-http01-dnat.service
   → Reboots node

6. Node boots:
   → systemd starts crtmgr-http01-dnat.service (oneshot, before network)
   → sysctl enables ip_forward
   → nft loads DNAT/MASQUERADE rules
   → iptables inserts FORWARD ACCEPT rule

7. ACME challenge works:
   Let's Encrypt → API VIP:80 → DNAT → Ingress VIP:80 → router → solver pod
   solver pod → MASQUERADE → Let's Encrypt
   Certificate issued ✓

8. User deletes CR:
   → DeletionTimestamp set
   → Controller: delete MachineConfig → MCO rolls out removal → remove finalizer
   → Kubernetes GCs the HTTP01Proxy object
```

---

## 13. Key Design Decisions

| Decision | Rationale |
|----------|-----------|
| **nftables + iptables together** | Kernel evaluates both iptables-nft and native nftables hooks. OpenShift's iptables FORWARD chain has `policy DROP`, so both must ACCEPT forwarded packets. |
| **MachineConfig instead of DaemonSet** | Previous approach (PR #458) used a reverse proxy DaemonSet requiring a container image, Dockerfile, CI pipeline, ServiceAccount, RBAC, NetworkPolicies, and SCC. MachineConfig eliminates all of that. |
| **Singleton CR** | One API VIP and one Ingress VIP per cluster. Multiple HTTP01Proxy objects would be meaningless. |
| **Cached platform info** | Avoids redundant Infrastructure CR reads on every reconciliation loop. Cache invalidated via Infrastructure watch. |
| **`unstructured.Unstructured`** | Avoids Go module dependency on MachineConfig types. Works on clusters without MCO CRDs (MicroShift). |
| **Alpha feature gate, disabled by default** | New feature with limited platform support. Opt-in prevents accidental activation. |
| **Finalizer for cleanup** | Guarantees MachineConfig removal even if user deletes CR unexpectedly. |
| **IrrecoverableError for unsupported platforms** | Prevents futile 30-second retry loops on AWS/GCP/MicroShift. Sets `Degraded=True` and stops. |
| **File mode 0600 (384 decimal)** | nftables config contains VIP addresses — restrictive permissions limit exposure. |
| **MachineConfig name prefix `98-`** | High priority number ensures application late in the MCO stack, avoiding conflicts with system configs. |

---

## 14. Files Changed Summary

**43 files changed, +3479 lines, -8 lines**

| Category | Files | Lines |
|----------|-------|-------|
| API types and deepcopy | `api/operator/v1alpha1/` (3 files) | ~230 |
| Controller logic | `pkg/controller/http01proxy/` (6 source files) | ~580 |
| Controller tests | `pkg/controller/http01proxy/` (5 test files) | ~1400 |
| Generated clients | `pkg/operator/clientset/`, `informers/`, `listers/`, `applyconfigurations/` (11 files) | ~800 |
| CRD, RBAC, CSV, bundle | `config/`, `bundle/` (6 files) | ~400 |
| Manager wiring | `pkg/operator/setup_manager.go`, `starter.go` | ~40 |
| E2E test | `test/e2e/http01proxy_test.go` | ~130 |
| Build and config | `Makefile`, `.gitignore`, `config/manager/`, `config/samples/` | ~25 |
| Feature gate test update | `pkg/features/features_test.go` | ~5 |
