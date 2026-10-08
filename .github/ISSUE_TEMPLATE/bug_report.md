---
name: Bug Report
about: Report a cert-manager-operator bug
title: '[BUG] '
labels: bug
assignees: ''
---

Bugs that need a product fix should be filed in Jira (`OCPBUGS` or `CM`) so they
track OpenShift releases. Use this GitHub template only for repo-local notes or
when you cannot file Jira.

## Bug Description

A clear and concise description of what the bug is.

## To Reproduce

1. Install cert-manager-operator (OLM CSV version / `make local-run`)
2. Apply the relevant CR (`CertManager` / `IstioCSR` / `TrustManager`)
3. See error (`oc get` / operator logs)

## Expected Behavior

What you expected to happen.

## Actual Behavior

What actually happened. Include `oc` output or operator logs if you have them.

## Environment

- OpenShift version:
- cert-manager-operator version (CSV / image):
- Operand (cert-manager / istio-csr / trust-manager) version if known:
- Cloud provider / platform (AWS, Azure, GCP, none):
- Feature gates (`--unsupported-addon-features`) if relevant:

## Additional Context

Must-gather, related CRs, or a possible cause.
