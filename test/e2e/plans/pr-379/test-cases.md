# Test Plan: CM-867 — TrustManager operand reconcilers (PR #379)

<!-- Source: https://github.com/openshift/cert-manager-operator/pull/379 -->
<!-- Repo: openshift/cert-manager-operator -->
<!-- Framework: Ginkgo v2; build tag: e2e -->
<!-- Runbook: .cursor/rules/rules.md (Steps 1–7; plan only — no prod edits) -->

## Summary

PR **#379** (**CM-867**) implements TrustManager **resource reconcilers** beyond the initial ServiceAccount path: **Deployment**, **Services** (webhook + metrics), **RBAC** (cluster and trust-namespace–scoped, leader election in operand namespace), **Issuer/Certificate** for webhook TLS, **ValidatingWebhookConfiguration** with cert-manager CA injection, **default CA package** ConfigMap/volume wiring, and shared **`pkg/controller/common`** validation. This plan lists **e2e-style scenarios** (default **10** cases per runbook) mapped to **`Feature:TrustManager`** / **`TechPreview`** specs, with dedup against current `test/e2e/trustmanager_test.go`. **TrustManager Bundle (`bundles.trust.cert-manager.io`)** is **out of scope** for this PR; **§I** does not apply (see **Upstream parity**).

## Test Cases

### CM-867-TC-001: Happy path — full managed operand graph

**Priority:** Critical  
**Domain:** `reconciliation`, `operand-rollout`, `operand-manifests`  
**Category:** 1 (Core)  
**OpenShift-specific:** yes  
**Coverage gap:** Prove all reconciler-owned objects exist together after Ready (SA, Deployment, Services, RBAC, Issuer, Certificate, VWC) with managed labels.  
**Prerequisites:** `cert-manager-operator` healthy; operand namespace `cert-manager`; TrustManager TechPreview enabled (`UNSUPPORTED_ADDON_FEATURES` / suite `BeforeAll`); `TrustManagers` CRD installed.  
**Steps:**

1. Create `TrustManager` named `cluster` with valid `spec.trustManagerConfig` (use existing builder / defaults).  
   **Expected:** `Ready=True`; no perpetual `Degraded=True` without recovery.
2. List or get each managed kind in `cert-manager` (and cluster-scoped RBAC/VWC as applicable): ServiceAccount `trust-manager`, Deployment `trust-manager`, Services `trust-manager` and `trust-manager-metrics`, Issuer/Certificate, ClusterRole/ClusterRoleBinding, Roles/RoleBindings as defined by controller, `ValidatingWebhookConfiguration` `trust-manager`.  
   **Expected:** All present; labels include `app.kubernetes.io/managed-by: cert-manager-operator` and `app: cert-manager-trust-manager` (or equivalent per constants).
**Stop condition:** Missing any core operand object blocks trust-manager admission and bundle distribution.

---

### CM-867-TC-002: Deployment — availability, flags, TLS volume, ServiceAccount ref

**Priority:** Critical  
**Domain:** `reconciliation`, `operand-rollout`, `tls`  
**Category:** 1  
**OpenShift-specific:** partial  
**Coverage gap:** Deployment spec matches bindata + TrustManager spec (args, TLS secret volume, `serviceAccountName`).  
**Prerequisites:** Same as TC-001.  
**Steps:**

1. Wait for Deployment `trust-manager` in `cert-manager` Available.  
   **Expected:** Ready replicas match desired; container args include trust-namespace, metrics, leader-election, webhook flags per product defaults.
2. Inspect pod template: TLS volume references `trust-manager-tls` (or configured secret); `serviceAccountName` is `trust-manager`.  
   **Expected:** Matches controller contract; pod can mount webhook cert material.
**Stop condition:** Wrong SA or missing TLS volume breaks webhooks and rollouts.

---

### CM-867-TC-003: Services — webhook and metrics endpoints

**Priority:** High  
**Domain:** `reconciliation`, `install-health`  
**Category:** 1  
**OpenShift-specific:** no  
**Coverage gap:** Distinct Services for webhook traffic vs metrics (`9402`) with correct selectors/ports.  
**Prerequisites:** TC-001 setup.  
**Steps:**

1. Get `Service/trust-manager` and `Service/trust-manager-metrics` in `cert-manager`.  
   **Expected:** Ports and selectors align with Deployment pods; managed labels present.
2. (Optional) Port-forward or in-cluster probe where allowed — document if skipped in CI.  
   **Expected:** Metrics port reachable from pod network if probed.
**Stop condition:** Missing metrics Service hides SRE monitoring; wrong Service breaks webhooks.

---

### CM-867-TC-004: RBAC — ClusterRole / Role matrix and SecretTargets scoping

**Priority:** High  
**Domain:** `rbac`, `reconciliation`, `openshift-rbac`  
**Category:** 1, 3  
**OpenShift-specific:** yes  
**Coverage gap:** ClusterRole gains/loses secret rules when `SecretTargets` policy toggles; Role/RoleBinding exist in trust namespace and leader election objects in operand namespace for custom trust namespace.  
**Prerequisites:** TC-001; ability to patch `TrustManager` spec.  
**Steps:**

1. With `SecretTargets` Disabled, fetch ClusterRole `trust-manager`.  
   **Expected:** No secret write/read rules that violate policy.
2. Update to `SecretTargets` Custom with explicit `authorizedSecrets`; wait for reconcile.  
   **Expected:** ClusterRole includes expected scoped secret verbs/names.
3. With custom `trustNamespace`, verify Role/RoleBinding in custom namespace and leader election Role(+Binding) remain in `cert-manager`.  
   **Expected:** Matches controller placement rules.
**Stop condition:** Over-broad secret RBAC is a security defect; missing rules breaks Custom bundle writes.

---

### CM-867-TC-005: Webhook — `ValidatingWebhookConfiguration` and CA injection

**Priority:** Critical  
**Domain:** `reconciliation`, `tls`, `negative-input-validation`  
**Category:** 1, 8  
**OpenShift-specific:** partial  
**Coverage gap:** VWC references correct Service; `cert-manager.io/inject-ca-from` annotation points at operator-managed Certificate.  
**Prerequisites:** cert-manager webhook/cainjector healthy.  
**Steps:**

1. Get `ValidatingWebhookConfiguration/trust-manager`.  
   **Expected:** `clientConfig.service` points to `trust-manager` Service in `cert-manager`; CA injection annotation references `cert-manager/trust-manager` Certificate (or current naming).
2. Confirm webhook `failurePolicy` / paths match shipped manifest intent (document if only smoke-level).  
   **Expected:** Admission can succeed once cert is Ready.
**Stop condition:** Miswired webhook blocks trust APIs cluster-wide.

---

### CM-867-TC-006: Certificate / Issuer chain and TLS Secret

**Priority:** Critical  
**Domain:** `reconciliation`, `tls`, `issuer`  
**Category:** 2  
**OpenShift-specific:** no  
**Coverage gap:** Issuer becomes ready, Certificate becomes ready, TLS Secret contains `tls.crt`, `tls.key`, `ca.crt`.  
**Prerequisites:** ClusterIssuer or Issuer wiring as today in operand namespace.  
**Steps:**

1. Wait for Issuer `trust-manager` Ready (or terminal failure with clear message).  
   **Expected:** Ready within suite timeouts.
2. Wait for Certificate `trust-manager` Ready; verify Secret `trust-manager-tls`.  
   **Expected:** Keys present; DNS/CN aligns with Service DNS name pattern.
**Stop condition:** Webhook TLS never materializes → broken admission.

---

### CM-867-TC-007: Default CA package — volume, mount, hash annotation, CNO bundle

**Priority:** High  
**Domain:** `reconciliation`, `operand-manifests`, `install-health`  
**Category:** 3, 1  
**OpenShift-specific:** yes  
**Coverage gap:** Enabling `DefaultCAPackage` creates/updates ConfigMap-backed volume, `--default-package-location`, pod template hash annotation; uses CNO-injected trusted CA in operator namespace.  
**Prerequisites:** `cert-manager-operator-trusted-ca-bundle` (or configured name) present when policy Enabled.  
**Steps:**

1. Toggle `defaultCAPackage.policy` Disabled → Enabled → Disabled per product behavior.  
   **Expected:** Deployment args/volumes/annotations follow existing e2e expectations; no silent failure on missing trusted CA bundle.
**Stop condition:** Broken CA package path breaks OpenShift trust bundles feature.

---

### CM-867-TC-008: External deletion — controller recreates managed resources

**Priority:** High  
**Domain:** `reconciliation`, `operand-rollout`  
**Category:** 1  
**OpenShift-specific:** no  
**Coverage gap:** Deleting Deployment, Service, ClusterRole, VWC, etc., is repaired by reconcile (SSA/update paths).  
**Prerequisites:** TC-001 steady state.  
**Steps:**

1. Delete selected managed objects one at a time (SA, Deployment, webhook Service, ClusterRole, VWC, …).  
   **Expected:** Each is recreated or repaired within `Eventually` windows used in suite.
**Stop condition:** Permanent loss of webhook or workload after transient delete.

---

### CM-867-TC-009: Metadata drift — labels and annotations (managed + custom)

**Priority:** Medium  
**Domain:** `reconciliation`, `overrides`  
**Category:** 3  
**Coverage gap:** Controller restores managed labels; merges `controllerConfig` labels/annotations; does not strip required cert-manager annotations on VWC.  
**Prerequisites:** TC-001.  
**Steps:**

1. Tamper managed labels on Deployment/SA/ClusterRole; wait for restore.  
   **Expected:** Drift corrected.
2. Create TrustManager with custom `controllerConfig` labels/annotations; verify they appear on representative resources and VWC still has CA injection annotation.  
   **Expected:** Merge rules respected.
**Stop condition:** Thrash loop or loss of CA injection annotation.

---

### CM-867-TC-010: Cross-controller health — Istio CSR unaffected (shared `pkg/controller/common`)

**Priority:** High  
**Domain:** `reconciliation`, `install-health`  
**Category:** 1, 4  
**OpenShift-specific:** yes  
**Coverage gap:** PR #379 touches `istiocsr` and `setup_manager`; Istio CSR controller and TrustManager can coexist without manager startup failures.  
**Prerequisites:** Optional Istio CSR TechPreview workflow per `istio_csr_test.go` labels.  
**Steps:**

1. Run or filter existing **`Feature:IstioCSR`** e2e smoke (create namespace, IstioCSR, wait operands).  
   **Expected:** Same pass rate as pre-change baseline on representative cluster.
2. With TrustManager enabled in subscription, confirm operator deployment ready and no crash loops referencing manager setup.  
   **Expected:** Operator `Available=True`.
**Stop condition:** Istio CSR regression or manager merge conflict is release-blocking.

---

## Coverage Map

| Scenario | Existing spec (`test/e2e/trustmanager_test.go` unless noted) | Domain | Decision | Upstream parity (#394) |
| --- | --- | --- | --- | --- |
| CM-867-TC-001 | `Context("resource creation")` / `It("should create all resources managed by the controller with correct labels")` | Core | **skip** — covered | N/A |
| CM-867-TC-002 | `Context("deployment configuration")` / `It("should have deployment available with correct configuration")` (+ related Its) | Operand | **skip** — covered | N/A |
| CM-867-TC-003 | Same resource-creation `It` (webhook + metrics Services asserted) | Install | **skip** — covered | N/A |
| CM-867-TC-004 | `Context("RBAC configuration")` (+ SecretTargets / custom trust namespace Its) | RBAC | **skip** — covered | N/A |
| CM-867-TC-005 | `Context("webhook and certificate configuration")` / CA injection + service ref Its | TLS / webhook | **skip** — covered | N/A |
| CM-867-TC-006 | Issuer/Certificate ready + TLS secret Its in same Context | Issuer / certs | **skip** — covered | N/A |
| CM-867-TC-007 | `Context("default CA package configuration")` / long transition `It` | Trust / OpenShift | **skip** — covered | N/A |
| CM-867-TC-008 | `Context("resource deletion and recreation")` | Reconcile | **skip** — covered | N/A |
| CM-867-TC-009 | `Context("label drift reconciliation")`, `Context("managed label removal reconciliation")`, `Context("custom labels and annotations")` | Overrides | **skip** — covered | N/A |
| CM-867-TC-010 | `test/e2e/istio_csr_test.go` + operator health helpers (`VerifyHealthyOperatorConditions`, observe patterns) | Trust / mesh | **skip** — covered elsewhere | N/A |

## Implementation (local, no PR)

Traceability for **`ginkgo --label-filter=CM-867-TC-...`** is wired on existing specs (no duplicate `It` bodies per runbook **§A**):

| TC ID | Ginkgo label location |
| --- | --- |
| CM-867-TC-001, CM-867-TC-003 | `trustmanager_test.go` — `It("should create all resources managed by the controller with correct labels", ...)` |
| CM-867-TC-002 | `trustmanager_test.go` — `It("should have deployment available with correct configuration", ...)` |
| CM-867-TC-004 | `trustmanager_test.go` — `It("should configure ClusterRoleBinding with correct subjects and roleRef", ...)` |
| CM-867-TC-005 | `trustmanager_test.go` — `It("should configure webhook with cert-manager CA injection annotation", ...)` |
| CM-867-TC-006 | `trustmanager_test.go` — `It("should have Certificate become ready and create TLS secret", ...)` |
| CM-867-TC-007 | `trustmanager_test.go` — `It("should reconcile deployment when default CA package policy transitions between Disabled and Enabled", ...)` |
| CM-867-TC-008 | `trustmanager_test.go` — `It("should recreate resources managed by the controller when deleted externally", ...)` |
| CM-867-TC-009 | `trustmanager_test.go` — label drift, managed label removal, and custom labels `It`s |
| CM-867-TC-010 | `istio_csr_test.go` — `It("should return cert-chain as response", ...)` |

**Follow-up ideas (not counted in the 10 TC cap — document if needed):**

- **Admission smoke:** send a request that should hit `trust-manager` validating webhook (product-specific resource); current suite mostly asserts object shape, not an admission HTTP round-trip — **gap** / future `extend`.
- **OLM/CSV RBAC:** verify Subscription-installed CSV grants operator SA `trustmanagers` verbs — often **manual** or release pipeline — **gap** unless added under `test/e2e` with explicit user approval for any install harness.

---

## Upstream parity (TrustManager Bundle only)

**N/A for CM-867 / PR #379.** This PR expands **operator-managed trust-manager operand** reconcilers (Deployment, RBAC, Services, webhooks, certs). It does **not** implement or require **`bundles.trust.cert-manager.io`** Bundle sync. If a ticket later maps to **§I** in `.cursor/rules/rules.md`, open a **CM-873**-style plan and use `trustmanager_bundle_test.go` / helpers per **[PR #394](https://github.com/openshift/cert-manager-operator/pull/394)** / **[PR #412](https://github.com/openshift/cert-manager-operator/pull/412)**.

---

## OLM / OpenShift

- **OLM / CSV:** PR #379 updates bundle manifests / RBAC for the operator to manage new resources — full CSV verification is typically **release / install** automation; e2e assumes operator already installed.
- **TechPreview:** TrustManager remains gated — tests must keep **`TechPreview`** / **`TechPreview:Inverted`** labels per **rules §D** and existing `trustmanager_test.go` patterns.
- **Namespaces:** Operand `cert-manager`; operator `cert-manager-operator` per suite constants.

---

## Ginkgo labels (§D) — apply when implementing or extending specs

| Dimension | Example |
| --- | --- |
| Platform | `Platform:Generic` (default CI) unless cloud-specific |
| Feature | `Feature:TrustManager` |
| TechPreview | `TechPreview` for gated-on paths; `TechPreview:Inverted` for default feature-set paths |

---

## File placement note

Canonical path per runbook: **`test/e2e/plans/pr-379/test-cases.md`** (this file). A copy may exist under workspace `local/test-plans/` for QE-only tracking — keep them in sync if both are used.
