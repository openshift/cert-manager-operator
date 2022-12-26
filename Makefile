
# BUNDLE_VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the BUNDLE_VERSION as arg of the bundle target (e.g make bundle BUNDLE_VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export BUNDLE_VERSION=0.0.2)
BUNDLE_VERSION ?= 0.0.1

# CHANNELS define the bundle channels used in the bundle.
# Add a new line here if you would like to change its default config. (E.g CHANNELS = "candidate,fast,stable")
# To re-generate a bundle for other specific channels without changing the standard setup, you can:
# - use the CHANNELS as arg of the bundle target (e.g make bundle CHANNELS=candidate,fast,stable)
# - use environment variables to overwrite this value (e.g export CHANNELS="candidate,fast,stable")
CHANNELS = "alpha"
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif

# DEFAULT_CHANNEL defines the default channel used in the bundle.
# Add a new line here if you would like to change its default config. (E.g DEFAULT_CHANNEL = "stable")
# To re-generate a bundle for any other default channel without changing the default setup, you can:
# - use the DEFAULT_CHANNEL as arg of the bundle target (e.g make bundle DEFAULT_CHANNEL=stable)
# - use environment variables to overwrite this value (e.g export DEFAULT_CHANNEL="stable")
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

KUSTOMIZE := go run sigs.k8s.io/kustomize/kustomize/v4

K8S_ENVTEST_VERSION := 1.21.4

PACKAGE=github.com/openshift/cert-manager-operator

BIN=$(lastword $(subst /, ,$(PACKAGE)))
BIN_DIR=$(shell pwd)/bin

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

CONTAINER_ENGINE ?= docker
CONTAINER_PUSH_ARGS ?= $(if $(filter ${CONTAINER_ENGINE}, docker), , --tls-verify=${TLS_VERIFY})
TLS_VERIFY ?= true
CONTAINER_IMAGE_NAME ?= registry.ci.openshift.org/openshift/release:rhel-8-release-golang-1.17-openshift-4.10

BUNDLE_DIR := bundle
BUNDLE_MANIFEST_DIR := $(BUNDLE_DIR)/manifests
BUNDLE_IMG ?= olm-bundle:latest
INDEX_IMG ?= olm-bundle-index:latest
OPM_VERSION ?= v1.23.0

GOLANGCI_LINT_BIN=$(BIN_DIR)/golangci-lint

OPERATOR_SDK_BIN=$(BIN_DIR)/operator-sdk

COMMIT ?= $(shell git rev-parse HEAD)
SHORTCOMMIT ?= $(shell git rev-parse --short HEAD)
GOBUILD_VERSION_ARGS = -ldflags "-X $(PACKAGE)/pkg/version.SHORTCOMMIT=$(SHORTCOMMIT) -X $(PACKAGE)/pkg/version.COMMIT=$(COMMIT)"

E2E_TIMEOUT ?= 1h

MANIFEST_SOURCE = https://github.com/cert-manager/cert-manager/releases/download/v1.9.1/cert-manager.yaml


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
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

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

update-manifests:
	hack/update-cert-manager-manifests.sh $(MANIFEST_SOURCE)
.PHONY: update-manifests

update-scripts:
	hack/update-deepcopy.sh
	hack/update-clientgen.sh
.PHONY: update-scripts

.PHONY: update
update: update-scripts update-manifests update-bindata

.PHONY: update-with-container
update-with-container:
	$(CONTAINER_ENGINE) run -ti --rm -v $(PWD):/go/src/github.com/openshift/cert-manager-operator:z -w /go/src/github.com/openshift/cert-manager-operator $(CONTAINER_IMAGE_NAME) make update
	 
verify-scripts:
	hack/verify-deepcopy.sh
	hack/verify-clientgen.sh
.PHONY: verify-scripts

.PHONY: verify
verify: verify-scripts

.PHONY: verify-deps
verify-deps:
	hack/verify-deps.sh

local-run: build
	./cert-manager-operator start --config=./hack/local-run-config.yaml --kubeconfig=$${KUBECONFIG:-$$HOME/.kube/config} --namespace=openshift-cert-manager-operator
.PHONY: local-run

##@ Build
GO=GO111MODULE=on GOFLAGS=-mod=vendor CGO_ENABLED=0 go

build-operator: ## Build operator binary, no additional checks or code generation
	$(GO) build $(GOBUILD_VERSION_ARGS) -o $(BIN)

build: generate fmt vet build-operator ## Build operator binary.

run: manifests generate fmt vet ## Run a controller from your host.
	go run $(PACKAGE)

image-build: ## Build container image with the operator.
	$(CONTAINER_ENGINE) build -t ${IMG} .

image-push: ## Push container image with the operator.
	$(CONTAINER_ENGINE) push ${IMG} ${CONTAINER_PUSH_ARGS}

##@ Deployment

deploy: manifests ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default | kubectl apply -f - 

undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/default | kubectl delete -f -

.PHONY: bundle
bundle: $(OPERATOR_SDK_BIN) manifests
	$(OPERATOR_SDK_BIN) generate kustomize manifests -q
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	$(KUSTOMIZE) build config/manifests | $(OPERATOR_SDK_BIN) generate bundle -q --version $(BUNDLE_VERSION) $(BUNDLE_METADATA_OPTS)
	$(OPERATOR_SDK_BIN) bundle validate $(BUNDLE_DIR)

.PHONY: bundle-image-build
bundle-image-build: bundle
	$(CONTAINER_ENGINE) build -t ${BUNDLE_IMG} -f bundle.Dockerfile .

.PHONY: bundle-image-push
bundle-image-push:
	$(CONTAINER_ENGINE) push ${BUNDLE_IMG}

.PHONY: index-image-build
index-image-build: opm
	$(OPM) index add -c $(CONTAINER_ENGINE) --bundles ${BUNDLE_IMG} --tag ${INDEX_IMG}

.PHONY: index-image-push
index-image-push:
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
	./test/e2e

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

clean:
	$(GO) clean
	rm -f $(BIN)

# GO_REQUIRED_MIN_VERSION = 1.17
# GO_TEST_FLAGS=-v
# RUNTIME?=docker
#
# APP_NAME?=cert-manager-operator
# IMAGE_REGISTRY?=registry.svc.ci.openshift.org
# IMAGE_ORG?=openshift-cert-manager
# IMAGE_TAG?=latest
# IMAGE_OPERATOR?=$(IMAGE_REGISTRY)/$(IMAGE_ORG)/cert-manager-operator:$(IMAGE_TAG)
# IMAGE_OPERATOR_BUNDLE?=$(IMAGE_REGISTRY)/$(IMAGE_ORG)/cert-manager-operator-bundle:$(IMAGE_TAG)
#
# TEST_OPERATOR_NAMESPACE?=openshift-cert-manager-operator
#
# MANIFEST_SOURCE = https://github.com/cert-manager/cert-manager/releases/download/v1.9.1/cert-manager.yaml
#
# OPERATOR_SDK_VERSION?=v1.12.0
# OPERATOR_SDK?=$(PERMANENT_TMP_GOPATH)/bin/operator-sdk-$(OPERATOR_SDK_VERSION)
# OPERATOR_SDK_DIR=$(dir $(OPERATOR_SDK))

# Include the library makefiles
#
#
# # $1 - target name
# # $2 - apis
# # $3 - manifests
# # $4 - output
# $(call add-crd-gen,operator-alpha,./apis/operator/v1alpha1,./bundle/manifests,./bundle/manifests)
# $(call add-crd-gen,config-alpha,./apis/config/v1alpha1,./bundle/manifests,./bundle/manifests)
#
#
# # generate image targets
# $(call build-image,cert-manager-operator,$(IMAGE_OPERATOR),./images/ci/Dockerfile,.)
# $(call build-image,cert-manager-operator-bundle,$(IMAGE_OPERATOR_BUNDLE),./bundle/bundle.Dockerfile,./bundle)
#

#
# update-manifests:
# 	hack/update-cert-manager-manifests.sh $(MANIFEST_SOURCE)
# .PHONY: update-manifests
#
#
# local-deploy-manifests:
# 	- kubectl create namespace openshift-cert-manager-operator
# 	kubectl apply $$(ls ./bundle/manifests/*crd.yaml | awk ' { print " -f " $$1 } ')
# .PHONY: local-deploy-manifests
#
#
# local-clean:
# 	- oc delete namespace cert-manager
# 	- oc delete -f ./bundle/manifests/
# .PHONY: local-clean
#
# operator-push-bundle: images
# 	$(RUNTIME) push $(IMAGE_OPERATOR)
# 	$(RUNTIME) push $(IMAGE_OPERATOR_BUNDLE)
# .PHONY: operator-push-bundle
#
# ensure-operator-sdk:
# ifeq "" "$(wildcard $(OPERATOR_SDK))"
# 	$(info Installing Operator SDK into '$(OPERATOR_SDK)')
# 	mkdir -p '$(OPERATOR_SDK_DIR)'
# 	curl -L https://github.com/operator-framework/operator-sdk/releases/download/$(OPERATOR_SDK_VERSION)/operator-sdk_$(shell go env GOOS)_$(shell go env GOHOSTARCH) -o $(OPERATOR_SDK)
# 	chmod +x $(OPERATOR_SDK)
# else
# 	$(info Using existing Operator SDK from "$(OPERATOR_SDK)")
# endif
# .PHONY: ensure-operator-sdk
#
# operator-run-bundle: ensure-operator-sdk operator-push-bundle
# 	- kubectl create namespace $(TEST_OPERATOR_NAMESPACE)
# 	$(OPERATOR_SDK) run bundle $(IMAGE_OPERATOR_BUNDLE) --namespace $(TEST_OPERATOR_NAMESPACE)
# .PHONY: operator-run-bundle
#
# operator-clean:
# 	- kubectl delete namespace $(TEST_OPERATOR_NAMESPACE)
# .PHONY: operator-clean
