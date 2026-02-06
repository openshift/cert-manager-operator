#!/usr/bin/env bash

set -o nounset
set -o pipefail
set -o errexit

# Script to run govulncheck and filter known vulnerabilities.
# Usage: ./hack/govulncheck.sh <govulncheck_binary> <output_dir>
#
# Arguments:
#   govulncheck_binary: Path to the govulncheck binary
#   output_dir: Directory to store results
#
# Environment variables:
#   ARTIFACT_DIR: If set and directory exists, results are copied there for CI artifact collection

# Known vulnerabilities to ignore (in vendored packages, not operator code).
# Each vulnerability ID has been reviewed and deemed acceptable.
#
## Below vulnerabilities are in the kubernetes package, which impacts the server and not the operator, which is the client.
# - https://pkg.go.dev/vuln/GO-2025-3547 - Kubernetes kube-apiserver Vulnerable to Race Condition in k8s.io/kubernetes
# - https://pkg.go.dev/vuln/GO-2025-3521 - Kubernetes GitRepo Volume Inadvertent Local Repository Access in k8s.io/kubernetes
# - https://pkg.go.dev/vuln/GO-2025-4240 - Half-blind Server Side Request Forgery in kube-controller-manager through in-tree Portworx StorageClass in k8s.io/kubernetes
#
## Below vulnerabilities are in the go packages, which impacts the operator code and fixed in 1.25.6, but is not available downstream yet.
# - https://pkg.go.dev/vuln/GO-2026-4341 - Memory exhaustion in query parameter parsing in net/url
# - https://pkg.go.dev/vuln/GO-2026-4340 - Handshake messages may be processed at the incorrect encryption level in crypto/tls
# - https://pkg.go.dev/vuln/GO-2025-4175 - Improper application of excluded DNS name constraints when verifying wildcard names in crypto/x509
# - https://pkg.go.dev/vuln/GO-2025-4155 - Excessive resource consumption when printing error string for host certificate validation in crypto/x509
KNOWN_VULNS_PATTERN="GO-2025-3547|GO-2025-3521|GO-2025-4240|GO-2026-4341|GO-2026-4340|GO-2025-4175|GO-2025-4155"

GOVULNCHECK_BIN="${1:-}"
OUTPUT_DIR="${2:-}"

if [[ -z "${GOVULNCHECK_BIN}" ]] || [[ -z "${OUTPUT_DIR}" ]]; then
    echo "Usage: $0 <govulncheck_binary> <output_dir>"
    exit 1
fi

RESULTS_FILE="${OUTPUT_DIR}/govulncheck.results"

echo "Running govulncheck vulnerability scan..."
mkdir -p "${OUTPUT_DIR}"

# Run govulncheck and capture output (don't fail on vulnerabilities found)
"${GOVULNCHECK_BIN}" ./... > "${RESULTS_FILE}" 2>&1 || true

# Copy results to ARTIFACT_DIR if in CI environment
if [[ -n "${ARTIFACT_DIR:-}" ]] && [[ -d "${ARTIFACT_DIR}" ]]; then
    cp "${RESULTS_FILE}" "${ARTIFACT_DIR}/"
    echo "Results copied to ${ARTIFACT_DIR}/govulncheck.results"
fi

# Verify govulncheck actually ran successfully
if ! grep -q "pkg.go.dev" "${RESULTS_FILE}"; then
    echo ""
    echo "-- ERROR -- govulncheck may have failed to run"
    echo "Please review ${RESULTS_FILE} for details"
    echo ""
    cat "${RESULTS_FILE}"
    exit 1
fi

echo "Filtering known vulnerabilities and counting new ones..."

# Find new vulnerabilities (not in known list)
if [[ -n "${KNOWN_VULNS_PATTERN}" ]]; then
    new_vulns=$(grep "pkg.go.dev" "${RESULTS_FILE}" | grep -Ev "${KNOWN_VULNS_PATTERN}" || true)
else
    new_vulns=$(grep "pkg.go.dev" "${RESULTS_FILE}" || true)
fi

if [[ -n "${new_vulns}" ]]; then
    reported=$(echo "${new_vulns}" | wc -l)
    echo ""
    echo "-- ERROR -- ${reported} new vulnerabilities reported:"
    echo "${new_vulns}"
    echo ""
    echo "Please review ${RESULTS_FILE} for details"
    echo "To ignore these vulnerabilities, add them to KNOWN_VULNS_PATTERN with valid justification"
    echo ""
    exit 1
else
    echo "✓ Vulnerability scan passed - no new issues found"
fi
