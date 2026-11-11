# Trust-manager network policies

The cert-manager operator creates four `NetworkPolicy` objects in the `cert-manager` namespace whenever a `TrustManager` custom resource named `cluster` exists. These policies are applied automatically. There is no field on the `TrustManager` CR to enable or disable them, and they do not follow `CertManager.spec.defaultNetworkPolicy`.

The policies select only pods labeled `app: cert-manager-trust-manager`. Other workloads in the namespace are unaffected.

| Name | Traffic |
| --- | --- |
| `trust-manager-deny-all` | Default deny ingress and egress for trust-manager pods |
| `trust-manager-allow-egress-to-api-server` | Egress TCP 443 and 6443 (any destination) |
| `trust-manager-allow-ingress-to-webhook` | Ingress TCP 6443 (any source) |
| `trust-manager-allow-ingress-to-metrics` | Ingress TCP 9402 (any source) |

Allow rules restrict ports only. They do not use namespace selectors such as OVN `policy-group.network.openshift.io/host-network` or `openshift-user-workload-monitoring`, so the same manifests work on Hypershift and MicroShift. Tighter peers can be added later.

Deleting the `TrustManager` resource does not remove the policies.
