# OpenShift Integration Guidelines

Rules for integrating cert-manager-operator with cluster-provided OpenShift services: egress proxy, trusted CA, TLS security profile, cloud credentials, monitoring, and optional (discoverable) APIs. All hooks are wired in `pkg/controller/certmanager/generic_deployment_controller.go` and applied per-operand deployment (`cert-manager`, `cert-manager-webhook`, `cert-manager-cainjector`) via `deploymentcontroller.DeploymentHookFunc`.

## 1. Proxy

- **Rule**: Never hardcode proxy env vars. OLM injects `HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY` into the operator Deployment when a cluster-wide egress proxy exists (CSV must declare `proxy-aware: true`).
- The operator propagates these to operands via `withProxyEnv` (`pkg/controller/certmanager/deployment_overrides.go`), which reads `operator-lib/proxy.ReadProxyVarsFromEnv()` and merges into container env for every operand deployment. Do not add a per-deployment opt-out.
- See [docs/proxy.md](../../docs/proxy.md).

## 2. Trusted Certificate Authority

- **Rule**: Never bake custom CAs into images or operand code. Trust is delivered only via a cluster-injected ConfigMap referenced by name.
- Flow: admin creates an empty ConfigMap in `cert-manager` NS, labels it `config.openshift.io/inject-trusted-cabundle=true` (CNO injects `ca-bundle.crt`), then sets `--trusted-ca-configmap=<name>` (flag bound to `operator.TrustedCAConfigMapName`, `pkg/cmd/operator/cmd.go`) via the operator's subscription/deployment env `TRUSTED_CA_CONFIGMAP_NAME`.
- Implementation: `withCAConfigMap` (`deployment_overrides.go`) looks up the ConfigMap via the target-namespace `ConfigMapInformer` lister; if not found it returns a retryable error (`"(Retrying) trusted CA config map %q doesn't exist"`). The library-go `DeploymentController` treats every hook error the same way: it sets `Degraded=True` **and** keeps retrying via the factory's rate limiter — there is no non-degrading "just requeue" state on this stack (see [error-handling-guidelines.md](error-handling-guidelines.md) §7). It never becomes a permanent, un-retried failure, but it does surface as Degraded until the ConfigMap appears.
- Mount contract is fixed and must not change: volume name `trusted-ca`, mount path `/etc/pki/tls/certs/cert-manager-tls-ca-bundle.crt`, `subPath: ca-bundle.crt` (constants `trustedCAVolumeName`, `trustedCAPath`, `defaultCABundleKey`).
- If `trustedCAConfigmapName` is empty, `withCAConfigMap` is a no-op — do not add a default configmap name.
- See [docs/proxy.md](../../docs/proxy.md) (Trusted CA section).

## 3. TLS Security Profile

- **Rule**: Do not hardcode TLS min-version/cipher args on operand containers. When the cluster's `APIServer/cluster` `spec.tlsAdherence` is `StrictAllComponents`, derive them from `spec.tlsSecurityProfile` via `pkg/tlsprofile`; when `tlsAdherence` is unset or `LegacyAdheringComponentsOnly`, the hook leaves operand args untouched (no cluster-derived override is applied at all) — see [security-guidelines.md](security-guidelines.md) for the adherence gate.
- `tlsprofile.EffectiveSpec(profile)` resolves `nil`/empty profile to `Intermediate` (API default semantics); supports `Old`/`Intermediate`/`Modern`/`Custom`. Never assume `Custom.Custom` is non-nil — return an error if missing.
- `CertManagerWebhookTLSArgs` / `CertManagerOperandMetricsTLSArgs` emit `--tls-min-version`, `--metrics-tls-min-version`, and cipher args, converting OpenSSL cipher names to IANA via `libgocrypto.OpenSSLToIANACipherSuites`.
- **Critical rule**: when `MinTLSVersion == VersionTLS13`, do **not** emit `--tls-cipher-suites` / `--metrics-tls-cipher-suites` at all — Go ignores cipher config for TLS 1.3 and setting it is a documented anti-pattern (`CertManagerCipherSuiteArgKeys` exists so other hooks can strip these keys, see `common.StripArgsByKeys`).
- Applied by `common.WithClusterTLSProfileFromAPIServer(apiServerInformer)` (`pkg/controller/common/tls_profile_hook.go`), registered only when the shared `config.openshift.io` informer factory is available — i.e. when the `Infrastructure` resource is discoverable (§6). This is the same gate used for `withCloudCredentials`; there is no independent `APIServer`-specific discovery check. This hook must run **before** `withUnsupportedArgsOverrideHook` in the hook chain so break-glass `unsupportedConfigOverrides` args always win over cluster-derived TLS args — do not reorder.
- Unknown deployment names are skipped gracefully (hook returns `nil`, args untouched). A missing `APIServer/cluster` object (the type is discoverable, but the singleton object doesn't exist) instead returns an error from the hook, which — like the trusted-CA/cloud-credentials errors above — is retried and sets Degraded via the library-go `DeploymentController`, not silently ignored. See `tls_profile_hook_test.go` cases `UnknownDeployment` and `APIServerNotFound`.

## 4. Cloud Credentials (ambient credentials)

- **Rule**: The operator only **mounts an existing Secret** into the `cert-manager` controller deployment for ACME DNS-01 ambient credentials (AWS Route53 / GCP Cloud DNS). It **must never create, own, or reconcile a `CredentialsRequest`** object — that is a cluster-admin/`ccoctl` responsibility documented for humans in [docs/cloud_credentials.md](../../docs/cloud_credentials.md), not operator code.
- Flag `--cloud-credentials-secret` (`operator.CloudCredentialSecret`) names a Secret that **must already exist** in the `cert-manager` namespace before it is referenced.
- Implementation: `withCloudCredentials` (`pkg/controller/certmanager/credentials_request.go`):
  - No-op for every deployment except `certmanagerControllerDeployment` — never mount on webhook/cainjector.
  - No-op if `secretName` is empty.
  - If the secret does not exist yet, return the same retryable `"(Retrying) cloud secret %q doesn't exist"` pattern as trusted-CA — this likewise sets `Degraded=True` on the library-go `DeploymentController` while it keeps retrying; it never becomes a permanent, un-retried failure (see [error-handling-guidelines.md](error-handling-guidelines.md) §7).
  - Reads `Infrastructure/cluster` `.status.platformStatus.Type` to decide mount shape:
    - **AWS**: volume `cloud-credentials` → secret mounted at `/.aws`; also sets `AWS_SDK_LOAD_CONFIG=1` env (required for the AWS SDK to bind `role_arn` from the credentials file).
    - **GCP**: volume `cloud-credentials` → secret key `service_account.json` projected to `/.config/gcloud/application_default_credentials.json`.
    - Any other platform type → hard error `"unsupported cloud provider %q for mounting cloud credentials secret"`. Do not silently ignore unsupported platforms.
- This hook is only registered when the Infrastructure informer is `Applicable()` (§6) — on clusters without the Infrastructure API, cloud-credentials mounting is skipped entirely, not defaulted.
- `ClusterIssuer` gets ambient credentials by default; `Issuer` requires `--issuer-ambient-credentials` on the controller (a container arg, set via CertManager spec overrides, not this hook).

## 5. Monitoring

- **Rule**: The operator does not create `ServiceMonitor`/`PodMonitor` objects itself. It only advertises CSV annotation `operatorframework.io/cluster-monitoring: "true"` and ships operand Services with the standard label set (`app.kubernetes.io/{name,instance,component}`) in `bindata/`; enabling scrape is a cluster/admin action.
- Operands expose Prometheus metrics on port `9402` at `/metrics` for all three components (controller, webhook, cainjector).
- Admins must enable OpenShift user-workload monitoring (`enableUserWorkload: true` in `cluster-monitoring-config`) and apply a `ServiceMonitor` selecting `cert-manager` namespace services by the labels above.
- Metrics TLS (when the cluster TLS profile requires it) is layered on via `tlsprofile.CertManagerOperandMetricsTLSArgs`, not via this doc's ServiceMonitor step — do not conflate the two; a metrics TLS listener still needs `insecureSkipVerify`/TLS config on the scraping side if enabled.
- See [docs/operand_metrics.md](../../docs/operand_metrics.md) for full scrape/query walkthrough.

## 6. Optional APIs (Infrastructure / APIServer discovery)

- **Rule**: `config.openshift.io` types (`Infrastructure`, `APIServer`) are **not guaranteed to exist** (e.g. some OKD/hypershift-guest topologies). Never assume they are present — always discover first.
- `pkg/operator/starter.go` performs discovery once at startup: `utils.NewResourceDiscoverer(infraGVR, configClient.Discovery())` + `utils.InitInformerIfAvailable(...)` returns an `OptionalInformer[SharedInformerFactory]`.
  - `Discover()` treats a discovery `NotFound` as `(false, nil)` — absence is not an error.
  - The shared informer factory (covers both `Infrastructures` and `APIServers`) is only constructed if discovery of the `Infrastructure` resource succeeds; `APIServer` itself is not separately probed.
- Downstream consumers **must** gate on `optInfraInformer.Applicable()` before:
  - Starting the informer factory (`starter.go`).
  - Registering `withCloudCredentials` and `common.WithClusterTLSProfileFromAPIServer` hooks, and adding their informers to the controller's informer list (`generic_deployment_controller.go`).
- When not applicable: cloud-credentials mounting and cluster-TLS-profile args are simply skipped (operand runs with whatever static args ship in bindata) — this must not crash or degrade the operator.
- Never add a new optional CRD/API dependency without following this same discover-then-gate pattern; do not add a hard informer `Start()`/lister `Get()` call without a prior `Applicable()`/discovery check.

## Reference Index

| Concern | Flag | Hook | Doc |
|---|---|---|---|
| Proxy | (OLM-injected env) | `withProxyEnv` | [proxy.md](../../docs/proxy.md) |
| Trusted CA | `--trusted-ca-configmap` | `withCAConfigMap` | [proxy.md](../../docs/proxy.md) |
| TLS profile | (from `APIServer/cluster`) | `common.WithClusterTLSProfileFromAPIServer` | `pkg/tlsprofile` |
| Cloud credentials | `--cloud-credentials-secret` | `withCloudCredentials` | [cloud_credentials.md](../../docs/cloud_credentials.md) |
| Monitoring | CSV `operatorframework.io/cluster-monitoring: "true"` | n/a (bindata Service labels) | [operand_metrics.md](../../docs/operand_metrics.md) |
| Optional APIs | n/a | `utils.InitInformerIfAvailable` + `Applicable()` | `pkg/operator/starter.go` |
