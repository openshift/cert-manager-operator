GO_REQUIRED_MIN_VERSION = 1.17

RUNTIME?=docker

APP_NAME?=cert-manager-operator
IMAGE_REGISTRY?=registry.svc.ci.openshift.org
IMAGE_ORG?=openshift-cert-manager
IMAGE_TAG?=latest
IMAGE_OPERATOR?=$(IMAGE_REGISTRY)/$(IMAGE_ORG)/cert-manager-operator:$(IMAGE_TAG)
IMAGE_OPERATOR_BUNDLE?=$(IMAGE_REGISTRY)/$(IMAGE_ORG)/cert-manager-operator-bundle:$(IMAGE_TAG)

TEST_OPERATOR_NAMESPACE?=openshift-cert-manager-operator

MANIFEST_SOURCE = https://github.com/jetstack/cert-manager/releases/download/v1.7.1/cert-manager.yaml

OPERATOR_SDK_VERSION?=v1.12.0
OPERATOR_SDK?=$(PERMANENT_TMP_GOPATH)/bin/operator-sdk-$(OPERATOR_SDK_VERSION)
OPERATOR_SDK_DIR=$(dir $(OPERATOR_SDK))

# Include the library makefiles
include $(addprefix ./vendor/github.com/openshift/build-machinery-go/make/, \
	golang.mk \
	targets/openshift/bindata.mk \
	targets/openshift/images.mk \
	targets/openshift/imagebuilder.mk \
	targets/openshift/deps.mk \
	targets/openshift/operator/telepresence.mk \
	targets/openshift/operator/profile-manifests.mk \
	targets/openshift/crd-schema-gen.mk \
)


# $1 - target name
# $2 - apis
# $3 - manifests
# $4 - output
$(call add-crd-gen,operator-alpha,./apis/operator/v1alpha1,./bundle/manifests,./bundle/manifests)
$(call add-crd-gen,config-alpha,./apis/config/v1alpha1,./bundle/manifests,./bundle/manifests)

# generate bindata targets
$(call add-bindata,assets,./bindata/...,bindata,assets,pkg/operator/assets/bindata.go)

# generate image targets
$(call build-image,cert-manager-operator,$(IMAGE_OPERATOR),./images/ci/Dockerfile,.)
$(call build-image,cert-manager-operator-bundle,$(IMAGE_OPERATOR_BUNDLE),./bundle/bundle.Dockerfile,./bundle)

# exclude e2e test from unit tests
GO_TEST_PACKAGES :=./pkg/... ./cmd/...

# re-use test-unit target for e2e tests
.PHONY: test-e2e
test-e2e: GO_TEST_PACKAGES :=./test/e2e/...
test-e2e: test-unit

update-manifests:
	hack/update-cert-manager-manifests.sh $(MANIFEST_SOURCE)
.PHONY: update-manifests

update-scripts:
	hack/update-deepcopy.sh
	hack/update-clientgen.sh
.PHONY: update-scripts
update: update-scripts update-codegen-crds update-manifests update-bindata

verify-scripts:
	hack/verify-deepcopy.sh
	hack/verify-clientgen.sh
.PHONY: verify-scripts
verify: verify-scripts verify-codegen-crds

local-deploy-manifests:
	- kubectl create namespace openshift-cert-manager-operator
	kubectl apply $$(ls ./bundle/manifests/*crd.yaml | awk ' { print " -f " $$1 } ')
.PHONY: local-deploy-manifests

local-run:
	./cert-manager-operator start --config=./hack/local-run-config.yaml --kubeconfig=$${KUBECONFIG:-$$HOME/.kube/config} --namespace=openshift-cert-manager-operator
.PHONY: local-run

local-clean:
	- oc delete namespace cert-manager
	- oc delete -f ./bundle/manifests/
.PHONY: local-clean

operator-push-bundle: images
	$(RUNTIME) push $(IMAGE_OPERATOR)
	$(RUNTIME) push $(IMAGE_OPERATOR_BUNDLE)
.PHONY: operator-push-bundle

ensure-operator-sdk:
ifeq "" "$(wildcard $(OPERATOR_SDK))"
	$(info Installing Operator SDK into '$(OPERATOR_SDK)')
	mkdir -p '$(OPERATOR_SDK_DIR)'
	curl -L https://github.com/operator-framework/operator-sdk/releases/download/$(OPERATOR_SDK_VERSION)/operator-sdk_$(shell go env GOOS)_$(shell go env GOHOSTARCH) -o $(OPERATOR_SDK)
	chmod +x $(OPERATOR_SDK)
else
	$(info Using existing Operator SDK from "$(OPERATOR_SDK)")
endif
.PHONY: ensure-operator-sdk

operator-run-bundle: ensure-operator-sdk operator-push-bundle
	- kubectl create namespace $(TEST_OPERATOR_NAMESPACE)
	$(OPERATOR_SDK) run bundle $(IMAGE_OPERATOR_BUNDLE) --namespace $(TEST_OPERATOR_NAMESPACE)
.PHONY: operator-run-bundle

operator-clean:
	- kubectl delete namespace $(TEST_OPERATOR_NAMESPACE)
.PHONY: operator-clean