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

# verify-rhcos10-crypto.sh - Verify RHCOS 10 crypto library compatibility
#
# This script verifies that cert-manager is compatible with RHCOS 10 crypto libraries
# by checking OpenSSL versions, FIPS mode status, TLS connectivity, and certificate
# generation capabilities.

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Counters
CHECKS_PASSED=0
CHECKS_FAILED=0
CHECKS_WARNING=0

# Output directory
OUTPUT_DIR="${OUTPUT_DIR:-_output}"
REPORT_FILE="${OUTPUT_DIR}/rhcos10-crypto-verification-report.txt"

# Logging functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $*"
}

log_success() {
    echo -e "${GREEN}[PASS]${NC} $*"
    ((++CHECKS_PASSED))
}

log_error() {
    echo -e "${RED}[FAIL]${NC} $*"
    ((++CHECKS_FAILED))
}

log_warning() {
    echo -e "${YELLOW}[WARN]${NC} $*"
    ((++CHECKS_WARNING))
}

# Initialize report
init_report() {
    mkdir -p "${OUTPUT_DIR}"
    cat > "${REPORT_FILE}" <<EOF
================================================================================
RHCOS 10 Crypto Library Verification Report
================================================================================
Date: $(date -u +"%Y-%m-%d %H:%M:%S UTC")
Cluster: $(oc config current-context 2>/dev/null || echo "unknown")

EOF
}

# Add to report
add_to_report() {
    echo "$*" >> "${REPORT_FILE}"
}

# Check prerequisites
check_prerequisites() {
    log_info "Checking prerequisites..."

    local missing_tools=()

    if ! command -v oc &> /dev/null; then
        missing_tools+=("oc")
    fi

    if ! command -v jq &> /dev/null; then
        missing_tools+=("jq")
    fi

    if ! command -v openssl &> /dev/null; then
        missing_tools+=("openssl")
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

# Check RHCOS version
check_rhcos_version() {
    log_info "Checking RHCOS version on cluster nodes..."
    add_to_report ""
    add_to_report "================================================================================
Node OS Versions
================================================================================"

    local nodes_json
    nodes_json=$(oc get nodes -o json)

    local rhcos10_nodes=0
    local total_nodes=0

    while IFS= read -r node; do
        ((++total_nodes))
        local name os_image kernel
        name=$(echo "$node" | jq -r '.name')
        os_image=$(echo "$node" | jq -r '.os')
        kernel=$(echo "$node" | jq -r '.kernel')

        add_to_report "Node: $name"
        add_to_report "  OS Image: $os_image"
        add_to_report "  Kernel: $kernel"

        # Check if RHCOS 10 or RHEL 10
        if echo "$os_image" | grep -qE "(RHCOS|CoreOS|Red Hat Enterprise Linux CoreOS) (10|410)"; then
            ((++rhcos10_nodes))
            log_success "Node $name is running RHCOS 10"
        else
            log_warning "Node $name may not be running RHCOS 10: $os_image"
        fi
    done < <(echo "$nodes_json" | jq -c '.items[] | {name: .metadata.name, os: .status.nodeInfo.osImage, kernel: .status.nodeInfo.kernelVersion}')

    add_to_report ""
    add_to_report "Total nodes: $total_nodes"
    add_to_report "RHCOS 10 nodes: $rhcos10_nodes"

    if [ "$rhcos10_nodes" -eq "$total_nodes" ]; then
        log_success "All $total_nodes nodes are running RHCOS 10"
    elif [ "$rhcos10_nodes" -gt 0 ]; then
        log_warning "Only $rhcos10_nodes of $total_nodes nodes are running RHCOS 10"
    else
        log_error "No nodes detected as running RHCOS 10"
    fi
}

# Check OpenSSL version on nodes
check_node_openssl() {
    log_info "Checking OpenSSL version on nodes..."
    add_to_report ""
    add_to_report "================================================================================"
    add_to_report "Node OpenSSL Versions"
    add_to_report "================================================================================"

    local worker_node
    worker_node=$(oc get nodes -l node-role.kubernetes.io/worker -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

    if [ -z "$worker_node" ]; then
        # Try master node if no worker
        worker_node=$(oc get nodes -l node-role.kubernetes.io/master -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
    fi

    if [ -z "$worker_node" ]; then
        log_error "No nodes found to check OpenSSL version"
        return
    fi

    log_info "Checking OpenSSL on node: $worker_node"

    local openssl_output
    if openssl_output=$(oc debug "node/$worker_node" -- chroot /host openssl version -a 2>&1); then
        add_to_report "Node: $worker_node"
        add_to_report "$openssl_output"

        # Extract version
        local version
        version=$(echo "$openssl_output" | grep -E "^OpenSSL" | awk '{print $2}')

        if [ -n "$version" ]; then
            log_success "OpenSSL version on node: $version"

            # Check for OpenSSL 3.x (expected in RHCOS 10 / RHEL 10)
            if echo "$version" | grep -qE "^3\."; then
                log_success "OpenSSL 3.x detected (expected for RHCOS 10)"
            else
                log_warning "OpenSSL version is not 3.x: $version"
            fi
        fi
    else
        log_error "Failed to check OpenSSL version on node"
        add_to_report "Error: Failed to retrieve OpenSSL version"
    fi
}

# Check FIPS mode
check_fips_mode() {
    log_info "Checking FIPS mode status..."
    add_to_report ""
    add_to_report "================================================================================"
    add_to_report "FIPS Mode Status"
    add_to_report "================================================================================"

    local worker_node
    worker_node=$(oc get nodes -l node-role.kubernetes.io/worker -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

    if [ -z "$worker_node" ]; then
        worker_node=$(oc get nodes -l node-role.kubernetes.io/master -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
    fi

    if [ -z "$worker_node" ]; then
        log_error "No nodes found to check FIPS mode"
        return
    fi

    local fips_status
    if fips_status=$(oc debug "node/$worker_node" -- chroot /host cat /proc/sys/crypto/fips_enabled 2>&1); then
        add_to_report "Node: $worker_node"
        add_to_report "FIPS enabled: $fips_status"

        if [ "$fips_status" = "1" ]; then
            log_info "FIPS mode is ENABLED on node"
        else
            log_info "FIPS mode is DISABLED on node"
        fi
    else
        log_warning "Could not determine FIPS mode status"
        add_to_report "Error: Could not retrieve FIPS status"
    fi
}

# Check cert-manager pods
check_certmanager_pods() {
    log_info "Checking cert-manager pod status..."
    add_to_report ""
    add_to_report "================================================================================"
    add_to_report "Cert-Manager Pod Status"
    add_to_report "================================================================================"

    if ! oc get namespace cert-manager &> /dev/null; then
        log_error "cert-manager namespace does not exist"
        return
    fi

    local pods
    pods=$(oc get pods -n cert-manager -o json)

    local pod_count
    pod_count=$(echo "$pods" | jq '.items | length')

    if [ "$pod_count" -eq 0 ]; then
        log_error "No cert-manager pods found"
        return
    fi

    log_info "Found $pod_count cert-manager pods"

    local running_pods=0
    while IFS= read -r pod_name; do
        local status
        status=$(oc get pod "$pod_name" -n cert-manager -o jsonpath='{.status.phase}')

        add_to_report "Pod: $pod_name - Status: $status"

        if [ "$status" = "Running" ]; then
            ((++running_pods))
            log_success "Pod $pod_name is running"
        else
            log_error "Pod $pod_name is not running: $status"
        fi
    done < <(echo "$pods" | jq -r '.items[].metadata.name')

    if [ "$running_pods" -eq "$pod_count" ]; then
        log_success "All $pod_count cert-manager pods are running"
    else
        log_error "Only $running_pods of $pod_count pods are running"
    fi
}

# Check OpenSSL in cert-manager containers
check_container_openssl() {
    log_info "Checking OpenSSL version in cert-manager containers..."
    add_to_report ""
    add_to_report "================================================================================"
    add_to_report "Container OpenSSL Versions"
    add_to_report "================================================================================"

    if ! oc get namespace cert-manager &> /dev/null; then
        log_error "cert-manager namespace does not exist"
        return
    fi

    # Check controller pod
    local controller_pod
    controller_pod=$(oc get pods -n cert-manager -l app=cert-manager,app.kubernetes.io/component=controller -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

    if [ -z "$controller_pod" ]; then
        log_warning "cert-manager controller pod not found"
        return
    fi

    log_info "Checking OpenSSL in controller pod: $controller_pod"

    local openssl_version
    if openssl_version=$(oc exec -n cert-manager "$controller_pod" -- openssl version -a 2>&1); then
        add_to_report "Controller Pod: $controller_pod"
        add_to_report "$openssl_version"

        local version
        version=$(echo "$openssl_version" | grep -E "^OpenSSL" | awk '{print $2}')
        log_success "Container OpenSSL version: $version"
    else
        log_warning "Could not retrieve OpenSSL version from container (may not have openssl binary)"
        add_to_report "Note: OpenSSL binary may not be available in container"
    fi
}

# Test TLS connectivity from cert-manager
check_tls_connectivity() {
    log_info "Testing TLS connectivity from cert-manager controller..."
    add_to_report ""
    add_to_report "================================================================================"
    add_to_report "TLS Connectivity Tests"
    add_to_report "================================================================================"

    if ! oc get namespace cert-manager &> /dev/null; then
        log_error "cert-manager namespace does not exist"
        return
    fi

    local controller_pod
    controller_pod=$(oc get pods -n cert-manager -l app=cert-manager,app.kubernetes.io/component=controller -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

    if [ -z "$controller_pod" ]; then
        log_error "cert-manager controller pod not found"
        return
    fi

    log_info "Testing TLS connection to Kubernetes API from pod: $controller_pod"

    local tls_output
    if tls_output=$(oc exec -n cert-manager "$controller_pod" -- curl -v -k https://kubernetes.default.svc 2>&1 | head -30); then
        add_to_report "TLS connection test to kubernetes.default.svc:"
        add_to_report "$tls_output"

        if echo "$tls_output" | grep -q "SSL connection using"; then
            local cipher
            cipher=$(echo "$tls_output" | grep "SSL connection using" || echo "")
            log_success "TLS connection successful: $cipher"
        else
            log_warning "TLS connection test completed but cipher information not found"
        fi
    else
        log_error "TLS connectivity test failed"
    fi
}

# Check for crypto-related errors in logs
check_crypto_errors_in_logs() {
    log_info "Checking cert-manager logs for crypto-related errors..."
    add_to_report ""
    add_to_report "================================================================================"
    add_to_report "Crypto-Related Errors in Logs"
    add_to_report "================================================================================"

    if ! oc get namespace cert-manager &> /dev/null; then
        log_error "cert-manager namespace does not exist"
        return
    fi

    local error_found=false

    while IFS= read -r pod_name; do
        log_info "Checking logs for pod: $pod_name"

        local logs
        if logs=$(oc logs "$pod_name" -n cert-manager --tail=500 2>&1 | grep -iE "(crypto|ssl|tls|fips|cipher|certificate.*error|x509.*error)" || true); then
            if [ -n "$logs" ]; then
                add_to_report "Pod: $pod_name"
                add_to_report "$logs"
                log_warning "Found crypto-related messages in $pod_name logs (review needed)"
                error_found=true
            fi
        fi
    done < <(oc get pods -n cert-manager -o jsonpath='{.items[*].metadata.name}')

    if [ "$error_found" = false ]; then
        log_success "No crypto-related errors found in logs"
        add_to_report "No crypto-related errors found"
    fi
}

# Test certificate generation with different algorithms
test_certificate_generation() {
    log_info "Testing certificate generation with different algorithms..."
    add_to_report ""
    add_to_report "================================================================================"
    add_to_report "Certificate Generation Tests"
    add_to_report "================================================================================"

    local test_ns="cert-manager-rhcos10-test"

    # Create test namespace
    if ! oc get namespace "$test_ns" &> /dev/null; then
        oc create namespace "$test_ns" > /dev/null 2>&1 || true
    fi

    # Cleanup function
    cleanup_test_resources() {
        log_info "Cleaning up test resources..."
        oc delete certificate --all -n "$test_ns" &> /dev/null || true
        oc delete issuer --all -n "$test_ns" &> /dev/null || true
        oc delete namespace "$test_ns" &> /dev/null || true
    }

    # Ensure cleanup on exit
    trap cleanup_test_resources EXIT

    # Create self-signed issuer
    log_info "Creating self-signed issuer..."
    oc apply -f - > /dev/null 2>&1 <<EOF
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: test-selfsigned
  namespace: $test_ns
spec:
  selfSigned: {}
EOF

    # Wait for issuer to be ready
    sleep 2

    # Test RSA 2048
    log_info "Testing RSA 2048 certificate generation..."
    oc apply -f - > /dev/null 2>&1 <<EOF
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: test-rsa-2048
  namespace: $test_ns
spec:
  secretName: test-rsa-2048-tls
  duration: 2160h
  renewBefore: 360h
  isCA: false
  privateKey:
    algorithm: RSA
    size: 2048
  usages:
    - server auth
  dnsNames:
  - test-rsa-2048.example.com
  issuerRef:
    name: test-selfsigned
    kind: Issuer
EOF

    if oc wait --for=condition=Ready certificate/test-rsa-2048 -n "$test_ns" --timeout=60s &> /dev/null; then
        log_success "RSA 2048 certificate generated successfully"
        add_to_report "✓ RSA 2048: SUCCESS"
    else
        log_error "Failed to generate RSA 2048 certificate"
        add_to_report "✗ RSA 2048: FAILED"
    fi

    # Test RSA 4096
    log_info "Testing RSA 4096 certificate generation..."
    oc apply -f - > /dev/null 2>&1 <<EOF
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: test-rsa-4096
  namespace: $test_ns
spec:
  secretName: test-rsa-4096-tls
  duration: 2160h
  renewBefore: 360h
  isCA: false
  privateKey:
    algorithm: RSA
    size: 4096
  usages:
    - server auth
  dnsNames:
  - test-rsa-4096.example.com
  issuerRef:
    name: test-selfsigned
    kind: Issuer
EOF

    if oc wait --for=condition=Ready certificate/test-rsa-4096 -n "$test_ns" --timeout=60s &> /dev/null; then
        log_success "RSA 4096 certificate generated successfully"
        add_to_report "✓ RSA 4096: SUCCESS"
    else
        log_error "Failed to generate RSA 4096 certificate"
        add_to_report "✗ RSA 4096: FAILED"
    fi

    # Test ECDSA P-256
    log_info "Testing ECDSA P-256 certificate generation..."
    oc apply -f - > /dev/null 2>&1 <<EOF
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: test-ecdsa-256
  namespace: $test_ns
spec:
  secretName: test-ecdsa-256-tls
  duration: 2160h
  renewBefore: 360h
  isCA: false
  privateKey:
    algorithm: ECDSA
    size: 256
  usages:
    - server auth
  dnsNames:
  - test-ecdsa-256.example.com
  issuerRef:
    name: test-selfsigned
    kind: Issuer
EOF

    if oc wait --for=condition=Ready certificate/test-ecdsa-256 -n "$test_ns" --timeout=60s &> /dev/null; then
        log_success "ECDSA P-256 certificate generated successfully"
        add_to_report "✓ ECDSA P-256: SUCCESS"
    else
        log_error "Failed to generate ECDSA P-256 certificate"
        add_to_report "✗ ECDSA P-256: FAILED"
    fi

    # Test ECDSA P-384
    log_info "Testing ECDSA P-384 certificate generation..."
    oc apply -f - > /dev/null 2>&1 <<EOF
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: test-ecdsa-384
  namespace: $test_ns
spec:
  secretName: test-ecdsa-384-tls
  duration: 2160h
  renewBefore: 360h
  isCA: false
  privateKey:
    algorithm: ECDSA
    size: 384
  usages:
    - server auth
  dnsNames:
  - test-ecdsa-384.example.com
  issuerRef:
    name: test-selfsigned
    kind: Issuer
EOF

    if oc wait --for=condition=Ready certificate/test-ecdsa-384 -n "$test_ns" --timeout=60s &> /dev/null; then
        log_success "ECDSA P-384 certificate generated successfully"
        add_to_report "✓ ECDSA P-384: SUCCESS"
    else
        log_error "Failed to generate ECDSA P-384 certificate"
        add_to_report "✗ ECDSA P-384: FAILED"
    fi
}

# Generate summary
generate_summary() {
    log_info "Generating verification summary..."
    add_to_report ""
    add_to_report "================================================================================"
    add_to_report "Summary"
    add_to_report "================================================================================"
    add_to_report "Checks Passed: $CHECKS_PASSED"
    add_to_report "Checks Failed: $CHECKS_FAILED"
    add_to_report "Checks Warning: $CHECKS_WARNING"
    add_to_report ""

    local status
    if [ "$CHECKS_FAILED" -eq 0 ]; then
        status="PASS"
        add_to_report "Overall Status: ✓ PASS"
        log_success "Overall verification: PASS"
    else
        status="FAIL"
        add_to_report "Overall Status: ✗ FAIL"
        log_error "Overall verification: FAIL"
    fi

    if [ "$CHECKS_WARNING" -gt 0 ]; then
        add_to_report "Warnings: Review recommended"
        log_warning "$CHECKS_WARNING warnings require review"
    fi

    add_to_report ""
    add_to_report "Report saved to: $REPORT_FILE"
    add_to_report "================================================================================"

    echo ""
    log_info "Detailed report saved to: $REPORT_FILE"
    echo ""

    return $([ "$status" = "PASS" ] && echo 0 || echo 1)
}

# Main execution
main() {
    echo "================================================================================"
    echo "RHCOS 10 Crypto Library Verification"
    echo "================================================================================"
    echo ""

    init_report
    check_prerequisites
    check_rhcos_version
    check_node_openssl
    check_fips_mode
    check_certmanager_pods
    check_container_openssl
    check_tls_connectivity
    check_crypto_errors_in_logs
    test_certificate_generation

    echo ""
    generate_summary
}

# Run main function
main "$@"
