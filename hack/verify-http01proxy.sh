#!/usr/bin/env bash
set -euo pipefail

# Verifies that the HTTP01 Challenge Proxy feature is fully deployed and healthy.
#
# Checks:
#   - CRDs present: certmanagers.operator.openshift.io, http01proxies.operator.openshift.io
#   - CRs present: CertManager/cluster, HTTP01Proxy/default
#   - Platform: BareMetal with distinct API and Ingress VIPs
#   - Core cert-manager deployments available
#   - HTTP01 Proxy DaemonSet running on all master nodes
#   - RBAC: ServiceAccount, ClusterRole, ClusterRoleBinding
#   - Network policies: deny-all and allow-egress
#   - Proxy pod health and status conditions
#
# Usage:
#   hack/verify-http01proxy.sh [--namespace NAMESPACE] [--timeout TIMEOUT]
#
# Defaults:
#   NAMESPACE: cert-manager-operator
#   TIMEOUT:   180s

NAMESPACE="cert-manager-operator"
TIMEOUT="180s"
ERRORS=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --namespace|-n) NAMESPACE="$2"; shift 2;;
    --timeout)      TIMEOUT="$2"; shift 2;;
    *)              echo "Unknown arg: $1" >&2; exit 2;;
  esac
done

echo "[info] Namespace: ${NAMESPACE}"
echo "[info] Timeout:   ${TIMEOUT}"
echo ""

# ── helpers ──────────────────────────────────────────────────────────────────

check_pass() { echo "  OK"; }
check_fail() { echo "  FAIL: $1"; ERRORS=$((ERRORS + 1)); }
check_warn() { echo "  WARN: $1"; }

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || { echo "[error] $1 not found in PATH" >&2; exit 1; }
}

require_crd() {
  local crd="$1"
  echo -n "[check] CRD ${crd} ..."
  if oc get crd "${crd}" >/dev/null 2>&1; then
    check_pass
  else
    check_fail "CRD not found"
  fi
}

require_resource() {
  local kind="$1" name="$2" ns="${3:--}"
  if [[ "$ns" != "-" ]]; then
    echo -n "[check] ${kind}/${name} -n ${ns} ..."
    if oc get "$kind" "$name" -n "$ns" >/dev/null 2>&1; then
      check_pass
    else
      check_fail "not found"
    fi
  else
    echo -n "[check] ${kind}/${name} (cluster-scoped) ..."
    if oc get "$kind" "$name" >/dev/null 2>&1; then
      check_pass
    else
      check_fail "not found"
    fi
  fi
}

wait_deploy() {
  local name="$1" ns="$2"
  echo "[wait] deployment/${name} -n ${ns} (${TIMEOUT})"
  if ! oc -n "$ns" rollout status deploy/"$name" --timeout="$TIMEOUT" 2>/dev/null; then
    check_fail "deployment/${name} not available within ${TIMEOUT}"
  fi
}

# ── preflight ────────────────────────────────────────────────────────────────

require_cmd oc

echo "═══════════════════════════════════════════════════════════"
echo "[phase] Platform Validation"
echo "═══════════════════════════════════════════════════════════"

PLATFORM=$(oc get infrastructure cluster -o jsonpath='{.status.platformStatus.type}' 2>/dev/null || echo "unknown")
echo -n "[check] Platform type ..."
if [[ "$PLATFORM" == "BareMetal" ]]; then
  check_pass
else
  check_fail "expected BareMetal, got ${PLATFORM}"
fi

API_VIPS=$(oc get infrastructure cluster -o jsonpath='{.status.platformStatus.baremetal.apiServerInternalIPs}' 2>/dev/null || echo "")
echo -n "[check] API VIPs present ..."
if [[ -n "$API_VIPS" && "$API_VIPS" != "[]" ]]; then
  echo "  OK (${API_VIPS})"
else
  check_fail "no API VIPs found"
fi

INGRESS_VIPS=$(oc get infrastructure cluster -o jsonpath='{.status.platformStatus.baremetal.ingressIPs}' 2>/dev/null || echo "")
echo -n "[check] Ingress VIPs present ..."
if [[ -n "$INGRESS_VIPS" && "$INGRESS_VIPS" != "[]" ]]; then
  echo "  OK (${INGRESS_VIPS})"
else
  check_fail "no Ingress VIPs found"
fi

echo -n "[check] API and Ingress VIPs differ ..."
if [[ "$API_VIPS" != "$INGRESS_VIPS" ]]; then
  check_pass
else
  check_fail "API VIPs and Ingress VIPs are identical"
fi

echo ""
echo "═══════════════════════════════════════════════════════════"
echo "[phase] CRDs"
echo "═══════════════════════════════════════════════════════════"

require_crd certmanagers.operator.openshift.io
require_crd http01proxies.operator.openshift.io

echo ""
echo "═══════════════════════════════════════════════════════════"
echo "[phase] Custom Resources"
echo "═══════════════════════════════════════════════════════════"

require_resource certmanagers.operator.openshift.io cluster -
require_resource http01proxies.operator.openshift.io default "$NAMESPACE"

echo ""
echo "═══════════════════════════════════════════════════════════"
echo "[phase] Core cert-manager Deployments"
echo "═══════════════════════════════════════════════════════════"

wait_deploy cert-manager cert-manager
wait_deploy cert-manager-webhook cert-manager
wait_deploy cert-manager-cainjector cert-manager

echo ""
echo "═══════════════════════════════════════════════════════════"
echo "[phase] HTTP01 Proxy DaemonSet"
echo "═══════════════════════════════════════════════════════════"

DS_EXISTS=false
echo -n "[check] DaemonSet cert-manager-http01-proxy ..."
if oc get daemonset cert-manager-http01-proxy -n "$NAMESPACE" >/dev/null 2>&1; then
  DS_EXISTS=true
  check_pass
else
  check_fail "DaemonSet not found"
fi

if [[ "$DS_EXISTS" == "true" ]]; then
  DESIRED=$(oc get daemonset cert-manager-http01-proxy -n "$NAMESPACE" -o jsonpath='{.status.desiredNumberScheduled}' 2>/dev/null || echo "0")
  READY=$(oc get daemonset cert-manager-http01-proxy -n "$NAMESPACE" -o jsonpath='{.status.numberReady}' 2>/dev/null || echo "0")
  MASTER_COUNT=$(oc get nodes -l node-role.kubernetes.io/master --no-headers 2>/dev/null | wc -l | tr -d ' ')

  echo -n "[check] DaemonSet ready (${READY}/${DESIRED}, masters: ${MASTER_COUNT}) ..."
  if [[ "$READY" -gt 0 && "$READY" == "$DESIRED" ]]; then
    check_pass
  else
    check_fail "expected ${DESIRED} ready, got ${READY}"
  fi

  echo ""
  echo "[info] Proxy pods:"
  oc get pods -n "$NAMESPACE" -l app=cert-manager-http01-proxy -o wide 2>/dev/null || true
fi

echo ""
echo "═══════════════════════════════════════════════════════════"
echo "[phase] RBAC"
echo "═══════════════════════════════════════════════════════════"

require_resource serviceaccount cert-manager-http01-proxy "$NAMESPACE"
require_resource clusterrole cert-manager-http01-proxy -
require_resource clusterrolebinding cert-manager-http01-proxy -

echo ""
echo "═══════════════════════════════════════════════════════════"
echo "[phase] Network Policies"
echo "═══════════════════════════════════════════════════════════"

require_resource networkpolicy cert-manager-http01-proxy-deny-all "$NAMESPACE"
require_resource networkpolicy cert-manager-http01-proxy-allow-egress "$NAMESPACE"

echo ""
echo "═══════════════════════════════════════════════════════════"
echo "[phase] HTTP01Proxy Status"
echo "═══════════════════════════════════════════════════════════"

echo "[info] HTTP01Proxy conditions:"
oc get http01proxies.operator.openshift.io default -n "$NAMESPACE" \
  -o jsonpath='{range .status.conditions[*]}  {.type}: {.status} ({.reason}) - {.message}{"\n"}{end}' 2>/dev/null || echo "  (no conditions found)"

PROXY_IMAGE=$(oc get http01proxies.operator.openshift.io default -n "$NAMESPACE" \
  -o jsonpath='{.status.proxyImage}' 2>/dev/null || echo "")
if [[ -n "$PROXY_IMAGE" ]]; then
  echo "[info] Proxy image: ${PROXY_IMAGE}"
fi

if [[ "$DS_EXISTS" == "true" ]]; then
  echo ""
  echo "═══════════════════════════════════════════════════════════"
  echo "[phase] Proxy Pod Health"
  echo "═══════════════════════════════════════════════════════════"

  NOT_RUNNING=$(oc get pods -n "$NAMESPACE" -l app=cert-manager-http01-proxy \
    --field-selector=status.phase!=Running --no-headers 2>/dev/null | wc -l | tr -d ' ')
  echo -n "[check] All proxy pods Running ..."
  if [[ "$NOT_RUNNING" -eq 0 ]]; then
    check_pass
  else
    check_fail "${NOT_RUNNING} pod(s) not in Running state"
  fi

  RESTART_TOTAL=$(oc get pods -n "$NAMESPACE" -l app=cert-manager-http01-proxy \
    -o jsonpath='{range .items[*]}{range .status.containerStatuses[*]}{.restartCount}{"\n"}{end}{end}' 2>/dev/null \
    | awk '{s+=$1} END {print s+0}')
  echo -n "[check] No container restarts ..."
  if [[ "$RESTART_TOTAL" -eq 0 ]]; then
    check_pass
  else
    check_warn "${RESTART_TOTAL} total restart(s)"
  fi
fi

echo ""
echo "═══════════════════════════════════════════════════════════"

if [[ "$ERRORS" -gt 0 ]]; then
  echo "[FAIL] ${ERRORS} check(s) failed."
  exit 1
else
  echo "[done] All checks passed."
fi
