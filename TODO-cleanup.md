# Cleanup Opportunities

Identified 2026-04-08 via codebase review. Grouped by effort level.

## Trivial (bundle into one PR)

- [ ] Typo: "name o the" → "name of the" in `istiocsr/constants.go:76`
- [ ] `fmt.Errorf("%s", msg)` → `errors.New(msg)` in `istiocsr/utils.go:501,547`
- [ ] `int32(420)` → `int32(0o644)` in `istiocsr/deployments.go`, `certmanager/deployment_overrides.go`
- [ ] Magic string `"Kubernetes"` → `defaultClusterID` constant in `istiocsr/deployments.go:140`
- [ ] Magic string `"default"` → `defaultIstioRevision` constant in `istiocsr/certificates.go:90`
- [ ] Duplicate constant `roleBindingSubjectKind` in `istiocsr/rbacs.go:17` and `trustmanager/constants.go:49` → move to common
- [ ] Duplicate constants `istiodCertificateCommonNameFmt` and `istiodCertificateDefaultDNSName` have same value `"istiod.%s.svc"` in `istiocsr/constants.go:52,55` → consolidate
- [ ] Remove redundant `sort.Strings` in `certmanager/deployment_overrides.go:103` (already sorted in `mergeContainerArgs`)
- [ ] `make(map[string]string, 0)` → `make(map[string]string)` in `certmanager/deployment_overrides_validation.go:89,138`

## Easy (individual PRs, mechanical changes)

- [ ] Replace 8 `decode*ObjBytes` functions in istiocsr with `common.DecodeObjBytes[T]` (~95 lines deleted)
- [ ] Move `updatePodTemplateLabels` to common (identical one-liner in istiocsr + trustmanager)
- [ ] Extract `UpdateContainerImage(deployment, envVar, containerName)` to common (shared pattern)
- [ ] Extract `BuildDefaultResourceLabels(appName, versionEnvVar)` to common (prevents label schema divergence)
- [ ] `IsEmptyString(interface{})` → `IsEmptyString(string)` in `test/library/utils.go:141` (unsafe type assertion)
- [ ] Duplicate `MustAsset` call in `certmanager/generic_deployment_controller.go:32,74` → call once, reuse
- [ ] Duplicate issuer API call in `istiocsr/deployments.go:247,371` → return from first call
- [ ] Decode YAML just to get SA name in `istiocsr/rbacs.go:21` → use a constant

## Medium (higher impact, needs design)

- [ ] Move `addFinalizer`/`removeFinalizer` to common (identical in istiocsr + trustmanager, ~80 lines)
- [ ] Move `updateStatus` to common (identical in istiocsr + trustmanager, needs generic interface)
- [ ] Extract create-or-update reconcile pattern (8+ copies in istiocsr → generic helper with callback)
- [ ] Batch status updates in istiocsr (5 separate API calls per reconcile → accumulate and flush once)
- [ ] Extract e2e `addOverride*` helpers (~400 lines of copy-paste in test utils → generic mutation helper)

## Open PRs (in flight)

- [x] CNF-22825 / PR #314 — Consolidate istiocsr logging around klog/v2 (CI: 7/9 green, e2e pending)
- [x] CNF-22826 / PR #242 — Consolidate context usage around context.TODO() (CI: 7/9 green, e2e pending)
