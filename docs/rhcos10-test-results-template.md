# RHCOS 10 / RHEL 10 Compatibility Test Results

> **Template Instructions:** Copy this template and fill in the details for each test run.

---

## Test Information

**Date:** YYYY-MM-DD
**Tester:** [Your Name]
**Tracking Issue:** [Link to your tracking issue/story]

---

## Cluster Information

**OpenShift Version:** [e.g., 4.20.1, 4.21.0, 4.22.0]
**RHCOS Version:** [e.g., Red Hat Enterprise Linux CoreOS 410.92.202X...]
**Platform:** [e.g., AWS, GCP, Azure, BareMetal]
**Cluster Name/ID:** [e.g., rhcos10-test-cluster]
**Region/Zone:** [if applicable, e.g., us-east-1]

**FIPS Mode:** [ ] Enabled / [ ] Disabled

---

## cert-manager Operator Version

**Operator Version:** [e.g., 1.19.0]
**cert-manager Version:** [e.g., v1.19.2]
**Installation Method:** [ ] OLM / [ ] Manifests
**Subscription Channel:** [if OLM, e.g., stable-v1]

---

## Test Results Summary

| Test Category | Status | Notes |
|--------------|--------|-------|
| Deployment Verification | [ ] Pass / [ ] Fail | |
| E2E Test Suite | [ ] Pass / [ ] Fail / [ ] Partial | |
| Crypto Library Verification | [ ] Pass / [ ] Fail | |
| Integration Testing | [ ] Pass / [ ] Fail / [ ] N/A | |

**Overall Status:** [ ] ✅ PASS / [ ] ❌ FAIL / [ ] ⚠️ BLOCKED

---

## Detailed Test Results

### 1. Deployment Verification

**Status:** [ ] Pass / [ ] Fail

#### Operator Deployment

- **cert-manager-operator namespace:** [ ] Created
- **Operator deployment:** [ ] Available
- **Operator pods:** [ ] Running
- **Operator logs:** [ ] No errors / [ ] Errors found (details below)

#### Operand Deployment

- **cert-manager namespace:** [ ] Created
- **cert-manager deployment:** [ ] Available
- **cert-manager-webhook deployment:** [ ] Available
- **cert-manager-cainjector deployment:** [ ] Available
- **All pods running:** [ ] Yes / [ ] No

#### Issues Found
```
[Describe any deployment issues here, or write "None"]
```

---

### 2. E2E Test Suite Results

**Status:** [ ] Pass / [ ] Fail / [ ] Partial

**Test Command Used:**
```bash
make test-e2e
# or with filters:
# E2E_GINKGO_LABEL_FILTER='Platform: isSubsetOf {AWS}' make test-e2e
```

**Test Duration:** [e.g., 45 minutes]

**Test Results:**
- **Total Tests:** [number]
- **Passed:** [number]
- **Failed:** [number]
- **Skipped:** [number]

#### Failed Tests (if any)

| Test Name | Failure Reason | JIRA Bug (if filed) |
|-----------|---------------|---------------------|
| [Test 1] | [Reason] | [BUG-XXX] |
| [Test 2] | [Reason] | [BUG-XXX] |

#### Test Execution Notes
```
[Add any relevant notes about test execution, e.g., flaky tests, environmental issues, etc.]
```

---

### 3. Crypto Library Verification

**Status:** [ ] Pass / [ ] Fail

**Verification Command:**
```bash
make verify-rhcos10-crypto
```

#### OpenSSL Version

**Node OpenSSL Version:** [e.g., OpenSSL 3.0.7 1 Nov 2022]
**Container OpenSSL Version:** [e.g., OpenSSL 3.0.7 1 Nov 2022]
**Expected:** OpenSSL 3.x for RHCOS 10 ✓

#### FIPS Mode Status

**FIPS Enabled on Nodes:** [ ] Yes / [ ] No
**Value:** [0 or 1]

If FIPS enabled:
- [ ] cert-manager respects FIPS mode
- [ ] Only FIPS-approved algorithms used
- [ ] No FIPS-related errors in logs

#### TLS Connectivity

- **TLS connection to Kubernetes API:** [ ] Success / [ ] Failed
- **Cipher used:** [e.g., TLS_AES_128_GCM_SHA256]
- **Protocol:** [e.g., TLSv1.3]

#### Certificate Generation Tests

| Algorithm | Key Size | Status | Notes |
|-----------|----------|--------|-------|
| RSA | 2048 | [ ] Pass / [ ] Fail | |
| RSA | 4096 | [ ] Pass / [ ] Fail | |
| ECDSA | P-256 | [ ] Pass / [ ] Fail | |
| ECDSA | P-384 | [ ] Pass / [ ] Fail | |

#### Crypto Errors in Logs

[ ] No crypto-related errors found
[ ] Crypto errors found (details below)

```
[Paste any crypto-related errors from logs, or write "None"]
```

---

### 4. Integration Testing

**Status:** [ ] Pass / [ ] Fail / [ ] N/A

#### Cloud Provider Integration (if applicable)

**Provider:** [ ] AWS Route53 / [ ] GCP Cloud DNS / [ ] Azure DNS / [ ] N/A

**Test Command:**
```bash
# Example for AWS:
# E2E_GINKGO_LABEL_FILTER='Platform: isSubsetOf {AWS} && Issuer: isSubsetOf {ACME-DNS01}' make test-e2e
```

**Results:**
- **ACME DNS01 Issuer:** [ ] Pass / [ ] Fail / [ ] N/A
- **ACME HTTP01 Issuer:** [ ] Pass / [ ] Fail / [ ] N/A
- **Cloud credentials:** [ ] Working / [ ] Issues

**Notes:**
```
[Add notes about cloud provider integration testing]
```

#### Vault Integration (if tested)

- **Vault Issuer:** [ ] Pass / [ ] Fail / [ ] N/A

**Notes:**
```
[Add notes about Vault integration testing]
```

---

## Issues and Bugs

### Bugs Filed

| JIRA ID | Summary | Severity | Status |
|---------|---------|----------|--------|
| [BUG-XXX] | [Bug summary] | [ ] Critical / [ ] Major / [ ] Minor | [ ] New / [ ] In Progress |
| [BUG-XXX] | [Bug summary] | [ ] Critical / [ ] Major / [ ] Minor | [ ] New / [ ] In Progress |

### Known Issues / Workarounds

```
[Document any known issues and workarounds here, or write "None"]

Example:
- Issue: E2E test TestXYZ flakes occasionally
  Workaround: Rerun tests; appears to be timing-related
  Bug: BUG-XXX
```

---

## Performance and Resource Usage

### Pod Resource Usage

| Pod | CPU (avg) | Memory (avg) | Status |
|-----|-----------|--------------|--------|
| cert-manager-operator-controller-manager | [e.g., 10m] | [e.g., 50Mi] | Normal / High |
| cert-manager | [e.g., 20m] | [e.g., 100Mi] | Normal / High |
| cert-manager-webhook | [e.g., 5m] | [e.g., 30Mi] | Normal / High |
| cert-manager-cainjector | [e.g., 10m] | [e.g., 50Mi] | Normal / High |

**Resource usage compared to RHCOS 9:**
```
[Compare if you have baseline from RHCOS 9, or write "N/A - no baseline available"]
```

---

## Observations and Notes

### RHCOS 10 Specific Observations

```
[Document any RHCOS 10 specific behavior, issues, or improvements observed]

Examples:
- Faster TLS handshakes with OpenSSL 3.x
- Different default cipher suites
- FIPS mode behavior changes
- etc.
```

### Differences from RHCOS 9 (if known)

```
[Document any differences observed compared to RHCOS 9, or write "N/A"]
```

### Recommendations

```
[Add any recommendations for users, documentation updates, or future improvements]
```

---

## Supporting Evidence

### Logs and Diagnostics

**Location of collected diagnostics:** [e.g., _output/diagnostics/]

**Key files:**
- Cluster version: [file path or attachment]
- Node information: [file path or attachment]
- Operator logs: [file path or attachment]
- Test results: [file path or attachment]
- Crypto verification report: [file path or attachment]

### Screenshots (optional)

[Attach screenshots if relevant, e.g., dashboard views, test results, error messages]

---

## Conclusion

### Summary

```
[Provide a 2-3 sentence summary of the test results]

Example:
cert-manager operator was successfully deployed and tested on OpenShift 4.22 with RHCOS 10.
All e2e tests passed, and crypto library verification confirmed compatibility with OpenSSL 3.x.
No critical issues were found, and the component is ready for OpenShift 4.22 release.
```

### Recommendation

[ ] **Approved for Release** - No blocking issues found
[ ] **Conditional Approval** - Minor issues found, documented with workarounds
[ ] **Not Ready** - Critical issues must be resolved before release
[ ] **Blocked** - Cannot complete testing due to [reason]

### Sign-off

**Tested By:** [Your Name]
**Date:** YYYY-MM-DD
**Signature:** [Your signature or approval]

---

## References

- **Testing Guide:** [docs/rhcos10-testing.md](rhcos10-testing.md)
- **Project Repository:** [cert-manager-operator](https://github.com/openshift/cert-manager-operator)

---

## Appendix: Command Reference

### Quick Test Commands

```bash
# Deploy operator
make deploy

# Wait for stable state
make test-e2e-wait-for-stable-state

# Run all compatibility tests
make test-rhcos10

# Run only crypto verification
make verify-rhcos10-crypto

# Run e2e tests
make test-e2e

# Collect diagnostics
oc get nodes -o wide > nodes.txt
oc get pods -n cert-manager-operator > operator-pods.txt
oc get pods -n cert-manager > operand-pods.txt
oc logs deployment/cert-manager -n cert-manager --tail=100 > cert-manager-logs.txt
```

### Useful Debug Commands

```bash
# Check RHCOS version
oc get nodes -o json | jq -r '.items[] | {name: .metadata.name, os: .status.nodeInfo.osImage}'

# Check FIPS mode
NODE=$(oc get nodes -o name | head -1)
oc debug $NODE -- chroot /host cat /proc/sys/crypto/fips_enabled

# Check OpenSSL version on node
oc debug $NODE -- chroot /host openssl version -a

# Check cert-manager pod status
oc get pods -n cert-manager -o wide
oc describe pod <pod-name> -n cert-manager

# Check for crypto errors in logs
oc logs deployment/cert-manager -n cert-manager | grep -iE "(error|crypto|ssl|tls|fips)"
```

---

*Template Version: 1.0*
*Last Updated: 2026-03-06*
