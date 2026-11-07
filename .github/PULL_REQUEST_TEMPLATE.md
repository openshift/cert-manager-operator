## Description

<!-- What does this change do, and why? -->

## Jira

<!-- Prefix the PR title and commits with a Jira key when one exists (CM-, OCPBUGS-, OAPE-, or NO-JIRA). -->

- Jira: <!-- e.g. CM-1234 / OCPBUGS-12345 -->

## Type of Change

- [ ] Bug fix
- [ ] New feature
- [ ] API / CRD change
- [ ] Documentation
- [ ] Test-only
- [ ] Other

## Testing

Do **not** use `go test ./...`. Same checks as OpenShift CI:

- [ ] `make fmt`
- [ ] `make lint`
- [ ] `make verify`
- [ ] `make test-unit`
- [ ] `make test-e2e` (if the change needs a cluster)

## Checklist

- [ ] Did not hand-edit generated assets (`bindata/`, `bundle/`, generated clients)
- [ ] New controller-runtime operands follow TrustManager SSA, not IstioCSR Create+Update
- [ ] Commit messages are **not** Conventional Commits (`feat:`, `fix:`)
- [ ] Review requested from [OWNERS](https://github.com/openshift/cert-manager-operator/blob/master/OWNERS)
