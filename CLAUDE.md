# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is the Cert Manager Operator for OpenShift. It deploys and manages the upstream cert-manager project on OpenShift clusters. The operator runs in the `cert-manager-operator` namespace and deploys its operand (cert-manager) in the `cert-manager` namespace - both namespaces are hardcoded.

## Common Commands

### Building and Running

```bash
# Build the operator binary
make build

# Build operator binary only (skip code generation and checks)
make build-operator

# Run operator locally (requires connection to OpenShift cluster)
make deploy                                    # Install operator manifests and CRDs
oc scale --replicas=0 deploy --all -n cert-manager-operator  # Scale down cluster operator
make local-run                                 # Run operator locally

# Build and push container image
export IMG=<registry>/<repository>/cert-manager-operator:<tag>
make image-build image-push
```

### Testing

```bash
# Run unit tests
make test

# Run a single test
go test -v ./pkg/controller/deployment/... -run TestDeploymentOverrides

# Run e2e tests (requires deployed operator in cluster)
make test-e2e

# Run e2e tests with specific label filter
make test-e2e E2E_GINKGO_LABEL_FILTER="Platform: isSubsetOf {AWS}"
```

### Code Quality

```bash
# Run linter
make lint

# Format code
make fmt

# Run go vet
make vet

# Update generated code (deepcopy, clientgen, manifests, bindata)
make update

# Verify generated code is up to date
make verify
```

### Updating cert-manager Version

1. Update `CERT_MANAGER_VERSION` in the Makefile
2. Run `make update` to regenerate manifests and bindata

## Architecture

### Controller Structure

The operator uses OpenShift's library-go controller framework. The main entry point is `pkg/operator/starter.go` which initializes:

- **CertManagerControllerSet** (`pkg/controller/deployment/cert_manager_controller_set.go`): A collection of 8 controllers managing cert-manager components:
  - Controller deployment + static resources
  - Webhook deployment + static resources
  - CAInjector deployment + static resources
  - NetworkPolicy controllers (static and user-defined)

- **DefaultCertManagerController**: Ensures a cluster-scoped `CertManager` CR named `cluster` exists with defaults

### API Types

Located in `api/operator/v1alpha1/`:

- **CertManager**: Cluster-scoped singleton CR (must be named `cluster`) that configures cert-manager deployment. Supports customization of controller, webhook, and cainjector deployments via `controllerConfig`, `webhookConfig`, and `cainjectorConfig` fields.

- **IstioCSR**: Namespace-scoped singleton CR (must be named `default`) for deploying istio-csr agent. Only active when `IstioCSR` feature gate is enabled via `--unsupported-addon-features="IstioCSR=true"`.

### Generated Code

- **Bindata**: Static manifests in `bindata/` are compiled into `pkg/operator/assets/bindata.go` via `make update-bindata`
- **Client/Informers/Listers**: Generated in `pkg/operator/clientset/`, `pkg/operator/informers/`, `pkg/operator/listers/`
- **DeepCopy**: Generated via controller-gen for API types

### Deployment Manifests

- `bindata/cert-manager-crds/`: cert-manager CRDs
- `bindata/cert-manager-deployment/`: cert-manager deployment manifests split into controller/webhook/cainjector
- `config/`: Kustomize configuration for operator deployment
- `bundle/`: OLM bundle manifests

## Key Patterns

### Deployment Overrides

The operator supports overriding cert-manager deployment specs through the CertManager CR:
- `overrideArgs`: Additional CLI arguments
- `overrideEnv`: Environment variables
- `overrideLabels`: Pod labels
- `overrideResources`: Resource requests/limits
- `overrideReplicas`: Replica count
- `overrideScheduling`: NodeSelector and tolerations

Validation logic is in `pkg/controller/deployment/deployment_overrides_validation.go`.

### Feature Gates

Feature gates are defined in `api/operator/v1alpha1/features.go` and managed via `pkg/features/features.go`. Currently supports:
- `IstioCSR`: Enables istio-csr controller (Technology Preview)

### Environment Variables for Images

The operator uses `RELATED_IMAGE_*` environment variables to specify operand images:
- `RELATED_IMAGE_CERT_MANAGER_CONTROLLER`
- `RELATED_IMAGE_CERT_MANAGER_WEBHOOK`
- `RELATED_IMAGE_CERT_MANAGER_CA_INJECTOR`
- `RELATED_IMAGE_CERT_MANAGER_ACMESOLVER`
- `RELATED_IMAGE_CERT_MANAGER_ISTIOCSR`

## Testing

- Unit tests use standard Go testing with testify assertions
- E2E tests use Ginkgo/Gomega and require a running OpenShift cluster with the operator deployed
- E2E tests are tagged with `// +build e2e` and use platform labels for filtering (e.g., `Platform: AWS`)
- Test utilities are in `test/library/`

## Import Organization

Local imports should be separated from third-party packages using the prefix `github.com/openshift/cert-manager-operator` (configured in `.golangci.yaml`).
