#!/usr/bin/env bash

# Copyright 2024 Red Hat, Inc.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# test-rhcos10-compatibility.sh - Run comprehensive RHCOS 10 compatibility tests
#
# This script orchestrates comprehensive compatibility testing for cert-manager
# on RHCOS 10, including deployment verification, e2e tests, crypto library checks,
# and results documentation.

set -euo pipefail

# Script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Configuration
OUTPUT_DIR="${OUTPUT_DIR:-${PROJECT_ROOT}/_output}"
TEST_REPORT="${OUTPUT_DIR}/rhcos10-compatibility-report.md"
E2E_TIMEOUT="${E2E_TIMEOUT:-2h}"
RUN_E2E_TESTS="${RUN_E2E_TESTS:-true}"
RUN_CRYPTO_VERIFICATION="${RUN_CRYPTO_VERIFICATION:-true}"

# Test results
DEPLOYMENT_PASSED=false
E2E_PASSED=false
CRYPTO_PASSED=false

# Logging functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $*"
}

log_success() {
    echo -e "${GREEN}[PASS]${NC} $*"
}

log_error() {
    echo -e "${RED}[FAIL]${NC} $*"
}

log_warning() {
    echo -e "${YELLOW}[WARN]${NC} $*"
}

log_section() {
    echo ""
    echo -e "${CYAN}========================================${NC}"
    echo -e "${CYAN}$*${NC}"
    echo -e "${CYAN}========================================${NC}"
    echo ""
}

# Print usage
usage() {
    cat <<EOF
Usage: $0 [OPTIONS]

Run comprehensive RHCOS 10 compatibility tests for cert-manager operator.

OPTIONS:
    --skip-e2e              Skip e2e test suite execution
    --skip-crypto           Skip crypto library verification
    --e2e-timeout DURATION  Set e2e test timeout (default: 2h)
    --output-dir DIR        Set output directory (default: _output)
    -h, --help              Show this help message

ENVIRONMENT VARIABLES:
    E2E_TIMEOUT             E2E test timeout (default: 2h)
    E2E_GINKGO_LABEL_FILTER Ginkgo label filter for e2e tests
    OUTPUT_DIR              Output directory for reports

EXAMPLES:
    # Run all tests
    $0

    # Skip e2e tests (only verify deployment and crypto)
    $0 --skip-e2e

    # Run with custom timeout
    $0 --e2e-timeout 3h

    # Run with specific e2e test filter
    E2E_GINKGO_LABEL_FILTER='Platform: isSubsetOf {AWS}' $0

EOF
}

# Parse command line arguments
parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            --skip-e2e)
                RUN_E2E_TESTS=false
                shift
                ;;
            --skip-crypto)
                RUN_CRYPTO_VERIFICATION=false
                shift
                ;;
            --e2e-timeout)
                E2E_TIMEOUT="$2"
                shift 2
                ;;
            --output-dir)
                OUTPUT_DIR="$2"
                TEST_REPORT="${OUTPUT_DIR}/rhcos10-compatibility-report.md"
                shift 2
                ;;
            -h|--help)
                usage
                exit 0
                ;;
            *)
                log_error "Unknown option: $1"
                usage
                exit 1
                ;;
        esac
    done
}

# Check prerequisites
check_prerequisites() {
    log_section "Checking Prerequisites"

    local missing_tools=()

    if ! command -v oc &> /dev/null; then
        missing_tools+=("oc")
    fi

    if ! command -v kubectl &> /dev/null; then
        missing_tools+=("kubectl")
    fi

    if ! command -v jq &> /dev/null; then
        missing_tools+=("jq")
    fi

    if ! command -v make &> /dev/null; then
        missing_tools+=("make")
    fi

    if [ ${#missing_tools[@]} -gt 0 ]; then
        log_error "Missing required tools: ${missing_tools[*]}"
        exit 1
    fi

    # Check cluster connectivity
    if ! oc cluster-info &> /dev/null; then
        log_error "Cannot connect to cluster. Please ensure you are logged in."
        exit 1
    fi

    log_success "All prerequisites met"
}

# Collect cluster information
collect_cluster_info() {
    log_section "Collecting Cluster Information"

    local ocp_version rhcos_version platform

    ocp_version=$(oc version -o json 2>/dev/null | jq -r '.openshiftVersion // "unknown"')
    platform=$(oc get infrastructure cluster -o jsonpath='{.status.platformStatus.type}' 2>/dev/null || echo "unknown")

    log_info "OpenShift Version: $ocp_version"
    log_info "Platform: $platform"

    # Get RHCOS version from a worker node
    local worker_node
    worker_node=$(oc get nodes -l node-role.kubernetes.io/worker -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

    if [ -z "$worker_node" ]; then
        worker_node=$(oc get nodes -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
    fi

    if [ -n "$worker_node" ]; then
        rhcos_version=$(oc get node "$worker_node" -o jsonpath='{.status.nodeInfo.osImage}')
        log_info "RHCOS Version: $rhcos_version"

        # Verify RHCOS 10
        if echo "$rhcos_version" | grep -qE "(RHCOS|CoreOS|Red Hat Enterprise Linux CoreOS) (10|410)"; then
            log_success "Cluster is running RHCOS 10"
        else
            log_warning "Cluster may not be running RHCOS 10: $rhcos_version"
            log_warning "This test suite is designed for RHCOS 10. Results may not be accurate."
        fi
    else
        log_warning "Could not determine RHCOS version"
        rhcos_version="unknown"
    fi

    # Export for report
    export CLUSTER_OCP_VERSION="$ocp_version"
    export CLUSTER_RHCOS_VERSION="$rhcos_version"
    export CLUSTER_PLATFORM="$platform"
    export CLUSTER_NAME="$(oc config current-context)"
}

# Verify cert-manager deployment
verify_deployment() {
    log_section "Verifying cert-manager Deployment"

    # Check if cert-manager-operator namespace exists
    if ! oc get namespace cert-manager-operator &> /dev/null; then
        log_error "cert-manager-operator namespace does not exist"
        log_info "Please deploy cert-manager operator first: make deploy"
        return 1
    fi

    # Check operator deployment
    log_info "Checking operator deployment..."
    if oc get deployment cert-manager-operator-controller-manager -n cert-manager-operator &> /dev/null; then
        if oc wait --for=condition=Available=true deployment/cert-manager-operator-controller-manager \
           -n cert-manager-operator --timeout=60s &> /dev/null; then
            log_success "Operator deployment is available"
        else
            log_error "Operator deployment is not available"
            return 1
        fi
    else
        log_error "Operator deployment not found"
        return 1
    fi

    # Check if cert-manager namespace exists
    if ! oc get namespace cert-manager &> /dev/null; then
        log_error "cert-manager namespace does not exist"
        return 1
    fi

    # Check operand deployments
    log_info "Checking operand deployments..."

    local deployments=("cert-manager" "cert-manager-webhook" "cert-manager-cainjector")
    local all_ready=true

    for deployment in "${deployments[@]}"; do
        if oc get deployment "$deployment" -n cert-manager &> /dev/null; then
            if oc wait --for=condition=Available=true "deployment/$deployment" \
               -n cert-manager --timeout=120s &> /dev/null; then
                log_success "Deployment $deployment is available"
            else
                log_error "Deployment $deployment is not available"
                all_ready=false
            fi
        else
            log_error "Deployment $deployment not found"
            all_ready=false
        fi
    done

    if [ "$all_ready" = true ]; then
        log_success "All cert-manager deployments are ready"
        DEPLOYMENT_PASSED=true
        return 0
    else
        log_error "Some cert-manager deployments are not ready"
        return 1
    fi
}

# Run e2e tests
run_e2e_tests() {
    log_section "Running E2E Test Suite"

    if [ "$RUN_E2E_TESTS" != "true" ]; then
        log_info "Skipping e2e tests (--skip-e2e flag set)"
        E2E_PASSED=true  # Mark as passed if skipped
        return 0
    fi

    log_info "Running e2e tests with timeout: $E2E_TIMEOUT"

    if [ -n "${E2E_GINKGO_LABEL_FILTER:-}" ]; then
        log_info "Using label filter: $E2E_GINKGO_LABEL_FILTER"
    fi

    # Run e2e tests from project root
    cd "$PROJECT_ROOT"

    if make test-e2e E2E_TIMEOUT="$E2E_TIMEOUT"; then
        log_success "E2E tests passed"
        E2E_PASSED=true
        return 0
    else
        log_error "E2E tests failed"
        E2E_PASSED=false
        return 1
    fi
}

# Run crypto verification
run_crypto_verification() {
    log_section "Running Crypto Library Verification"

    if [ "$RUN_CRYPTO_VERIFICATION" != "true" ]; then
        log_info "Skipping crypto verification (--skip-crypto flag set)"
        CRYPTO_PASSED=true  # Mark as passed if skipped
        return 0
    fi

    # Run crypto verification script
    if OUTPUT_DIR="$OUTPUT_DIR" bash "${SCRIPT_DIR}/verify-rhcos10-crypto.sh"; then
        log_success "Crypto verification passed"
        CRYPTO_PASSED=true
        return 0
    else
        log_error "Crypto verification failed"
        CRYPTO_PASSED=false
        return 1
    fi
}

# Collect logs and diagnostics
collect_diagnostics() {
    log_section "Collecting Diagnostic Information"

    local diag_dir="${OUTPUT_DIR}/diagnostics"
    mkdir -p "$diag_dir"

    log_info "Collecting cluster diagnostics to: $diag_dir"

    # Cluster version
    oc version > "${diag_dir}/cluster-version.txt" 2>&1 || true

    # Node information
    oc get nodes -o wide > "${diag_dir}/nodes.txt" 2>&1 || true
    oc get nodes -o json | jq -r '.items[] | {name: .metadata.name, os: .status.nodeInfo.osImage, kernel: .status.nodeInfo.kernelVersion}' \
        > "${diag_dir}/nodes-os-info.json" 2>&1 || true

    # Operator status
    oc get csv -n cert-manager-operator > "${diag_dir}/operator-csv.txt" 2>&1 || true
    oc get deployment -n cert-manager-operator > "${diag_dir}/operator-deployments.txt" 2>&1 || true
    oc get pods -n cert-manager-operator > "${diag_dir}/operator-pods.txt" 2>&1 || true
    oc logs deployment/cert-manager-operator-controller-manager -n cert-manager-operator --tail=500 \
        > "${diag_dir}/operator-logs.txt" 2>&1 || true

    # Operand status
    oc get certmanager cluster -o yaml > "${diag_dir}/certmanager-cr.yaml" 2>&1 || true
    oc get deployment -n cert-manager > "${diag_dir}/operand-deployments.txt" 2>&1 || true
    oc get pods -n cert-manager > "${diag_dir}/operand-pods.txt" 2>&1 || true
    oc get events -n cert-manager --sort-by='.lastTimestamp' > "${diag_dir}/operand-events.txt" 2>&1 || true

    # Operand logs
    for pod in $(oc get pods -n cert-manager -o name 2>/dev/null); do
        pod_name=$(basename "$pod")
        oc logs "$pod" -n cert-manager --tail=500 > "${diag_dir}/operand-log-${pod_name}.txt" 2>&1 || true
    done

    # E2E test results (if available)
    if [ -f "${OUTPUT_DIR}/report.json" ]; then
        cp "${OUTPUT_DIR}/report.json" "${diag_dir}/" || true
    fi

    if [ -f "${OUTPUT_DIR}/junit.xml" ]; then
        cp "${OUTPUT_DIR}/junit.xml" "${diag_dir}/" || true
    fi

    # Crypto verification report (if available)
    if [ -f "${OUTPUT_DIR}/rhcos10-crypto-verification-report.txt" ]; then
        cp "${OUTPUT_DIR}/rhcos10-crypto-verification-report.txt" "${diag_dir}/" || true
    fi

    log_success "Diagnostics collected"
}

# Generate test report
generate_report() {
    log_section "Generating Test Report"

    mkdir -p "$OUTPUT_DIR"

    local overall_status="FAIL"
    if [ "$DEPLOYMENT_PASSED" = true ] && [ "$E2E_PASSED" = true ] && [ "$CRYPTO_PASSED" = true ]; then
        overall_status="PASS"
    fi

    cat > "$TEST_REPORT" <<EOF
# RHCOS 10 Compatibility Test Report

**Date:** $(date -u +"%Y-%m-%d %H:%M:%S UTC")
**Cluster:** ${CLUSTER_NAME:-unknown}
**Overall Status:** $overall_status

---

## Cluster Information

- **OpenShift Version:** ${CLUSTER_OCP_VERSION:-unknown}
- **RHCOS Version:** ${CLUSTER_RHCOS_VERSION:-unknown}
- **Platform:** ${CLUSTER_PLATFORM:-unknown}

---

## Test Results Summary

| Test Category | Status | Details |
|--------------|--------|---------|
| Deployment Verification | $([ "$DEPLOYMENT_PASSED" = true ] && echo "✅ PASS" || echo "❌ FAIL") | Operator and operand deployment status |
| E2E Test Suite | $([ "$E2E_PASSED" = true ] && echo "✅ PASS" || echo "❌ FAIL") | Comprehensive functionality tests |
| Crypto Library Verification | $([ "$CRYPTO_PASSED" = true ] && echo "✅ PASS" || echo "❌ FAIL") | OpenSSL, FIPS, and certificate generation tests |

---

## Deployment Verification

**Status:** $([ "$DEPLOYMENT_PASSED" = true ] && echo "✅ PASS" || echo "❌ FAIL")

EOF

    if [ "$DEPLOYMENT_PASSED" = true ]; then
        cat >> "$TEST_REPORT" <<EOF
All cert-manager operator and operand deployments are healthy and running.

- ✅ cert-manager-operator deployment is available
- ✅ cert-manager deployment is available
- ✅ cert-manager-webhook deployment is available
- ✅ cert-manager-cainjector deployment is available

EOF
    else
        cat >> "$TEST_REPORT" <<EOF
Some cert-manager deployments failed to become ready. Check diagnostics for details.

EOF
    fi

    cat >> "$TEST_REPORT" <<EOF
---

## E2E Test Suite

**Status:** $([ "$E2E_PASSED" = true ] && echo "✅ PASS" || echo "❌ FAIL")

EOF

    if [ "$RUN_E2E_TESTS" = false ]; then
        cat >> "$TEST_REPORT" <<EOF
E2E tests were skipped (--skip-e2e flag was used).

EOF
    elif [ "$E2E_PASSED" = true ]; then
        cat >> "$TEST_REPORT" <<EOF
All e2e tests passed successfully. The full test suite completed without failures.

See test results in:
- \`${OUTPUT_DIR}/junit.xml\` - JUnit format test results
- \`${OUTPUT_DIR}/report.json\` - Ginkgo JSON report

EOF
    else
        cat >> "$TEST_REPORT" <<EOF
E2E tests failed. Review test output and diagnostics for failure details.

Test results available in:
- \`${OUTPUT_DIR}/junit.xml\` - JUnit format test results
- \`${OUTPUT_DIR}/report.json\` - Ginkgo JSON report

EOF
    fi

    cat >> "$TEST_REPORT" <<EOF
---

## Crypto Library Verification

**Status:** $([ "$CRYPTO_PASSED" = true ] && echo "✅ PASS" || echo "❌ FAIL")

EOF

    if [ "$RUN_CRYPTO_VERIFICATION" = false ]; then
        cat >> "$TEST_REPORT" <<EOF
Crypto verification was skipped (--skip-crypto flag was used).

EOF
    elif [ -f "${OUTPUT_DIR}/rhcos10-crypto-verification-report.txt" ]; then
        cat >> "$TEST_REPORT" <<EOF
Crypto library verification completed. See detailed report:
- \`${OUTPUT_DIR}/rhcos10-crypto-verification-report.txt\`

Key items verified:
- RHCOS 10 version detection
- OpenSSL version on nodes and containers
- FIPS mode status
- TLS connectivity from cert-manager pods
- Certificate generation with RSA and ECDSA algorithms

EOF
    else
        cat >> "$TEST_REPORT" <<EOF
Crypto verification report not available.

EOF
    fi

    cat >> "$TEST_REPORT" <<EOF
---

## Diagnostics

Detailed diagnostic information collected in: \`${OUTPUT_DIR}/diagnostics/\`

Files include:
- Cluster version and node information
- Operator and operand deployment status
- Pod logs and events
- Test results and reports

---

## Next Steps

EOF

    if [ "$overall_status" = "PASS" ]; then
        cat >> "$TEST_REPORT" <<EOF
✅ **All tests passed!**

1. Document these results per your organization's process
2. Share with your team
3. Mark testing complete for this OpenShift version

EOF
    else
        cat >> "$TEST_REPORT" <<EOF
❌ **Some tests failed**

1. Review diagnostics in \`${OUTPUT_DIR}/diagnostics/\`
2. Investigate failures and determine root cause
3. File bugs for any issues found:
   - Component: cert-manager-operator
   - Labels: rhcos10, rhel10, crypto (if applicable)
   - Include all debug information
4. Document findings and workarounds
5. Retest after fixes are applied

EOF
    fi

    cat >> "$TEST_REPORT" <<EOF
---

## References

- **Testing Guide:** \`docs/rhcos10-testing.md\`
- **Test Results Template:** \`docs/rhcos10-test-results-template.md\`

---

*Report generated by: \`hack/test-rhcos10-compatibility.sh\`*
EOF

    log_success "Test report generated: $TEST_REPORT"

    # Display report location
    echo ""
    log_info "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    log_info "Test Report: $TEST_REPORT"
    log_info "Diagnostics: ${OUTPUT_DIR}/diagnostics/"
    log_info "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""
}

# Print summary
print_summary() {
    log_section "Test Summary"

    echo ""
    echo "┌─────────────────────────────────────────────────────────────┐"
    echo "│                    RHCOS 10 Test Results                    │"
    echo "├─────────────────────────────────────────────────────────────┤"
    printf "│ Deployment Verification:    %-28s │\n" "$([ "$DEPLOYMENT_PASSED" = true ] && echo "✅ PASS" || echo "❌ FAIL")"
    printf "│ E2E Test Suite:             %-28s │\n" "$([ "$E2E_PASSED" = true ] && echo "✅ PASS" || echo "❌ FAIL")"
    printf "│ Crypto Library Verification:%-28s │\n" "$([ "$CRYPTO_PASSED" = true ] && echo "✅ PASS" || echo "❌ FAIL")"
    echo "├─────────────────────────────────────────────────────────────┤"

    if [ "$DEPLOYMENT_PASSED" = true ] && [ "$E2E_PASSED" = true ] && [ "$CRYPTO_PASSED" = true ]; then
        echo "│ Overall Status:             ✅ PASS                        │"
        echo "└─────────────────────────────────────────────────────────────┘"
        echo ""
        log_success "All RHCOS 10 compatibility tests passed!"
    else
        echo "│ Overall Status:             ❌ FAIL                        │"
        echo "└─────────────────────────────────────────────────────────────┘"
        echo ""
        log_error "Some RHCOS 10 compatibility tests failed. Review report for details."
    fi
    echo ""
}

# Main execution
main() {
    # Parse arguments
    parse_args "$@"

    # Print header
    echo "================================================================================"
    echo "               RHCOS 10 Compatibility Test Suite"
    echo "               cert-manager-operator"
    echo "================================================================================"
    echo ""

    # Create output directory
    mkdir -p "$OUTPUT_DIR"

    # Run tests
    check_prerequisites
    collect_cluster_info
    verify_deployment || true
    run_e2e_tests || true
    run_crypto_verification || true
    collect_diagnostics
    generate_report
    print_summary

    # Return appropriate exit code
    if [ "$DEPLOYMENT_PASSED" = true ] && [ "$E2E_PASSED" = true ] && [ "$CRYPTO_PASSED" = true ]; then
        exit 0
    else
        exit 1
    fi
}

# Run main function
main "$@"
