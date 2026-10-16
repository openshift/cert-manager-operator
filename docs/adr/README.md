---
status: accepted
applies_to:
  - docs/adr/
---

# Architecture Decision Records

Canonical ADRs for Cert Manager Operator. Format follows [Michael Nygard](https://cognitect.com/blog/2011/11/15/documenting-architecture-decisions) with sequential `NNNN-title.md` filenames.

| ADR | Title | Status |
|-----|-------|--------|
| [0001](0001-dual-controller-frameworks.md) | Dual controller frameworks (library-go + controller-runtime) | Accepted |
| [0002](0002-apply-strategies.md) | Per-controller resource apply strategy | Accepted |
| [0003](0003-feature-gates.md) | Operator feature gates via `--unsupported-addon-features` | Accepted |

## Status lifecycle

Each ADR has a **Status** of `Proposed`, `Accepted`, `Deprecated`, or `Superseded`. When a decision is revised, update that ADR in place: set **Deprecated** or **Superseded**, and link the replacement. Do not rewrite history of an Accepted ADR.

## Adding an ADR

1. Copy the next number (`0004-…`).
2. Fill **Status**, **Context**, **Decision**, and **Consequences**.
3. Link it from this table and from `AGENTS.md`.
