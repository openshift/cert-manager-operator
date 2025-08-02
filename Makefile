# BUNDLE_VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the BUNDLE_VERSION as arg of the bundle target (e.g make bundle BUNDLE_VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export BUNDLE_VERSION=0.0.2)
BUNDLE_VERSION ?= 1.18.0
CERT_MANAGER_VERSION ?= "v1.18.2"
ISTIO_CSR_VERSION ?= "v0.14.2"

# CHANNELS define the bundle channels used in the bundle.
# Add a new line here if you would like to change its default config. (E.g CHANNELS = "candidate,fast,stable")
# To re-generate a bundle for other specific channels without changing the standard setup, you can:
# - use the CHANNELS as arg of the bundle target (e.g make bundle CHANNELS=candidate,fast,stable)
# - use environment variables to overwrite this value (e.g export CHANNELS="candidate,fast,stable")
CHANNELS ?= "stable-v1,stable-v1.18"
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif

# DEFAULT_CHANNEL defines the default channel used in the bundle.
# Add a new line here if you would like to change its default config. (E.g DEFAULT_CHANNEL = "stable")
# To re-generate a bundle for any other default channel without changing the default setup, you can:
# - use the DEFAULT_CHANNEL as arg of the bundle target (e.g make bundle DEFAULT_CHANNEL=stable)
# - use environment variables to overwrite this value (e.g export DEFAULT_CHANNEL="stable")
DEFAULT_CHANNEL ?= stable-v1
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif
BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)

# IMAGE_TAG_BASE defines the docker.io namespace and part of the image name for remote images.
# This variable is used to construct full image tags for bundle and catalog images.
#
# For example, running 'make bundle-build bundle-push catalog-build catalog-push' will build and push both
# openshift.io/cert-manager-operator-bundle:$VERSION and openshift.io/cert-manager-operator-catalog:$VERSION.
IMAGE_TAG_BASE ?= openshift.io/cert-manager-operator

# BUNDLE_IMG defines the image:tag used for the bundle.
# You can use it as an arg. (E.g make bundle-build BUNDLE_IMG=<some-registry>/<project-name-bundle>:<tag>)
BUNDLE_IMG ?= $(IMAGE_TAG_BASE)-bundle:v$(BUNDLE_VERSION)

# BUNDLE_GEN_FLAGS are the flags passed to the operator-sdk generate bundle command
BUNDLE_GEN_FLAGS ?= -q --overwrite=false --version $(BUNDLE_VERSION) $(BUNDLE_METADATA_OPTS)

# USE_IMAGE_DIGESTS defines if images are resolved via tags or digests
# You can enable this value if you would like to use SHA Based Digests
# To enable set flag to true
USE_IMAGE_DIGESTS ?= false
ifeq ($(USE_IMAGE_DIGESTS), true)
	BUNDLE_GEN_FLAGS += --use-image-digests
endif

# Image URL to use all building/pushing image targets
IMG ?= $(IMAGE_TAG_BASE):$(IMG_VERSION)

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.25.0

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

GOLANGCI_LINT ?= go run github.com/golangci/golangci-lint/cmd/golangci-lint

CONTROLLER_GEN := go run sigs.k8s.io/controller-tools/cmd/controller-gen

SETUP_ENVTEST := go run sigs.k8s.io/controller-runtime/tools/setup-envtest

KUSTOMIZE := go run sigs.k8s.io/kustomize/kustomize/v5

K8S_ENVTEST_VERSION := 1.21.4

PACKAGE=github.com/openshift/cert-manager-operator

BIN=$(lastword $(subst /, ,$(PACKAGE)))
BIN_DIR=$(shell pwd)/bin

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

CONTAINER_ENGINE ?= podman
CONTAINER_PUSH_ARGS ?= $(if $(filter ${CONTAINER_ENGINE}, docker), , --tls-verify=${TLS_VERIFY})
TLS_VERIFY ?= true
CONTAINER_IMAGE_NAME ?= registry.ci.openshift.org/ocp/builder:rhel-9-golang-1.24-openshift-4.20

BUNDLE_DIR := bundle
BUNDLE_MANIFEST_DIR := $(BUNDLE_DIR)/manifests
BUNDLE_IMG ?= olm-bundle:latest
INDEX_IMG ?= olm-bundle-index:latest
OPM_VERSION ?= v1.23.0

GOLANGCI_LINT_BIN=$(BIN_DIR)/golangci-lint

OPERATOR_SDK_BIN=$(BIN_DIR)/operator-sdk

HELM_BIN=$(BIN_DIR)/helm

COMMIT ?= $(shell git rev-parse HEAD)
SHORTCOMMIT ?= $(shell git rev-parse --short HEAD)
GOBUILD_VERSION_ARGS = -ldflags "-X $(PACKAGE)/pkg/version.SHORTCOMMIT=$(SHORTCOMMIT) -X $(PACKAGE)/pkg/version.COMMIT=$(COMMIT)"

E2E_TIMEOUT ?= 1h
# E2E_GINKGO_LABEL_FILTER is ginkgo label query for selecting tests. See
# https://onsi.github.io/ginkgo/#spec-labels. The default is to run tests on the AWS platform.
E2E_GINKGO_LABEL_FILTER ?= "Platform: isSubsetOf {AWS}"

MANIFEST_SOURCE = https://github.com/cert-manager/cert-manager/releases/download/$(CERT_MANAGER_VERSION)/cert-manager.yaml

##@ Development

# Include the library makefiles
include $(addprefix ./vendor/github.com/openshift/build-machinery-go/make/, \
	targets/openshift/bindata.mk \
)

# generate bindata targets
$(call add-bindata,assets,./bindata/...,bindata,assets,pkg/operator/assets/bindata.go)

.PHONY: manifests
manifests: ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./api/..."
	hack/update-clientgen.sh

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

ENVTEST_ASSETS_DIR ?= $(shell pwd)/testbin
.PHONY: test
test: manifests generate fmt vet ## Run tests.
	mkdir -p "$(ENVTEST_ASSETS_DIR)"
	KUBEBUILDER_ASSETS="$(shell $(SETUP_ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(ENVTEST_ASSETS_DIR) -p path)" go test ./... -coverprofile cover.out

update-manifests: $(HELM_BIN)
	hack/update-cert-manager-manifests.sh $(MANIFEST_SOURCE)
	hack/update-istio-csr-manifests.sh $(ISTIO_CSR_VERSION)
.PHONY: update-manifests

.PHONY: update
update: generate update-manifests update-bindata

.PHONY: update-with-container
update-with-container:
	$(CONTAINER_ENGINE) run -ti --rm -v $(PWD):/go/src/github.com/openshift/cert-manager-operator:z -w /go/src/github.com/openshift/cert-manager-operator $(CONTAINER_IMAGE_NAME) make update
	 
verify-scripts:
	hack/verify-deepcopy.sh
	hack/verify-clientgen.sh
	hack/verify-bundle.sh
.PHONY: verify-scripts

.PHONY: verify
verify: verify-scripts

.PHONY: verify-with-container
verify-with-container:
	$(CONTAINER_ENGINE) run -ti --rm -v $(PWD):/go/src/github.com/openshift/cert-manager-operator:z -w /go/src/github.com/openshift/cert-manager-operator $(CONTAINER_IMAGE_NAME) make verify

.PHONY: verify-deps
verify-deps:
	hack/verify-deps.sh

local-run: build
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
.PHONY: local-run


##@ Build
GO=GO111MODULE=on CGO_ENABLED=1 go

# Check for required tools
.PHONY: check-tools
check-tools:
	@command -v go >/dev/null 2>&1 || { echo "WARNING: go is not installed. Please install it to avoid issues."; }
	@command -v $(CONTAINER_ENGINE) >/dev/null 2>&1 || { echo "WARNING: $(CONTAINER_ENGINE) is not installed. Please install it to avoid issues."; }
	@command -v kubectl >/dev/null 2>&1 || { echo "WARNING: kubectl is not installed. Please install it to avoid issues."; }

build-operator: ## Build operator binary, no additional checks or code generation
	@GOFLAGS="-mod=vendor" source hack/go-fips.sh && $(GO) build $(GOBUILD_VERSION_ARGS) -o $(BIN)

build: check-tools generate fmt vet build-operator ## Build operator binary.

run: check-tools manifests generate fmt vet ## Run a controller from your host.
	go run $(PACKAGE)

image-build: check-tools ## Build container image with the operator.
	$(CONTAINER_ENGINE) build -t ${IMG} .

image-push: check-tools ## Push container image with the operator.
	$(CONTAINER_ENGINE) push ${IMG} ${CONTAINER_PUSH_ARGS}

##@ Deployment

deploy: manifests ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default | kubectl apply -f - 

undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/default | kubectl delete -f -

.PHONY: bundle
bundle: check-tools $(OPERATOR_SDK_BIN) manifests
	$(OPERATOR_SDK_BIN) generate kustomize manifests -q
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	$(KUSTOMIZE) build config/manifests | $(OPERATOR_SDK_BIN) generate bundle -q --version $(BUNDLE_VERSION) $(BUNDLE_METADATA_OPTS)
	$(OPERATOR_SDK_BIN) bundle validate $(BUNDLE_DIR)

.PHONY: bundle-image-build
bundle-image-build: check-tools bundle
	$(CONTAINER_ENGINE) build -t ${BUNDLE_IMG} -f bundle.Dockerfile .

.PHONY: bundle-image-push
bundle-image-push: check-tools
	$(CONTAINER_ENGINE) push ${BUNDLE_IMG}

.PHONY: index-image-build
index-image-build: check-tools opm
	$(OPM) index add -c $(CONTAINER_ENGINE) --bundles ${BUNDLE_IMG} --tag ${INDEX_IMG}

.PHONY: index-image-push
index-image-push: check-tools
	$(CONTAINER_ENGINE) push ${INDEX_IMG}

OPM=$(BIN_DIR)/opm
opm: ## Download opm locally if necessary.
	$(call get-bin,$(OPM),$(BIN_DIR),https://github.com/operator-framework/operator-registry/releases/download/$(OPM_VERSION)/linux-amd64-opm)

define get-bin
@[ -f "$(1)" ] || { \
	[ ! -d "$(2)" ] && mkdir -p "$(2)" || true ;\
	echo "Downloading $(3)" ;\
	curl -fL $(3) -o "$(1)" ;\
	chmod +x "$(1)" ;\
}
endef

.PHONY: test-e2e
test-e2e: test-e2e-wait-for-stable-state
	go test \
	-timeout $(E2E_TIMEOUT) \
	-count 1 \
	-v \
	-p 1 \
	-tags e2e \
	-run "$(TEST)" \
	./test/e2e \
	-ginkgo.label-filter=$(E2E_GINKGO_LABEL_FILTER)

test-e2e-wait-for-stable-state:
	@echo "---- Waiting for stable state ----"
	# This ensures the test-e2e-debug-cluster is called if a timeout is reached.
	oc wait --for=condition=Available=true deployment/cert-manager-cainjector -n cert-manager --timeout=120s || $(MAKE) test-e2e-debug-cluster
	oc wait --for=condition=Available=true deployment/cert-manager -n cert-manager --timeout=120s || $(MAKE) test-e2e-debug-cluster
	oc wait --for=condition=Available=true deployment/cert-manager-webhook -n cert-manager --timeout=120s || $(MAKE) test-e2e-debug-cluster
	@echo "---- /Waiting for stable state ----"
.PHONY: test-e2e-wait-for-stable-state

test-e2e-debug-cluster:
	@echo "---- Debugging the current state ----"
	- oc get pod -n cert-manager-operator
	- oc get pod -n cert-manager
	- oc get co
	- oc get csv --all-namespaces
	- oc get crd | grep -i cert
	- oc get subscriptions --all-namespaces
	- oc logs deployment/cert-manager-operator -n cert-manager-operator
	@echo "---- /Debugging the current state ----"
.PHONY: test-e2e-debug-cluster
 
.PHONY: lint
lint: 
	$(GOLANGCI_LINT) run --config .golangci.yaml	

$(GOLANGCI_LINT_BIN):
	mkdir -p $(BIN_DIR)
	hack/golangci-lint.sh $(GOLANGCI_LINT_BIN)

$(OPERATOR_SDK_BIN):
	mkdir -p $(BIN_DIR)
	hack/operator-sdk.sh $(OPERATOR_SDK_BIN)

$(HELM_BIN):
	mkdir -p $(BIN_DIR)
	hack/helm.sh $(HELM_BIN)

.PHONY: clean
clean:
	go clean
	rm -f $(BIN)
