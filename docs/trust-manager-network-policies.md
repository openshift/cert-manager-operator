# Trust-manager network policies

The cert-manager operator creates four `NetworkPolicy` objects in the `cert-manager` namespace whenever a `TrustManager` custom resource named `cluster` exists. These policies are applied automatically. There is no field on the `TrustManager` CR to enable or disable them, and they do not follow `CertManager.spec.defaultNetworkPolicy`.

The policies select only pods labeled `app: cert-manager-trust-manager`. Other workloads in the namespace are unaffected.

| Name | Traffic |
| --- | --- |
| `trust-manager-deny-all` | Default deny ingress and egress for trust-manager pods |
| `trust-manager-allow-egress-to-api-server` | Egress TCP 443 and 6443 to OVN host-network peers (`policy-group.network.openshift.io/host-network`) |
| `trust-manager-allow-ingress-to-webhook` | Ingress TCP 6443 from host-network (kube-apiserver) |
| `trust-manager-allow-ingress-to-metrics` | Ingress TCP 9402 from `openshift-user-workload-monitoring` |

API egress uses the host-network namespace selector, not `openshift-kube-apiserver` by name. Trust-manager dials `kubernetes.default.svc:443`; kube-apiserver is host-networked, so OVN classifies that peer with `policy-group.network.openshift.io/host-network`. The rule allows both 443 (dial) and 6443 (enforced endpoint). That is tighter than cert-manager and istio-csr, which allow TCP 6443 to any destination.

Deleting the `TrustManager` resource does not remove the policies.
