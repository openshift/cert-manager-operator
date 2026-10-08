#!/usr/bin/env bash
#
# Wait for the default cert-manager operands, enable the TrustManager feature
# gate, apply a minimal TrustManager CR, and wait until trust-manager is Available.
#
# Used by TLS scanner / PQC readiness CI jobs and for local verification against
# a cluster where the operator is already installed via OLM.
#
# Prerequisites:
#   - oc in PATH, logged into a cluster with the cert-manager-operator installed
#   - python3 in PATH (used to merge Subscription env without dropping existing vars)
set -o errexit
set -o nounset
set -o pipefail

OPERATOR_NAMESPACE="${OPERATOR_NAMESPACE:-cert-manager-operator}"
OPERAND_NAMESPACE="${OPERAND_NAMESPACE:-cert-manager}"
OPERATOR_DEPLOYMENT="${OPERATOR_DEPLOYMENT:-cert-manager-operator-controller-manager}"
FEATURE_ENV_NAME="UNSUPPORTED_ADDON_FEATURES"
FEATURE_ENV_VALUE="TrustManager=true"
POLL_ATTEMPTS="${POLL_ATTEMPTS:-60}"
POLL_SLEEP_SECONDS="${POLL_SLEEP_SECONDS:-5}"

wait_for_default_operands() {
  echo "Waiting for default cert-manager operand deployments..."
  oc wait --for=create "namespace/${OPERAND_NAMESPACE}" --timeout=5m
  oc wait --for=create -n "${OPERAND_NAMESPACE}" deployment/cert-manager --timeout=5m
  oc wait --for=create -n "${OPERAND_NAMESPACE}" deployment/cert-manager-webhook --timeout=5m
  oc wait --for=create -n "${OPERAND_NAMESPACE}" deployment/cert-manager-cainjector --timeout=5m
  oc wait --for=condition=Available -n "${OPERAND_NAMESPACE}" deployment/cert-manager --timeout=5m
  oc wait --for=condition=Available -n "${OPERAND_NAMESPACE}" deployment/cert-manager-webhook --timeout=5m
  oc wait --for=condition=Available -n "${OPERAND_NAMESPACE}" deployment/cert-manager-cainjector --timeout=5m
}

enable_trust_manager_feature_gate() {
  echo "Enabling TrustManager feature gate via subscription..."
  # operator-sdk run bundle creates a versioned Subscription name
  # (e.g. cert-manager-operator-v1-20-0-sub), not a fixed cert-manager-operator.
  # Match e2e getCertManagerOperatorSubscription: list and patch the one present.
  local sub
  sub=$(oc -n "${OPERATOR_NAMESPACE}" get subscriptions.operators.coreos.com \
    -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
  if [[ -z "${sub}" ]]; then
    echo "No Subscription found in ${OPERATOR_NAMESPACE} namespace"
    oc -n "${OPERATOR_NAMESPACE}" get subscriptions.operators.coreos.com -o yaml || true
    exit 1
  fi

  echo "Patching Subscription ${sub} (preserve existing env, merge TrustManager=true into UNSUPPORTED_ADDON_FEATURES)"
  local patch
  patch=$(oc -n "${OPERATOR_NAMESPACE}" get "subscription/${sub}" -o json | python3 -c '
import json, sys

gate_name = "UNSUPPORTED_ADDON_FEATURES"
gate_entry = "TrustManager=true"

def merge_trust_manager(existing):
    merged = []
    seen_trust = False
    for part in (p.strip() for p in (existing or "").split(",")):
        if not part:
            continue
        name, _, _ = part.partition("=")
        if name == "TrustManager":
            if not seen_trust:
                merged.append(gate_entry)
                seen_trust = True
            continue
        merged.append(part)
    if not seen_trust:
        merged.append(gate_entry)
    return ",".join(merged)

sub = json.load(sys.stdin)
cfg = (sub.get("spec") or {}).get("config") or {}
env = []
found = False
for item in cfg.get("env") or []:
    if item.get("name") != gate_name:
        env.append(item)
        continue
    if found:
        continue
    updated = dict(item)
    updated["value"] = merge_trust_manager(item.get("value") or "")
    env.append(updated)
    found = True
if not found:
    env.append({"name": gate_name, "value": gate_entry})
print(json.dumps({"spec": {"config": {"env": env}}}))
')
  oc -n "${OPERATOR_NAMESPACE}" patch "subscription/${sub}" --type=merge -p "${patch}"
}

wait_for_feature_gate_rollout() {
  echo "Waiting for TrustManager feature gate on operator deployment env and rollout..."
  local found=false env_val
  for _ in $(seq 1 "${POLL_ATTEMPTS}"); do
    env_val=$(oc -n "${OPERATOR_NAMESPACE}" get "deploy/${OPERATOR_DEPLOYMENT}" \
      -o jsonpath="{range .spec.template.spec.containers[0].env[?(@.name==\"${FEATURE_ENV_NAME}\")]}{.value}{end}" 2>/dev/null || true)
    # Token match so merged values like IstioCSR=true,TrustManager=true succeed.
    if [[ ",${env_val// /}," == *",${FEATURE_ENV_VALUE},"* ]]; then
      echo "Found ${FEATURE_ENV_NAME}=${env_val} on operator deployment"
      found=true
      break
    fi
    sleep "${POLL_SLEEP_SECONDS}"
  done
  if [[ "${found}" != "true" ]]; then
    echo "Timed out waiting for ${FEATURE_ENV_NAME}=${FEATURE_ENV_VALUE} on operator deployment"
    oc -n "${OPERATOR_NAMESPACE}" get "deploy/${OPERATOR_DEPLOYMENT}" -o yaml || true
    exit 1
  fi
  oc -n "${OPERATOR_NAMESPACE}" rollout status "deployment/${OPERATOR_DEPLOYMENT}" --timeout=5m
}

apply_trust_manager_cr() {
  echo "Creating TrustManager CR (minimal, match e2e newTrustManagerCR)..."
  oc apply -f - <<EOF
apiVersion: operator.openshift.io/v1alpha1
kind: TrustManager
metadata:
  name: cluster
spec:
  trustManagerConfig: {}
EOF
}

wait_for_trust_manager() {
  echo "Waiting for TrustManager Ready and trust-manager deployment..."
  local found=false ready
  for _ in $(seq 1 "${POLL_ATTEMPTS}"); do
    ready=$(oc get trustmanagers.operator.openshift.io cluster \
      -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || true)
    if [[ "${ready}" == "True" ]]; then
      echo "TrustManager CR is Ready"
      found=true
      break
    fi
    sleep "${POLL_SLEEP_SECONDS}"
  done
  if [[ "${found}" != "true" ]]; then
    echo "Timed out waiting for TrustManager Ready=True"
    oc get trustmanagers.operator.openshift.io cluster -o yaml || true
    exit 1
  fi
  oc wait --for=create -n "${OPERAND_NAMESPACE}" deployment/trust-manager --timeout=5m
  # The TrustManager CR can be Ready while an older pod is still running. If
  # its ServiceAccount was deleted, that pod's projected token is revoked and
  # container restarts keep the same volume. Recreate pods now that reconcile
  # has applied the current ServiceAccount, then wait until the new pod is up.
  echo "Recreating trust-manager pods so they mount the current ServiceAccount token..."
  oc delete pods -n "${OPERAND_NAMESPACE}" -l app=cert-manager-trust-manager --wait=true --ignore-not-found=true
  # Deployment Available can stay True until the controller observes the
  # deletion, so wait for the replacement pod itself.
  oc wait --for=create -n "${OPERAND_NAMESPACE}" pod -l app=cert-manager-trust-manager --timeout=5m
  oc wait --for=condition=Ready -n "${OPERAND_NAMESPACE}" pod -l app=cert-manager-trust-manager --timeout=5m
}

wait_for_default_operands
enable_trust_manager_feature_gate
wait_for_feature_gate_rollout
apply_trust_manager_cr
wait_for_trust_manager
echo "TrustManager operand is Available"
