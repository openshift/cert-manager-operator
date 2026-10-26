# Approver-policy controller framework

The `ApproverPolicyManager` controller is a Tech Preview feature, disabled by
default. Enable it with the operator startup flag:

```text
--unsupported-addon-features=ApproverPolicyManager=true
```

For local development, pass the flag to the operator's `start` command alongside
the usual configuration. Cluster `FeatureSet` changes are not required. Changing
the startup flag requires restarting the operator.

The controller handles only the cluster-scoped `ApproverPolicyManager` named
`cluster`. The existing sample creates that resource:

```sh
oc apply -f config/samples/operator_v1alpha1_approverpolicymanager.yaml
oc get approverpolicymanager cluster -o yaml
```

This implementation provides controller registration, event filtering, finalizer
handling, and status reporting. It does not deploy approver-policy, create the
`cert-manager` namespace, or change cert-manager's built-in approval behavior.

During initialization, status reports:

| Condition | Status | Reason |
|-----------|--------|--------|
| Ready | False | Progressing |
| Degraded | False | Ready |

`Progressing` is the reason on the `Ready` condition, following the other optional
controllers' status convention. The message reports that operand installation is
pending. The controller retries after 30 seconds and does not report `Ready=True`
until a future implementation can verify operand readiness. Permanent client
failures use `Ready=False/Failed` and `Degraded=True/Failed`; status-write failures
are returned for retry.

The controller adds
`approverpolicymanager.operator.openshift.io/cert-manager-approver-policy-controller`
as its finalizer. Deleting the CR removes this finalizer and preserves any other
finalizers. This scaffold creates no operand resources to clean up; operand
installation must add actual cleanup before releasing the finalizer.

When the feature gate is disabled, the controller neither registers nor processes
requests, including deletion. Re-enable the gate to complete deletion of a CR
that still has its finalizer.
