# Trust-manager network policies

The cert-manager operator creates four `NetworkPolicy` objects in the `cert-manager` namespace whenever a `TrustManager` custom resource named `cluster` exists. These policies are applied automatically. There is no field on the `TrustManager` CR to enable or disable them, and they do not follow `CertManager.spec.defaultNetworkPolicy`.

The policies select only pods labeled `app: cert-manager-trust-manager`. Other workloads in the namespace are unaffected.

Deleting the `TrustManager` resource does not remove the policies.
