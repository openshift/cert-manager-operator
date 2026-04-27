# CM-873 — QE e2e scenario index (TrustManager Bundle)

This file is a **human-readable index** for [CM-873](https://redhat.atlassian.net/browse/CM-873) (TrustManager **Bundle** CR e2e).  
Implementation on `openshift/cert-manager-operator` landed in [#394](https://github.com/openshift/cert-manager-operator/pull/394), [#412](https://github.com/openshift/cert-manager-operator/pull/412), [#413](https://github.com/openshift/cert-manager-operator/pull/413).

When your fork is current with `openshift/cert-manager-operator` **master**, the scenarios below map to `test/e2e/trustmanager_bundle_test.go` (`Describe("Bundle", …)`).

## Scenario quick reference

| ID | Intent | Ginkgo `It` name (substring match) |
|----|--------|-------------------------------------|
| E2E-SC-01 | Inline → ConfigMap + drift restore | `sync inline source to ConfigMap target and restore` |
| E2E-SC-02 | ConfigMap source → target + source update | `sync ConfigMap source to ConfigMap target and re-sync` |
| E2E-SC-03 | Secret source → ConfigMap target | `sync Secret source to ConfigMap target` |
| E2E-SC-04 | Multiple sources → ConfigMap | `sync multiple sources to ConfigMap target` |
| E2E-SC-05 | Custom metadata on targets | `apply custom metadata to target ConfigMaps` |
| E2E-SC-06 | Namespace selector | `sync only to namespaces matching selector` |
| E2E-SC-07 | Inline change → targets update | `update targets when inline source changes` |
| E2E-SC-08 | Delete Bundle → cleanup | `remove targets when Bundle is deleted` |
| E2E-SC-09 | Secret target without policy | `targeting Secret without SecretTargets enabled` |
| E2E-SC-10 | useDefaultCAs without DefaultCAPackage | `useDefaultCAs without DefaultCAPackage` |
| E2E-SC-11 | Sources outside trust ns | `not sync sources that exist outside the trust namespace` |
| E2E-SC-12 | Inline → Secret (SecretTargets) | `sync inline source to Secret target` |
| E2E-SC-13 | Dual ConfigMap + Secret targets | `sync to both ConfigMap and Secret targets` |
| E2E-SC-14 | CM source → Secret | `sync ConfigMap source to Secret target` |
| E2E-SC-15 | Secret tamper restore | `restore Secret target when tampered` |
| E2E-SC-16 | Unauthorized Secret name | `not in authorizedSecrets list` |
| E2E-SC-17 | Disable SecretTargets status | `SecretTargetsDisabled` |
| E2E-SC-18 | useDefaultCAs → ConfigMap (DefaultCAPackage) | `sync useDefaultCAs source to ConfigMap target` |
| E2E-SC-19 | Default CAs + inline | `include default CAs alongside explicit inline` |
| E2E-SC-20 | Package CM drift reconcile | `reconcile unintended package ConfigMap drift` |
| E2E-SC-21 | CNO CA propagation | `propagate CNO CA bundle update` |
| E2E-SC-22 | useDefaultCAs + inline → CM + Secret | `sync useDefaultCAs and inline sources to both` |
| E2E-SC-23 | Custom trustNamespace CM | `sync ConfigMap source from custom trust namespace` |
| E2E-SC-24 | Custom trustNamespace Secret | `sync Secret source from custom trust namespace` |
| E2E-SC-25 | Default ns not synced (custom trust ns) | `not sync sources from default namespace when custom` |
| E2E-SC-26 | Filter expired on | `exclude expired certificates` |
| E2E-SC-27 | Filter expired off | `re-sync same Bundle with expired certs included` |

## Run (upstream tree)

```bash
go test ./test/e2e -tags=e2e -v -count=1 -ginkgo.focus='Bundle'
```
