# Project path.
PROJECT_ROOT := $(shell git rev-parse --show-toplevel)

# Warn when an undefined variable is referenced, helping catch typos and missing definitions.
MAKEFLAGS += --warn-undefined-variables

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL := /usr/bin/env bash
.SHELLFLAGS := -euo pipefail -c

# Ensure cache and config directories are writable (needed for CI environments where
# HOME may be unset or pointing to a non-writable directory like /).
export XDG_CACHE_HOME ?= $(PROJECT_ROOT)/_output/.cache
export XDG_CONFIG_HOME ?= $(PROJECT_ROOT)/_output/.config

# ============================================================================
# Version Configuration
# ============================================================================

# DEFAULT_VERSION is the default version to use for image tags when not set.
DEFAULT_VERSION := 1.19.0

# Helper function to validate semver (Major.Minor.Patch format)
# Returns 'valid' if the version matches semver (X.Y.Z) or 'latest', empty string otherwise
define validate-semver
$(shell echo '$(1)' | grep -Eq '^([0-9]+\.[0-9]+\.[0-9]+|latest)$$' && echo valid)
endef

# Formats version for image tags: adds 'v' prefix for semver, keeps 'latest' as-is
# Usage: $(call format-image-tag,1.0.0) -> v1.0.0
#        $(call format-image-tag,latest) -> latest
define format-image-tag
$(shell if [ '$(1)' = 'latest' ]; then echo '$(1)'; else echo 'v$(1)'; fi)
endef

# --- Project Versions ---

# BUNDLE_VERSION defines the version for the operator bundle (must be valid semver: Major.Minor.Patch).
BUNDLE_VERSION ?= $(DEFAULT_VERSION)
ifneq ($(call validate-semver,$(BUNDLE_VERSION)),valid)
$(error BUNDLE_VERSION '$(BUNDLE_VERSION)' is not valid semver (expected: Major.Minor.Patch))
endif

# IMG_VERSION defines the version tag for the operator image (must be valid semver: Major.Minor.Patch).
IMG_VERSION ?= latest
ifneq ($(call validate-semver,$(IMG_VERSION)),valid)
$(error IMG_VERSION '$(IMG_VERSION)' is not valid semver (expected: Major.Minor.Patch))
endif

# CATALOG_VERSION defines the version for the OLM catalog/index image (must be valid semver: Major.Minor.Patch).
CATALOG_VERSION ?= $(DEFAULT_VERSION)
ifneq ($(call validate-semver,$(CATALOG_VERSION)),valid)
$(error CATALOG_VERSION '$(CATALOG_VERSION)' is not valid semver (expected: Major.Minor.Patch))
endif

# --- Operand Versions ---

# Versions of the cert-manager components managed by this operator
CERT_MANAGER_VERSION ?= v1.19.2
ISTIO_CSR_VERSION ?= v0.15.0

# --- Test Versions ---

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION ?= 1.25.0

# ============================================================================
# Path Configuration
# ============================================================================

# Package name.
PACKAGE := github.com/openshift/cert-manager-operator

# Output directories
BIN_DIR := $(PROJECT_ROOT)/bin
OUTPUT_DIR := $(PROJECT_ROOT)/_output
ENVTEST_ASSETS_DIR := $(PROJECT_ROOT)/testbin

# Binary name derived from package
BIN := $(PROJECT_ROOT)/$(lastword $(subst /, ,$(PACKAGE)))

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN := $(shell go env GOPATH)/bin
else
GOBIN := $(shell go env GOBIN)
endif

# Tool binary paths (all built from vendor for consistency and performance)
CONTROLLER_GEN := $(BIN_DIR)/controller-gen
GOLANGCI_LINT := $(BIN_DIR)/golangci-lint
GOVULNCHECK := $(BIN_DIR)/govulncheck
HELM := $(BIN_DIR)/helm
KUSTOMIZE := $(BIN_DIR)/kustomize
OPERATOR_SDK := $(BIN_DIR)/operator-sdk
OPM := $(BIN_DIR)/opm
SETUP_ENVTEST := $(BIN_DIR)/setup-envtest
JSONNET := $(BIN_DIR)/jsonnet

# ============================================================================
# Image Configuration
# ============================================================================

# IMAGE_TAG_BASE defines the registry namespace and base name for all images.
# This is used to construct full image tags for operator, bundle, and catalog images.
IMAGE_TAG_BASE ?= openshift.io/cert-manager-operator

# Operator image
# Fixing the tag to latest, to align with the tag in config/manager/kustomization.yaml 
IMG ?= $(IMAGE_TAG_BASE):$(call format-image-tag,$(IMG_VERSION))

# Bundle image (OLM operator bundle)
BUNDLE_IMG ?= $(IMAGE_TAG_BASE)-bundle:$(call format-image-tag,$(BUNDLE_VERSION))

# Catalog/Index image (OLM catalog containing bundles)
CATALOG_IMG ?= $(IMAGE_TAG_BASE)-catalog:$(call format-image-tag,$(CATALOG_VERSION))

# USE_IMAGE_DIGESTS defines if images are resolved via tags or digests.
# To enable: make bundle USE_IMAGE_DIGESTS=true
USE_IMAGE_DIGESTS ?= false

# ============================================================================
# Bundle / OLM Configuration
# ============================================================================

# CHANNELS define the bundle channels used in the bundle.
# To override: make bundle CHANNELS=candidate,fast,stable or export CHANNELS="candidate,fast,stable"
CHANNELS ?= stable-v1,stable-v1.19
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif

# DEFAULT_CHANNEL defines the default channel used in the bundle.
# To override: make bundle DEFAULT_CHANNEL=stable or export DEFAULT_CHANNEL="stable"
DEFAULT_CHANNEL ?= stable-v1
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif

BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)

# BUNDLE_GEN_FLAGS are the flags passed to the operator-sdk generate bundle command
BUNDLE_GEN_FLAGS ?= -q --overwrite=false --version $(BUNDLE_VERSION) $(BUNDLE_METADATA_OPTS)
ifeq ($(USE_IMAGE_DIGESTS), true)
	BUNDLE_GEN_FLAGS += --use-image-digests
endif

# ============================================================================
# Container Configuration
# ============================================================================

# Container engine to use for building and pushing images
CONTAINER_ENGINE ?= podman

# TLS verification for container pushes (disable for local registries)
TLS_VERIFY ?= true
CONTAINER_PUSH_ARGS ?= $(if $(filter $(CONTAINER_ENGINE),docker),,--tls-verify=$(TLS_VERIFY))

# Container image used for running make targets in a container
CONTAINER_IMAGE_NAME ?= registry.ci.openshift.org/ocp/builder:rhel-9-golang-1.25-openshift-4.21

# ============================================================================
# Build Configuration
# ============================================================================

# Git information for version embedding
COMMIT ?= $(shell git rev-parse HEAD)
SHORTCOMMIT ?= $(shell git rev-parse --short HEAD)

# Go build flags
GOBUILD_VERSION_ARGS := -ldflags "-X $(PACKAGE)/pkg/version.SHORTCOMMIT=$(SHORTCOMMIT) -X $(PACKAGE)/pkg/version.COMMIT=$(COMMIT)"
GO := GO111MODULE=on CGO_ENABLED=1 go

# ============================================================================
# Test Configuration
# ============================================================================

# E2E test timeout
E2E_TIMEOUT ?= 2h

# E2E_GINKGO_LABEL_FILTER is ginkgo label query for selecting tests.
# See https://onsi.github.io/ginkgo/#spec-labels
# The default is to run tests on the AWS platform.
E2E_GINKGO_LABEL_FILTER ?= Platform: isSubsetOf {AWS} && CredentialsMode: isSubsetOf {Mint}

# ============================================================================
# Default Target
# ============================================================================

.PHONY: all
all: build verify

# ============================================================================
# Help
# ============================================================================

##@ General

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-25s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

# ============================================================================
# Build Machinery Includes
# ============================================================================

# Include the library makefiles
include $(addprefix ./vendor/github.com/openshift/build-machinery-go/make/, \
	targets/openshift/bindata.mk \
)

# Generate bindata targets
$(call add-bindata,assets,./bindata/...,bindata,assets,pkg/operator/assets/bindata.go)

# ============================================================================
# Development
# ============================================================================

##@ Development

.PHONY: manifests
manifests: $(CONTROLLER_GEN) ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="$(PROJECT_ROOT)/..." \
		output:crd:artifacts:config=$(PROJECT_ROOT)/config/crd/bases \
		output:rbac:artifacts:config=$(PROJECT_ROOT)/config/rbac

.PHONY: generate
generate: $(CONTROLLER_GEN) ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="$(PROJECT_ROOT)/hack/boilerplate.go.txt" paths="$(PROJECT_ROOT)/api/..."
	hack/update-clientgen.sh

# Targets that need Go workspace mode (CI sets GOFLAGS=-mod=vendor which conflicts with go.work)
fmt vet test test-e2e run update-vendor update-dep: GOFLAGS=

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: manifests generate fmt vet $(SETUP_ENVTEST) ## Run unit tests.
	mkdir -p "$(ENVTEST_ASSETS_DIR)"
	KUBEBUILDER_ASSETS="$(shell $(SETUP_ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(ENVTEST_ASSETS_DIR) -p path)" \
	go test ./... -coverprofile cover.out

# Test name pattern for -run flag (TEST="TestIssuer|TestCertificate" or TEST="" to run all)
TEST ?=
.PHONY: test-e2e
test-e2e: test-e2e-wait-for-stable-state ## Run end-to-end tests.
	go test -C $(PROJECT_ROOT)/test/e2e \
		-timeout $(E2E_TIMEOUT) \
		-count 1 -v -p 1 \
		-tags e2e -run "$(TEST)" . \
		-ginkgo.label-filter=$(E2E_GINKGO_LABEL_FILTER)

.PHONY: test-e2e-wait-for-stable-state
test-e2e-wait-for-stable-state:
	@echo "---- Waiting for stable state ----"
	@# This ensures the test-e2e-debug-cluster is called if a timeout is reached.
	oc wait --for=condition=Available=true deployment/cert-manager-cainjector -n cert-manager --timeout=120s || $(MAKE) test-e2e-debug-cluster
	oc wait --for=condition=Available=true deployment/cert-manager -n cert-manager --timeout=120s || $(MAKE) test-e2e-debug-cluster
	oc wait --for=condition=Available=true deployment/cert-manager-webhook -n cert-manager --timeout=120s || $(MAKE) test-e2e-debug-cluster
	@echo "---- /Waiting for stable state ----"

.PHONY: test-e2e-debug-cluster
test-e2e-debug-cluster:
	@echo "---- Debugging the current state ----"
	-oc get pod -n cert-manager-operator
	-oc get pod -n cert-manager
	-oc get co
	-oc get csv --all-namespaces
	-oc get crd | grep -i cert
	-oc get subscriptions --all-namespaces
	-oc logs deployment/cert-manager-operator -n cert-manager-operator
	@echo "---- /Debugging the current state ----"

.PHONY: lint
lint: $(GOLANGCI_LINT) ## Run golangci-lint linter.
	$(GOLANGCI_LINT) run --verbose --config $(PROJECT_ROOT)/.golangci.yaml $(PROJECT_ROOT)/...

.PHONY: lint-fix
lint-fix: $(GOLANGCI_LINT) ## Run golangci-lint linter and fix issues.
	$(GOLANGCI_LINT) run --config $(PROJECT_ROOT)/.golangci.yaml --fix $(PROJECT_ROOT)/...

.PHONY: local-run
local-run: build ## Run the operator locally against the cluster configured in ~/.kube/config.
	RELATED_IMAGE_CERT_MANAGER_WEBHOOK=quay.io/jetstack/cert-manager-webhook:$(CERT_MANAGER_VERSION) \
	RELATED_IMAGE_CERT_MANAGER_CA_INJECTOR=quay.io/jetstack/cert-manager-cainjector:$(CERT_MANAGER_VERSION) \
	RELATED_IMAGE_CERT_MANAGER_CONTROLLER=quay.io/jetstack/cert-manager-controller:$(CERT_MANAGER_VERSION) \
	RELATED_IMAGE_CERT_MANAGER_ACMESOLVER=quay.io/jetstack/cert-manager-acmesolver:$(CERT_MANAGER_VERSION) \
	RELATED_IMAGE_CERT_MANAGER_ISTIOCSR=quay.io/jetstack/cert-manager-istio-csr:$(ISTIO_CSR_VERSION) \
	OPERATOR_NAME=cert-manager-operator \
	OPERAND_IMAGE_VERSION=$(BUNDLE_VERSION) \
	OPERATOR_IMAGE_VERSION=$(BUNDLE_VERSION) \
	./cert-manager-operator start \
		--config=./hack/local-run-config.yaml \
		--kubeconfig=$${KUBECONFIG:-$$HOME/.kube/config} \
		--namespace=cert-manager-operator

# ============================================================================
# Build
# ============================================================================

##@ Build

.PHONY: build
build: generate fmt vet build-operator ## Build operator binary with all checks and code generation.

.PHONY: build-operator
build-operator: ## Build operator binary only (no checks or code generation).
	@GOFLAGS="-mod=vendor" source hack/go-fips.sh && $(GO) build $(GOBUILD_VERSION_ARGS) -o $(BIN)

.PHONY: run
run: manifests generate fmt vet ## Run the operator from your host (for development).
	go run $(PACKAGE)

.PHONY: image-build
image-build: ## Build container image with the operator.
	$(CONTAINER_ENGINE) build -t $(IMG) .

.PHONY: image-push
image-push: ## Push container image with the operator.
	$(CONTAINER_ENGINE) push $(IMG) $(CONTAINER_PUSH_ARGS)

# ============================================================================
# Deployment
# ============================================================================

##@ Deployment

.PHONY: deploy
deploy: $(KUSTOMIZE) manifests ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	@kubectl get namespace cert-manager-operator >/dev/null 2>&1 || { \
		echo "Namespace 'cert-manager-operator' does not exist. Creating it..."; \
		kubectl create namespace cert-manager-operator; \
	}
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	$(KUSTOMIZE) build config/default | kubectl apply -f -

.PHONY: undeploy
undeploy: $(KUSTOMIZE) ## Undeploy controller from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/default | kubectl delete --ignore-not-found -f -
	kubectl delete namespace cert-manager-operator --ignore-not-found

# ============================================================================
# Bundle / OLM
# ============================================================================

##@ Bundle / OLM

.PHONY: bundle
bundle: $(OPERATOR_SDK) $(KUSTOMIZE) manifests ## Generate bundle manifests and metadata, then validate generated files.
	$(OPERATOR_SDK) generate kustomize manifests -q
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	$(KUSTOMIZE) build config/manifests | $(OPERATOR_SDK) generate bundle $(BUNDLE_GEN_FLAGS)
	$(OPERATOR_SDK) bundle validate ./bundle

.PHONY: bundle-build
bundle-build: bundle ## Build the bundle image.
	$(CONTAINER_ENGINE) build -t $(BUNDLE_IMG) -f bundle.Dockerfile .

.PHONY: bundle-push
bundle-push: ## Push the bundle image.
	$(CONTAINER_ENGINE) push $(BUNDLE_IMG) $(CONTAINER_PUSH_ARGS)

.PHONY: catalog-build
catalog-build: $(OPM) ## Build the OLM catalog image.
	$(OPM) index add --container-tool $(CONTAINER_ENGINE) --mode semver --tag $(CATALOG_IMG) --bundles $(BUNDLE_IMG)

.PHONY: catalog-push
catalog-push: ## Push the OLM catalog image.
	$(CONTAINER_ENGINE) push $(CATALOG_IMG) $(CONTAINER_PUSH_ARGS)

# ============================================================================
# Verification
# ============================================================================

##@ Verification

.PHONY: verify
verify: verify-scripts verify-deps fmt vet ## Run all verification checks.

.PHONY: verify-scripts
verify-scripts: verify-bindata ## Run script-based verification checks.
	hack/verify-deepcopy.sh
	hack/verify-clientgen.sh
	hack/verify-bundle.sh

.PHONY: verify-deps
verify-deps: ## Verify Go module dependencies are correct.
	hack/verify-deps.sh

.PHONY: verify-with-container
verify-with-container: ## Run verification in a container.
	$(CONTAINER_ENGINE) run -ti --rm \
		-v $(PROJECT_ROOT):/go/src/github.com/openshift/cert-manager-operator:z \
		-w /go/src/github.com/openshift/cert-manager-operator \
		$(CONTAINER_IMAGE_NAME) make verify

.PHONY: govulncheck
govulncheck: $(GOVULNCHECK) $(OUTPUT_DIR) ## Run govulncheck vulnerability scan.
	@./hack/govulncheck.sh $(GOVULNCHECK) $(OUTPUT_DIR)

# ============================================================================
# Maintenance
# ============================================================================

##@ Maintenance

.PHONY: update
update: generate update-manifests update-bindata ## Update all generated code and manifests.

.PHONY: update-manifests
update-manifests: $(HELM) $(JSONNET) ## Update cert-manager and istio-csr operand manifests.
	hack/update-cert-manager-manifests.sh $(CERT_MANAGER_VERSION)
	hack/update-istio-csr-manifests.sh $(ISTIO_CSR_VERSION)

.PHONY: update-vendor
update-vendor: ## Update vendor directory for all modules in the workspace.
	go mod tidy
	go mod tidy -C $(PROJECT_ROOT)/test
	go mod tidy -C $(PROJECT_ROOT)/tools
	go work sync
	go work vendor

PKG ?=
.PHONY: update-dep
update-dep: ## Update a dependency across all modules. Usage: make update-dep PKG=k8s.io/api@v0.35.0
	@if [ -z "$(PKG)" ]; then echo "Usage: make update-dep PKG=package@version"; exit 1; fi
	@echo "Updating $(PKG) in main module..."
	go get $(PKG)
	@echo "Updating $(PKG) in test module..."
	go get -C $(PROJECT_ROOT)/test $(PKG)
	@echo "Updating $(PKG) in tools module..."
	go get -C $(PROJECT_ROOT)/tools $(PKG)
	@echo "Running update-vendor..."
	$(MAKE) update-vendor

.PHONY: update-with-container
update-with-container: ## Run update targets in a container.
	$(CONTAINER_ENGINE) run -ti --rm \
		-v $(PROJECT_ROOT):/go/src/github.com/openshift/cert-manager-operator:z \
		-w /go/src/github.com/openshift/cert-manager-operator \
		$(CONTAINER_IMAGE_NAME) make update

.PHONY: clean
clean: ## Clean up generated files and build artifacts.
	@echo "Cleaning up build artifacts..."
	go clean
	rm -rf $(BIN_DIR) $(OUTPUT_DIR) $(ENVTEST_ASSETS_DIR) cover.out $(BIN)

# ============================================================================
# Tool Installation
# ============================================================================

##@ Tools

# go-install-tool will 'go build' any package from vendor with custom target and name of the binary.
# $1 - target path with name of binary
# $2 - package path in vendor
define go-install-tool
@{ \
	bin_path=$(1); \
	package=$(2); \
	echo "Building $${package}..."; \
	mkdir -p $$(dirname $${bin_path}); \
	rm -f $${bin_path} 2>/dev/null || true; \
	go build -mod=vendor -o $${bin_path} $${package}; \
}
endef

# get-bin downloads a binary from a URL if it doesn't exist.
# $1 - target path with name of binary
# $2 - target directory
# $3 - download URL
# $4 - (optional) SHA256 checksum for verification
define get-bin
@[ -f "$(1)" ] || { \
	mkdir -p "$(2)"; \
	echo "Downloading $(3)..."; \
	curl -fsSL "$(3)" -o "$(1)"; \
	if [ -n "$(4)" ]; then echo "$(4)  $(1)" | sha256sum -c -; fi; \
	chmod +x "$(1)"; \
}
endef

## Location to install dependencies to
$(BIN_DIR):
	mkdir -p $(BIN_DIR)

$(OUTPUT_DIR):
	@mkdir -p $(OUTPUT_DIR)

# Tools built from vendor
$(CONTROLLER_GEN): $(BIN_DIR) ## Build controller-gen from vendor.
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen)

$(GOLANGCI_LINT): $(BIN_DIR) ## Build golangci-lint from vendor.
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/v2/cmd/golangci-lint)

$(KUSTOMIZE): $(BIN_DIR) ## Build kustomize from vendor.
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5)

$(SETUP_ENVTEST): $(BIN_DIR) ## Build setup-envtest from vendor.
	$(call go-install-tool,$(SETUP_ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest)

$(GOVULNCHECK): $(BIN_DIR) ## Build govulncheck from vendor.
	$(call go-install-tool,$(GOVULNCHECK),golang.org/x/vuln/cmd/govulncheck)

$(JSONNET): $(BIN_DIR) ## Build jsonnet from vendor.
	$(call go-install-tool,$(JSONNET),github.com/google/go-jsonnet/cmd/jsonnet)

# Tools downloaded as binaries (with checksum verification)
$(HELM): ## Download helm locally if necessary.
	hack/download-tools.sh helm $(HELM)

$(OPERATOR_SDK): ## Download operator-sdk locally if necessary.
	hack/download-tools.sh operator-sdk $(OPERATOR_SDK)

$(OPM): ## Download opm locally if necessary.
	hack/download-tools.sh opm $(OPM)
