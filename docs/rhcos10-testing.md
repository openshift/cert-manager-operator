# RHCOS 10 / RHEL 10 Compatibility Testing Guide

This guide provides comprehensive instructions for testing cert-manager operator compatibility with RHCOS 10 and RHEL 10 on OpenShift 4.20, 4.21, and 4.22.

## Overview

As part of the RHCOS10/RHEL10 readiness effort for OpenShift, the cert-manager operator must be verified to work correctly with:
- Red Hat CoreOS (RHCOS) 10
- Red Hat Enterprise Linux (RHEL) 10
- Updated crypto libraries and dependencies
- OpenShift Container Platform versions 4.20, 4.21, and 4.22

## Prerequisites

### Required Access
- OpenShift cluster running RHCOS 10 (versions 4.20, 4.21, or 4.22)
- Cluster administrator privileges

### Required Tools
- `oc` CLI (OpenShift command-line tool)
- `kubectl` CLI
- `make` (for running automation targets)
- `bash` shell (version 4.0 or higher)
- `jq` (for JSON processing)
- `curl` (for API testing)

### Verification of RHCOS 10 Environment

Before beginning testing, verify the cluster is running RHCOS 10:

```bash
# Check RHCOS version on all nodes
oc get nodes -o json | jq -r '.items[] | {name: .metadata.name, os: .status.nodeInfo.osImage, kernel: .status.nodeInfo.kernelVersion}'

# Expected output should show RHCOS 10.x
# Example: "Red Hat Enterprise Linux CoreOS 410.92.202X... (CoreOS)"
```

Check OpenShift version:

```bash
oc version
# Should show 4.20.x, 4.21.x, or 4.22.x
```

## Testing Scope

### 1. Deployment Testing
Verify cert-manager operator and operands deploy successfully on RHCOS 10.

### 2. Functionality Testing
Run comprehensive e2e tests to ensure all cert-manager features work correctly.

### 3. Crypto Library Testing
Verify compatibility with RHCOS 10 crypto libraries, including:
- OpenSSL version and FIPS mode
- TLS connectivity
- Certificate generation with various algorithms (RSA, ECDSA)
- Certificate validation and verification

### 4. Integration Testing
Verify cert-manager integrates correctly with:
- OpenShift API server
- OpenShift routing
- Cloud provider APIs (AWS Route53, GCP Cloud DNS, Azure DNS)
- Vault (if applicable)

## Step-by-Step Testing Procedures

### Phase 1: Pre-Deployment Verification

#### 1.1 Verify Cluster Prerequisites

```bash
# Check cluster operators are healthy
oc get co

# Verify no degraded operators
oc get co | grep -i degraded

# Check cluster version
oc get clusterversion
```

#### 1.2 Verify RHCOS 10 Node Status

```bash
# Check all nodes are ready
oc get nodes

# Verify RHCOS version on each node
for node in $(oc get nodes -o name); do
  echo "=== $node ==="
  oc debug $node -- chroot /host sh -c "cat /etc/os-release | grep -E '(PRETTY_NAME|VERSION_ID)'"
done
```

#### 1.3 Check Crypto Libraries

Run the automated crypto verification script:

```bash
make verify-rhcos10-crypto
```

Or manually check on a node:

```bash
# Choose a worker node
NODE=$(oc get nodes -l node-role.kubernetes.io/worker -o jsonpath='{.items[0].metadata.name}')

# Check OpenSSL version
oc debug node/$NODE -- chroot /host openssl version -a

# Check FIPS mode status
oc debug node/$NODE -- chroot /host cat /proc/sys/crypto/fips_enabled
```

### Phase 2: Deployment Testing

#### 2.1 Deploy cert-manager Operator

If using OLM (production method):

```bash
# Create operator namespace
oc create namespace cert-manager-operator

# Create subscription (adjust based on your catalog source)
cat <<EOF | oc apply -f -
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: openshift-cert-manager-operator
  namespace: cert-manager-operator
spec:
  channel: stable-v1
  name: openshift-cert-manager-operator
  source: redhat-operators
  sourceNamespace: openshift-marketplace
EOF
```

If using manifests (development method):

```bash
# Deploy from local manifests
make deploy
```

#### 2.2 Verify Operator Deployment

```bash
# Wait for operator to be ready
oc wait --for=condition=Available=true deployment/cert-manager-operator-controller-manager -n cert-manager-operator --timeout=300s

# Check operator logs for errors
oc logs deployment/cert-manager-operator-controller-manager -n cert-manager-operator --tail=50

# Verify CertManager CR is created
oc get certmanager cluster -o yaml
```

#### 2.3 Verify Operand Deployment

```bash
# Wait for all operands to be ready
make test-e2e-wait-for-stable-state

# Alternatively, check manually
oc wait --for=condition=Available=true deployment/cert-manager -n cert-manager --timeout=120s
oc wait --for=condition=Available=true deployment/cert-manager-webhook -n cert-manager --timeout=120s
oc wait --for=condition=Available=true deployment/cert-manager-cainjector -n cert-manager --timeout=120s

# Verify all pods are running
oc get pods -n cert-manager
```

#### 2.4 Check for RHCOS 10 Specific Issues

```bash
# Check pod logs for crypto-related errors
for pod in $(oc get pods -n cert-manager -o name); do
  echo "=== Checking $pod ==="
  oc logs $pod -n cert-manager | grep -iE "(error|fail|crypto|ssl|tls|fips)" || echo "No issues found"
done

# Check pod events
oc get events -n cert-manager --sort-by='.lastTimestamp'
```

### Phase 3: Functionality Testing

#### 3.1 Run E2E Test Suite

Run the full e2e test suite:

```bash
# Run all e2e tests (2 hour timeout)
make test-e2e

# Or run specific test categories
E2E_GINKGO_LABEL_FILTER='Platform: isSubsetOf {AWS}' make test-e2e
```

#### 3.2 Run RHCOS 10 Compatibility Test Suite

Use the automated compatibility test script:

```bash
make test-rhcos10
```

This will:
- Verify RHCOS 10 version
- Run full e2e test suite
- Execute crypto library verification
- Collect logs and metrics
- Generate test report

#### 3.3 Manual Functionality Tests

Create test resources to verify basic functionality:

```bash
# Create a self-signed issuer
cat <<EOF | oc apply -f -
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  name: test-selfsigned
  namespace: default
spec:
  selfSigned: {}
EOF

# Create a test certificate
cat <<EOF | oc apply -f -
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: test-cert-rhcos10
  namespace: default
spec:
  secretName: test-cert-rhcos10-tls
  duration: 2160h # 90d
  renewBefore: 360h # 15d
  subject:
    organizations:
    - red-hat
  isCA: false
  privateKey:
    algorithm: RSA
    encoding: PKCS1
    size: 2048
  usages:
    - server auth
    - client auth
  dnsNames:
  - test.example.com
  issuerRef:
    name: test-selfsigned
    kind: Issuer
EOF

# Wait for certificate to be ready
oc wait --for=condition=Ready certificate/test-cert-rhcos10 -n default --timeout=60s

# Verify certificate was created
oc get certificate test-cert-rhcos10 -n default
oc get secret test-cert-rhcos10-tls -n default

# Test certificate with different algorithms
# ECDSA P-256
cat <<EOF | oc apply -f -
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: test-cert-ecdsa-rhcos10
  namespace: default
spec:
  secretName: test-cert-ecdsa-rhcos10-tls
  duration: 2160h
  renewBefore: 360h
  isCA: false
  privateKey:
    algorithm: ECDSA
    size: 256
  usages:
    - server auth
  dnsNames:
  - test-ecdsa.example.com
  issuerRef:
    name: test-selfsigned
    kind: Issuer
EOF

oc wait --for=condition=Ready certificate/test-cert-ecdsa-rhcos10 -n default --timeout=60s
```

### Phase 4: Crypto Library Verification

#### 4.1 Automated Crypto Verification

```bash
make verify-rhcos10-crypto
```

#### 4.2 Manual Crypto Verification

Verify TLS connectivity from cert-manager pods:

```bash
# Get cert-manager controller pod
CONTROLLER_POD=$(oc get pods -n cert-manager -l app=cert-manager,app.kubernetes.io/component=controller -o jsonpath='{.items[0].metadata.name}')

# Test TLS connection to Kubernetes API
oc exec -n cert-manager $CONTROLLER_POD -- curl -v https://kubernetes.default.svc 2>&1 | grep -E "(SSL|TLS|cipher)"

# Check OpenSSL version in container
oc exec -n cert-manager $CONTROLLER_POD -- openssl version -a
```

Verify certificate generation with different algorithms:

```bash
# Extract and verify RSA certificate
oc get secret test-cert-rhcos10-tls -n default -o jsonpath='{.data.tls\.crt}' | base64 -d | openssl x509 -text -noout | grep -A 2 "Public Key Algorithm"

# Extract and verify ECDSA certificate
oc get secret test-cert-ecdsa-rhcos10-tls -n default -o jsonpath='{.data.tls\.crt}' | base64 -d | openssl x509 -text -noout | grep -A 2 "Public Key Algorithm"
```

#### 4.3 FIPS Mode Testing

If cluster is in FIPS mode:

```bash
# Verify FIPS mode is enabled on nodes
oc debug node/$NODE -- chroot /host cat /proc/sys/crypto/fips_enabled
# Should return 1 if FIPS is enabled

# Verify cert-manager respects FIPS mode
oc logs deployment/cert-manager -n cert-manager | grep -i fips

# Test that only FIPS-approved algorithms are used
# Attempt to create certificate with weak algorithm (should fail in FIPS mode)
```

### Phase 5: Cloud Provider Integration Testing

If testing cloud provider integration (AWS Route53, GCP Cloud DNS, Azure DNS):

#### 5.1 AWS Route53 (if applicable)

```bash
# Run AWS DNS01 tests
E2E_GINKGO_LABEL_FILTER='Platform: isSubsetOf {AWS} && Issuer: isSubsetOf {ACME-DNS01}' make test-e2e
```

#### 5.2 GCP Cloud DNS (if applicable)

```bash
# Run GCP DNS01 tests
E2E_GINKGO_LABEL_FILTER='Platform: isSubsetOf {GCP} && Issuer: isSubsetOf {ACME-DNS01}' make test-e2e
```

#### 5.3 Azure DNS (if applicable)

```bash
# Run Azure DNS01 tests
E2E_GINKGO_LABEL_FILTER='Platform: isSubsetOf {Azure} && Issuer: isSubsetOf {ACME-DNS01}' make test-e2e
```

## Troubleshooting

### Common Issues

#### Issue: Pods fail to start with crypto errors

**Symptoms:**
```
Error: failed to load private key: crypto/rsa: unsupported key size
```

**Investigation:**
```bash
# Check FIPS mode status
oc debug node/$NODE -- chroot /host cat /proc/sys/crypto/fips_enabled

# Check OpenSSL version
oc exec -n cert-manager $POD -- openssl version -a

# Check pod logs
oc logs $POD -n cert-manager
```

**Resolution:**
- Verify certificates use FIPS-approved key sizes (RSA >= 2048, ECDSA >= 256)
- Check for deprecated algorithms
- Review crypto library compatibility

#### Issue: TLS handshake failures

**Symptoms:**
```
Error: tls: failed to verify certificate: x509: certificate signed by unknown authority
```

**Investigation:**
```bash
# Check CA bundle
oc exec -n cert-manager $POD -- ls -la /etc/pki/tls/certs/

# Test TLS connection
oc exec -n cert-manager $POD -- curl -v https://kubernetes.default.svc
```

**Resolution:**
- Verify CA bundle is properly mounted
- Check proxy configuration
- Verify cluster certificates are valid

#### Issue: E2E tests fail on RHCOS 10

**Investigation:**
```bash
# Check test logs
cat /tmp/report.json | jq '.[] | select(.State == "failed")'

# Check cluster state
oc get pods -n cert-manager
oc get co

# Collect debug information
make test-e2e-debug-cluster
```

**Resolution:**
- Review test failure details
- Check for RHCOS 10 specific issues
- File bugs with detailed information

### Debug Information Collection

When filing bugs, collect the following information:

```bash
# Cluster version and node information
oc version > debug-info.txt
oc get nodes -o wide >> debug-info.txt
oc get nodes -o json | jq -r '.items[] | {name: .metadata.name, os: .status.nodeInfo.osImage, kernel: .status.nodeInfo.kernelVersion}' >> debug-info.txt

# Operator status
oc get csv -n cert-manager-operator >> debug-info.txt
oc get deployment -n cert-manager-operator >> debug-info.txt
oc logs deployment/cert-manager-operator-controller-manager -n cert-manager-operator --tail=100 >> debug-info.txt

# Operand status
oc get certmanager cluster -o yaml >> debug-info.txt
oc get pods -n cert-manager >> debug-info.txt
oc get events -n cert-manager --sort-by='.lastTimestamp' >> debug-info.txt

# Crypto information
NODE=$(oc get nodes -l node-role.kubernetes.io/worker -o jsonpath='{.items[0].metadata.name}')
oc debug node/$NODE -- chroot /host sh -c "cat /etc/os-release && openssl version -a && cat /proc/sys/crypto/fips_enabled" >> debug-info.txt

# Test results
cat /tmp/junit.xml >> debug-info.txt
cat /tmp/report.json | jq '.' >> debug-info.txt
```

## Results Documentation

After completing testing, document results using the template:

```bash
# Generate test report
make report-rhcos10
```

This will create a report in `_output/rhcos10-test-report.md`.

Alternatively, manually copy and fill out `docs/rhcos10-test-results-template.md`.

## Reporting Results

### Document Test Results

Document your test results using the provided template in `docs/rhcos10-test-results-template.md`:
- Test execution date
- OCP and RHCOS versions tested
- Test results summary
- Links to any bugs filed
- Overall status (Pass/Fail/Blocked)

### File Bugs

If issues are discovered:

1. File bugs in your project's bug tracker
2. Include all debug information collected
3. Tag with appropriate labels (rhcos10, rhel10, crypto)
4. Set appropriate priority based on impact
5. Include reproduction steps and environment details

### Share Results

Share test results with your team according to your organization's processes.

## Appendix: Quick Reference Commands

```bash
# Deploy operator
make deploy

# Wait for stable state
make test-e2e-wait-for-stable-state

# Run e2e tests
make test-e2e

# Verify crypto libraries
make verify-rhcos10-crypto

# Run RHCOS 10 compatibility tests
make test-rhcos10

# Generate report
make report-rhcos10

# Debug cluster state
make test-e2e-debug-cluster

# Clean up test resources
oc delete certificate,issuer,clusterissuer --all -n default
```
