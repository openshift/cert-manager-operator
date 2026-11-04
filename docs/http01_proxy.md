# HTTP01 challenge proxy for the API endpoint (BareMetal)

On BareMetal clusters the API VIP is served by keepalived/HAProxy on the control plane nodes, and nothing on port 80 of the API VIP reaches the OpenShift Ingress routers. ACME HTTP-01 challenges for API server certificates (for example `api.<cluster>.<domain>`) therefore fail, because the ACME server cannot fetch `http://<api-host>/.well-known/acme-challenge/<token>`.

The `HTTP01Proxy` resource solves this by forwarding port 80 traffic sent to the API VIP to the Ingress VIP, where the cert-manager HTTP-01 solver is exposed through a normal Ingress/Route.

## Enabling the feature

`HTTP01Proxy` is a Technology Preview feature behind the operator feature gate `HTTP01Proxy`, which is disabled by default. Enable it with the `UNSUPPORTED_ADDON_FEATURES` environment variable on the operator Subscription.

A JSON merge patch on `spec.config.env` replaces the **whole** list, which would drop other settings such as `TRUSTED_CA_CONFIGMAP_NAME` or proxy variables. Check what is already set first:

```bash
oc -n cert-manager-operator get subscription <subscription_name> -o jsonpath='{.spec.config.env}{"\n"}'
```

- If nothing is set, create the list:

  ```bash
  oc -n cert-manager-operator patch subscription <subscription_name> --type='merge' \
    -p '{"spec":{"config":{"env":[{"name":"UNSUPPORTED_ADDON_FEATURES","value":"HTTP01Proxy=true"}]}}}'
  ```

- If other variables are set but `UNSUPPORTED_ADDON_FEATURES` is not, append it and keep the existing entries:

  ```bash
  oc -n cert-manager-operator patch subscription <subscription_name> --type='json' \
    -p '[{"op":"add","path":"/spec/config/env/-","value":{"name":"UNSUPPORTED_ADDON_FEATURES","value":"HTTP01Proxy=true"}}]'
  ```

- If `UNSUPPORTED_ADDON_FEATURES` is already set, add `HTTP01Proxy=true` to its comma-separated value (for example `TrustManager=true,HTTP01Proxy=true`) with `oc -n cert-manager-operator edit subscription <subscription_name>`.

Then create the singleton:

```yaml
apiVersion: operator.openshift.io/v1alpha1
kind: HTTP01Proxy
metadata:
  name: cluster
spec: {}
```

## What the operator does

The controller reads `infrastructure/cluster`. On any platform other than `BareMetal`, the CR reports `Degraded=True` and nothing is applied.

On `BareMetal`, it renders a single MachineConfig, `98-nftables-crtmgr-http01-dnat`, for the `master` role. That MachineConfig contains:

- `/etc/sysconfig/nftables-crtmgr-http01.conf`, mode `0600`, which holds an `inet crtmgr_http01_dnat` table:
  - a `prerouting` DNAT rule for `<API VIP>:80` → `<Ingress VIP>:80`
  - a `postrouting` masquerade rule for the forwarded traffic
- `crtmgr-http01-dnat.service`, a oneshot unit that:
  - enables IP forwarding
  - loads the table
  - inserts the matching `FORWARD` accept rules
  - removes all of them again when the unit is stopped

API and Ingress VIPs are paired by IP family, so IPv4, IPv6 and dual-stack clusters are supported.

Deleting the `HTTP01Proxy` CR deletes the MachineConfig. The CR's finalizer is removed once that delete succeeds.

## Node disruption: control plane reboots

The proxy is delivered as a MachineConfig, so the Machine Config Operator (MCO) handles every change with its default node disruption behavior. With that default, MCO **drains and reboots each control plane node, one at a time**, when the rendered config changes. This happens when:

- the `HTTP01Proxy` CR is created (the MachineConfig is added),
- the API or Ingress VIPs in `infrastructure/cluster` change (the MachineConfig is re-rendered),
- the `HTTP01Proxy` CR is deleted (the MachineConfig is removed), and
- an operator upgrade changes the rendered MachineConfig. The operator compares a fingerprint of the rendered spec, so a new version that renders the rules file or unit differently updates the MachineConfig even when the CR and VIPs are unchanged. With automatic Subscription approval, that rollout starts as soon as the upgrade lands.

Plan these operations for a maintenance window. While HTTP01Proxy is in use, consider `installPlanApproval: Manual` on the Subscription so operator upgrades happen in that window too. Track the rollout with:

```bash
oc get mcp master -w
```

The operator does not manage the cluster's node disruption policy. Administrators who want to avoid these reboots can add entries for the rules file and the unit to `spec.nodeDisruptionPolicy` in `machineconfigurations.operator.openshift.io/cluster`. The OpenShift documentation describes this as "Using node disruption policies to minimize disruption from machine config changes". Use `Restart`, not `Reload`:

```yaml
spec:
  nodeDisruptionPolicy:
    files:
    - path: /etc/sysconfig/nftables-crtmgr-http01.conf
      actions:
      - type: Restart
        restart:
          serviceName: crtmgr-http01-dnat.service
    units:
    - name: crtmgr-http01-dnat.service
      actions:
      - type: DaemonReload
      - type: Restart
        restart:
          serviceName: crtmgr-http01-dnat.service
```

`crtmgr-http01-dnat.service` has no `ExecReload`, so a `Reload` action fails and can stall the MachineConfig rollout. That is intentional: re-running only `nft -f` would leave the iptables `FORWARD` rules for the old Ingress VIP in place. A restart runs the unit's stop commands, which remove the table and the `FORWARD` rules, and then its start commands, which add them again for the current VIPs.

Add these entries with `oc edit machineconfigurations.operator.openshift.io cluster`. A merge patch on `files` or `units` replaces any entries that are already there. This setup has not been validated by the operator, so test it on a non-production cluster first.

## Permissions

To manage its one MachineConfig, the operator's service account is granted:

- `create` on `machineconfigs.machineconfiguration.openshift.io`. Kubernetes RBAC cannot restrict `create` by object name.
- `get`, `update` and `delete` on the `machineconfigs` object named `98-nftables-crtmgr-http01-dnat` only.

The operator has no `list` or `watch` permission on MachineConfigs, and it reads its own MachineConfig without a cache.
