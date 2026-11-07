# Trust-manager network policies

The cert-manager operator creates four `NetworkPolicy` objects in the `cert-manager` namespace whenever a `TrustManager` custom resource named `cluster` exists. These policies are applied automatically. There is no field on the `TrustManager` CR to enable or disable them, and they do not follow `CertManager.spec.defaultNetworkPolicy`.

The policies select only pods labeled `app: cert-manager-trust-manager`. Other workloads in the namespace are unaffected.

| Name | Traffic |
| --- | --- |
| `trust-manager-deny-all` | Default deny ingress and egress for trust-manager pods |
| `trust-manager-allow-egress-to-api-server` | Egress TCP 443 and 6443 to any destination (same peer shape as cert-manager API egress, plus TCP 443) |
| `trust-manager-allow-ingress-to-webhook` | Ingress TCP 6443 from host-network (kube-apiserver) |
| `trust-manager-allow-ingress-to-metrics` | Ingress TCP 9402 from `openshift-user-workload-monitoring` |

API egress allows only TCP 443 and 6443 and does not restrict destination namespaces, matching the cert-manager and istio-csr operand policies on supported OpenShift topologies. Webhook ingress still uses the OVN host-network selector so kube-apiserver can reach the validating webhook.

Deleting the `TrustManager` resource does not remove the policies.
