# FIPS Compliance Guidelines

This document explains how FIPS (Federal Information Processing Standards) support is
implemented and enforced for the Cert Manager Operator, and what should **not** be
changed without a deliberate, reviewed decision.

## Why This Matters

The operator's OLM `ClusterServiceVersion` advertises
`features.operators.openshift.io/fips-compliant: "true"`
(`bundle/manifests/cert-manager-operator.clusterserviceversion.yaml`). This is a
public promise to OpenShift customers running in FIPS mode. If the binary shipped
in the operator image is not actually built with FIPS-validated crypto, this
annotation is misleading and the operator is out of compliance. Every build-time
mechanism described below exists to keep that annotation true.

## Build-Time FIPS Rules

FIPS enablement happens primarily at **build time**, via `hack/go-fips.sh`, which is
sourced from the `build-operator` Makefile target:

```1:11:hack/go-fips.sh
#!/bin/bash

if GOEXPERIMENT="strictfipsruntime" go build ./tools; then
    echo "INFO: building with FIPS support"

    export GOEXPERIMENT="strictfipsruntime"
    export GOFLAGS="${GOFLAGS} -tags=strictfipsruntime,openssl"
else
    echo "WARN: building without FIPS support, GOEXPERIMENT strictfipsruntime is not available in the go compiler"
    echo "WARN: this build cannot be used in CI or production, due to lack of FIPS!!"
fi
```

```351:353:Makefile
.PHONY: build-operator
build-operator: ## Build operator binary only (no checks or code generation).
	@GOFLAGS="-mod=vendor" source hack/go-fips.sh && $(GO) build $(GOBUILD_VERSION_ARGS) -o $(BIN)
```

The logic:

1. `go-fips.sh` first probes whether the local Go toolchain supports
   `GOEXPERIMENT=strictfipsruntime` by trying a throwaway build of `./tools`.
2. If supported, it **exports** `GOEXPERIMENT=strictfipsruntime` and appends
   `-tags=strictfipsruntime,openssl` to `GOFLAGS` for the real build that follows
   in the same Makefile recipe line (`source` keeps the exported vars in the same
   shell).
3. If unsupported, it prints two `WARN` lines and lets the build continue
   **without** FIPS flags — intended as a fallback for local development only,
   not for shipped artifacts.

The container image build (`Dockerfile`) uses `docker.io/golang:1.26` (expected to
support `strictfipsruntime`) and runs `make build`, so production and CI images are
built with FIPS mode enabled as long as that toolchain probe succeeds. There is no
separate FIPS/non-FIPS image variant — FIPS-ness is a property of the Go toolchain
used, not a build arg or Dockerfile stage.

## When Non-FIPS Builds Are Acceptable

A non-FIPS build (the `WARN` branch of `go-fips.sh`) is generally only appropriate when:

- You are building **locally** on a developer machine whose Go toolchain does not
  support `strictfipsruntime` (e.g. a non-RHEL/non-vendor Go distribution).
- The resulting binary is used **only** for local iteration, unit tests, or
  `make local-run` against a dev cluster — never for anything that leaves your
  workstation.

Non-FIPS builds should **not**:

- Be pushed as a CI artifact or container image (`make image-build` in CI always
  uses the FIPS-capable Go image, so this should not occur in practice).
- Be used for release, QE sign-off, or any environment claiming FIPS support.
- Trigger a change to the `fips-compliant: "true"` CSV annotation — the annotation
  describes what the *shipped* image guarantees, not what a given local build
  happened to produce.

If you find yourself routinely hitting the `WARN` path locally, prefer fixing your
Go toolchain/environment over "fixing" it by weakening `go-fips.sh` or its build
flags.

## Crypto Fork Expectations (`go.mod` replace)

```140:140:go.mod
replace github.com/cert-manager/cert-manager => github.com/openshift/jetstack-cert-manager v1.21.2
```

Upstream `cert-manager/cert-manager` uses Go's standard `crypto/*` packages
directly in ways that are not guaranteed to route through the FIPS-validated
BoringCrypto/OpenSSL module when `strictfipsruntime` is active. To close this
gap, all `cert-manager/cert-manager` imports are transparently redirected to
**`openshift/jetstack-cert-manager`**, a Red Hat-maintained fork that:

- Tracks the same upstream version tag (`v1.21.2` here — kept in lockstep with
  `CERT_MANAGER_VERSION` in the Makefile).
- Carries the minimal patch set needed so the vendored crypto call paths are
  compatible with `strictfipsruntime` / `openssl` build tags.

Expectations for anyone touching this:

- The replace target version **should match** `CERT_MANAGER_VERSION` in the
  Makefile. Bumping one without the other can desync manifests
  (`update-manifests`) from the vendored code.
- Avoid removing or "simplifying" this replace directive to point back at upstream
  `cert-manager/cert-manager`, even temporarily for debugging — doing so silently
  drops FIPS compliance for the cert-manager operand's crypto paths while the
  `fips-compliant` CSV annotation would still claim otherwise.
- Fork updates (new patches, rebasing onto a newer upstream tag) should come from
  the same upstream/downstream sync process that manages this fork, not from ad
  hoc local patches applied to vendor code.
- After changing the replace target or version, run `make update-vendor` (which
  runs `go mod tidy`, `go work sync`, `go work vendor`) and `make verify-deps` —
  avoid hand-editing `vendor/` to match.

## The `fips-compliant` CSV Annotation

```283:287:bundle/manifests/cert-manager-operator.clusterserviceversion.yaml
    features.operators.openshift.io/csi: "false"
    features.operators.openshift.io/disconnected: "true"
    features.operators.openshift.io/fips-compliant: "true"
    features.operators.openshift.io/proxy-aware: "true"
    features.operators.openshift.io/tls-profiles: "false"
```

This annotation is generated/maintained as part of the OLM bundle
(`config/manifests/bases/cert-manager-operator.clusterserviceversion.yaml` is the
base; `bundle/manifests/...` is the generated bundle output from `make bundle`).
It should stay `"true"` as long as:

- `hack/go-fips.sh` is wired into `build-operator`, and
- the `openshift/jetstack-cert-manager` replace is in place for the operand
  crypto paths.

If either of those build-time guarantees is ever removed or weakened, this
annotation should be flipped to `"false"` in the same change — that is a
compliance-affecting decision that needs explicit sign-off, not something to do
as a side effect of an unrelated refactor.

## What Agents Should Not Change Casually

Treat the following as **high-risk, review-required** changes. Avoid modifying
them opportunistically while working on unrelated features/bugs:

1. **`hack/go-fips.sh`** — avoid removing the `GOEXPERIMENT`/`GOFLAGS` exports, the
   `strictfipsruntime`/`openssl` build tags, or silencing the `WARN` messages.
2. **`build-operator` target in `Makefile`** — avoid dropping the
   `source hack/go-fips.sh` step or reordering it after the `go build` call.
3. **The `go.mod` replace for `cert-manager/cert-manager`** — avoid pointing it
   back at upstream, forking it to a different repo, or bumping its version
   independently of `CERT_MANAGER_VERSION` without understanding the crypto
   implications.
4. **`features.operators.openshift.io/fips-compliant` in the CSV** (both the
   `config/manifests/bases` source and the generated `bundle/manifests` copy) —
   only change together with a real change to the build-time FIPS guarantees, and
   call it out explicitly in the PR description.
5. **The builder base image in `Dockerfile`** — swapping to a Go distribution
   without `strictfipsruntime` support silently downgrades every production
   image to the `WARN`/non-FIPS path.

If a task seems to require touching any of the above, stop and confirm the FIPS
implications with a human reviewer (or at minimum, call it out prominently) before
proceeding — do not treat it as routine build-config cleanup.

## Quick Reference

| Concern | Source of truth |
|---|---|
| FIPS build flags | `hack/go-fips.sh` |
| Wiring into build | `Makefile` → `build-operator` |
| Operand crypto fork | `go.mod` → `replace github.com/cert-manager/cert-manager => github.com/openshift/jetstack-cert-manager ...` |
| Compliance claim | `bundle/manifests/cert-manager-operator.clusterserviceversion.yaml` → `features.operators.openshift.io/fips-compliant` |
| Human-readable overview | `architecture/components.md` (FIPS row) |
