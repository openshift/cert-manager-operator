# API Contracts Guidelines

Rules for changing or adding to `api/operator/v1alpha1/`. Read `domain/*.md`
and the relevant ADR before touching a CR's Spec/Status. **Verify every field against
the Go types under `api/operator/v1alpha1/` — never invent, assume, or backfill a
field from upstream cert-manager/trust-manager/istio-csr docs.**

## 1. CRD Ownership: Operator vs. Operand

This repo mixes CRDs owned by **this operator** with CRDs owned by the **operands it
deploys**. Know which is which before editing:

| CRD | Owner | Where it lives |
|---|---|---|
| `CertManager` (`operator.openshift.io`) | This operator | `api/operator/v1alpha1/certmanager_types.go` |
| `IstioCSR` (`operator.openshift.io`) | This operator | `api/operator/v1alpha1/istiocsr_types.go` |
| `TrustManager` (`operator.openshift.io`) | This operator | `api/operator/v1alpha1/trustmanager_types.go` |
| `Certificate`, `Issuer`, `ClusterIssuer`, `Order`, `Challenge` (`cert-manager.io`/`acme.cert-manager.io`) | **cert-manager operand**, reconciled by the cert-manager controller Deployment, not this operator's loop | `vendor/github.com/cert-manager/...`; bases in `config/crd/bases/*-crd.yaml` |
| `Bundle` (`trust.cert-manager.io`) | **trust-manager operand** | `config/crd/bases/customresourcedefinition_bundles.trust.cert-manager.io.yml` |
| `CertManager` (`config.openshift.io`) | **Not wired** — stub CRD, no Go types, not in `config/crd/kustomization.yaml` | `config/crd/bases/config.openshift.io_certmanagers.yaml` |

Rules:

- Only add Go types for CRDs this operator's controllers actually reconcile
  (`operator.openshift.io/v1alpha1`). Never add Go types for operand CRDs — those are
  vendored, not owned here.
- Never edit `config.openshift.io_certmanagers.yaml`. It is dead/unused; a common
  mistake is assuming it's the operator API.
- Do **not** hand-author CRD YAML under `config/crd/bases/`. Operand CRDs
  (`*-crd.yaml`, `customresourcedefinition_*.yml`) are produced there by
  `make update-manifests` (`hack/update-{cert-manager,istio-csr,trust-manager}-manifests.sh`
  from upstream releases). After regeneration, wire the new file into
  `config/crd/kustomization.yaml` — do **not** add a Go type in
  `api/operator/v1alpha1/`.

## 2. Spec/Status Conventions

Two status models coexist by design (see [ADR-0001](../../docs/adr/0001-dual-controller-frameworks.md)).
Never assume one is used everywhere:

- **CertManager**: embeds OpenShift `apiv1.OperatorSpec` / `apiv1.OperatorStatus`
  (library-go status model: `ManagementState`, `Conditions`, `ObservedGeneration`,
  `Versions`, `Generations`, `UnsupportedConfigOverrides`).
- **IstioCSR** / **TrustManager**: use the repo's own `ConditionalStatus`
  (`api/operator/v1alpha1/meta.go`) — just `Conditions []metav1.Condition`. Condition
  types are `Ready` / `Degraded`; reasons are `Failed` / `Ready` / `Progressing`
  (`conditions.go`). Do not introduce new condition types without updating
  `conditions.go` and both `domain/istiocsr.md` and `trustmanager.md`.

Structural conventions to follow for any new CR or field:

- Top-level `Spec`/`Status` fields: lowercase JSON tag, `+kubebuilder:validation:Required`
  + `+required` (or `Optional`/`+optional`), and a doc comment starting with the
  lowercase field name (see `TrustManager`/`IstioCSR` struct comments) — this becomes
  the CRD schema description and OLM CSV doc.
- Config sub-structs are split into an operand-behavior config
  (`TrustManagerConfig`, `IstioCSRConfig`) and an operator-behavior config
  (`ControllerConfig` field — labels/annotations the operator applies to created
  resources). Follow this split for new fields: does it configure the *operand's*
  runtime behavior, or *how the operator creates* resources for it?
- New fields generally carry an explicit kubebuilder validation marker
  (`+kubebuilder:validation:{Required,Optional,Enum,Minimum,Maximum,MinLength,MaxLength,MinItems,MaxItems,Pattern}`)
  rather than relying on Go zero-values as implicit defaults — prefer `+kubebuilder:default:=`
  where a default makes sense.
- Lists that need merge semantics use `+listType=map` with `+listMapKey=...`
  (`NetworkPolicy`, `ConditionalStatus.Conditions`); simple dedup lists use
  `+listType=set` (`Revisions`, `CertificateDNSNames`); free-form ordered lists use
  `+listType=atomic` (`Tolerations`).
- Maps use `+mapType=atomic` (whole-map replace, e.g. `NodeSelector`) or
  `+mapType=granular` (per-key ownership, e.g. `Labels`/`Annotations`) — pick based on
  whether partial ownership by multiple actors is expected.

## 3. Immutability Rules

Immutability is enforced with CEL `+kubebuilder:validation:XValidation` using
`oldSelf`/`self`, **not** with webhooks (there is no admission webhook wired for these
CRs; `config/crd/patches/webhook_in_certmanagers.yaml` is present but commented out).
Patterns already in use:

- **Set-once, then locked**: `oldSelf == '' || self == oldSelf` (or the zero-value
  equivalent for ints) — e.g. `TrustNamespace`, `PrivateKeySize`,
  `PrivateKeyAlgorithm`, `Port`.
- **One-way ratchet** (can enable, can't disable): `oldSelf != 'true' || self == 'true'`
  — `DefaultNetworkPolicy`. Once `"true"`, it can never revert.
- **Fully immutable after any value is set**: `self == oldSelf` — `IssuerRef`,
  `Istio.Namespace`.
- **Creation-only field** (must be present or absent consistently): the
  `has(oldSelf.x) == has(self.x)` pattern — `PrivateKeyAlgorithm`/`PrivateKeySize`
  companion checks, `IssuerRef` presence check on `CertManagerConfig`.
- **Immutable list identity, mutable contents**: `NetworkPolicies` — `name`/
  `componentName` pairs are immutable once present, but `egress` rules within an
  existing entry are not locked by this rule.

When adding a new immutable field: put the `XValidation` on the field (or the
containing struct for cross-field rules), write a clear `message`, and document the
immutability in the field's doc comment (not just the marker) — reviewers and
generated CRD docs both need it in prose.

## 4. Singleton Naming

`TrustManager` is a cluster-scoped singleton named **`cluster`**, and `IstioCSR` is a
namespaced singleton named **`default`** (one per namespace) — both are enforced via CEL
on the resource:

```go
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'cluster'",message="... is a singleton, .metadata.name must be 'cluster'"
```

`CertManager` is also treated as a cluster-scoped singleton named **`cluster`** by
convention, but this is **not** CEL-enforced in `certmanager_types.go` — the library-go
controller set simply hardcodes `Lister().Get("cluster")` everywhere it reads the CR, so
a differently-named CertManager object is silently never reconciled rather than rejected
by the API. Don't assume CertManager's singleton behavior is schema-enforced when
reasoning about it.

If you add a **new** singleton CR, follow the TrustManager/IstioCSR CEL pattern above
(adjust the required name), and it must be `+genclient:nonNamespaced` if cluster-scoped.
IstioCSR's namespaced singleton behavior is additionally enforced by the controller
(`disallowMultipleIstioCSRInstances`), not by CEL alone, since CEL can't see sibling
objects — a new namespaced singleton needs the same controller-side check.

## 5. Feature-Gated CRDs

`IstioCSR` (GA, default **true**) and `TrustManager` (TechPreview, default **false**)
are gated in `api/operator/v1alpha1/features.go` via
`featuregate.Feature`/`OperatorFeatureGates`, enabled at runtime through
`--unsupported-addon-features` (see [ADR-0003](../../docs/adr/0003-feature-gates.md)).
`CertManager` is always on and ungated.

Rules for a new feature-gated CR:

- The **CRD itself is always installed** (shipped unconditionally in
  `config/crd/bases/` + `kustomization.yaml`); only the **controller** is gated.
  Do not try to conditionally install the CRD based on the feature gate.
  - Note: TrustManager's cluster `FeatureSet` gating requirement was **removed**
    (CM-1141) — new gates should follow the `TrustManager`/`IstioCSR` pattern of
    operator-local flag gating only, not cluster `FeatureSet` allow-listing, unless a
    new ADR says otherwise.
- Add the gate to `OperatorFeatureGates` in `features.go` with an explicit `Default`
  and `PreRelease` (`GA` or `"TechPreview"`), plus a doc comment linking the
  enhancement doc, mirroring `FeatureIstioCSR`/`FeatureTrustManager`.
- Wire enablement/CSV args the same way as the existing two gates
  (`pkg/features`, `setup_manager.go`); don't invent a new gating mechanism.

## 6. Generation / Manifests Workflow

Never hand-edit generated output. After changing any file in `api/operator/v1alpha1/`:

1. `make generate` — regenerates deepcopy (`zz_generated.deepcopy.go`) via
   `controller-gen object:...` and client-gen artifacts (`hack/update-clientgen.sh`).
2. `make manifests` — regenerates **operator** CRDs (`operator.openshift.io_*.yaml`)
   under `config/crd/bases/` and RBAC under `config/rbac/` via
   `controller-gen rbac:... crd webhook` from Go types in `api/operator/v1alpha1/`.
3. If operand versions/manifests changed (not API types), use
   `make update-manifests` (`hack/update-{cert-manager,istio-csr,trust-manager}-manifests.sh`)
   — refreshes **operand** CRDs in `config/crd/bases/` (`*-crd.yaml`, Bundle YAML) plus
   bindata; separate from `make generate`/`manifests`.
4. `make update` runs generate + update-manifests + update-bindata together; CI's
   `verify-scripts` (`verify-bindata`, `verify-deepcopy.sh`, `verify-clientgen.sh`,
   `verify-bundle.sh`) will fail the build if generated output is stale or hand-edited.

Never manually edit CRD **content** under `config/crd/bases/` (or
`zz_generated.deepcopy.go`, `pkg/operator/assets/bindata.go`, generated
clientset/informers/listers/applyconfigurations, or `bindata/` YAML). Edit Go
types and run `make manifests` for operator CRDs; bump upstream versions and run
`make update-manifests` for operand CRDs. You may edit
`config/crd/kustomization.yaml` to wire a newly generated CRD file.

## 7. What Not to Invent About Fields

- Don't add fields, enum values, or defaults that aren't present in the Go type under
  `api/operator/v1alpha1/` — verify in source, not from upstream cert-manager/
  istio-csr/trust-manager docs, memory, or naming conventions from other operators.
- Don't assume a field is namespaced, cluster-scoped, mutable, or defaulted without
  checking its markers (`+kubebuilder:validation:*`, `+kubebuilder:default`,
  `XValidation`) directly.
- Don't assume all three CRs share apply/reconcile semantics — CertManager uses
  library-go `resourceapply` (not SSA), IstioCSR uses Create+`UpdateWithRetry`, only
  TrustManager uses controller-runtime SSA with field owner `trust-manager-controller`.
  Field-ownership/conflict behavior described in docs must match the actual controller.
- Don't document `UnsupportedConfigOverrides.{Controller,Webhook,CAInjector}` as
  first-class `Spec` fields — they're helper types for parsing
  `OperatorSpec.UnsupportedConfigOverrides` (a `RawExtension`), not real schema fields.
- When documenting a CR, cross-check `domain/<name>.md` — if your reading of
  the Go type conflicts with that doc, prefer the Go source and flag the doc as
  possibly stale rather than propagating the conflict into new docs.
