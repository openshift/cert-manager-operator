GO_REQUIRED_MIN_VERSION = 1.17
RUNTIME?=docker

APP_NAME?=cert-manager-operator
IMAGE_REGISTRY?=registry.svc.ci.openshift.org
IMAGE_ORG?=openshift-cert-manager
IMAGE_TAG?=latest
IMAGE_OPERATOR?=$(IMAGE_REGISTRY)/$(IMAGE_ORG)/cert-manager-operator:$(IMAGE_TAG)
IMAGE_OPERATOR_BUNDLE?=$(IMAGE_REGISTRY)/$(IMAGE_ORG)/cert-manager-operator-bundle:$(IMAGE_TAG)

TESTS_IMAGE_REGISTRY?=quay.io
TESTS_IMAGE_ORG?=ytripath
TESTS_IMAGE_TAG?=$(IMAGE_TAG)
TESTS_IMAGE?=$(TESTS_IMAGE_REGISTRY)/$(TESTS_IMAGE_ORG)/cert-manager-operator-scorecard-test:$(TESTS_IMAGE_TAG)
TESTS_WAIT_TIME?=600s
TESTS_NAMESPACE?=cert-manager-tests
TESTS_SA?=cert-manager-operator-e2e-test-runner
TEST_OPERATOR_NAMESPACE?=openshift-cert-manager-operator

MANIFEST_SOURCE = https://github.com/cert-manager/cert-manager/releases/download/v1.8.0/cert-manager.yaml

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

# generate scorecard image targets
.PHONY: build/custom-scorecard-tests
build/custom-scorecard-tests:
	go build -mod=vendor $(GO_GCFLAGS) $(GO_ASMFLAGS) -o $(BUILD_DIR)/$(@F) ./test

.PHONY: build-e2e-image
build-e2e-image:
	docker build -f ./images/custom-scorecard-tests/Dockerfile -t $(TESTS_IMAGE) ./

.PHONY: push-e2e-image
push-e2e-image: build-e2e-image
	docker push $(TESTS_IMAGE)

.PHONY: run-e2e-image
run-e2e-image:
	IMAGE=${TESTS_IMAGE} envsubst < bundle/tests/scorecard/base.yaml > bundle/tests/scorecard/config.yaml 
	oc project $(TESTS_NAMESPACE) || oc new-project $(TESTS_NAMESPACE)
	oc get sa $(TESTS_SA) -n $(TESTS_NAMESPACE) || oc create sa $(TESTS_SA) -n $(TESTS_NAMESPACE)
	oc adm policy add-cluster-role-to-user cluster-admin system:serviceaccount:$(TESTS_NAMESPACE):$(TESTS_SA)
	operator-sdk scorecard ./bundle --namespace=$(TESTS_NAMESPACE) --wait-time=$(TESTS_WAIT_TIME) -o json || true	
	oc delete project $(TESTS_NAMESPACE)

# exclude e2e test from unit tests
GO_TEST_PACKAGES :=./pkg/... ./cmd/...

test-e2e: 
	$(MAKE) 'BUILD_DIR=.' build/custom-scorecard-tests
	./custom-scorecard-tests --bundle ./bundle all
.PHONY: test-e2e

test-e2e-wait-for-stable-state:
	@echo "---- Waiting for stable state ----"
	# This ensures the test-e2e-debug-cluster is called if a timeout is reached.
	oc wait --for=condition=Available=true deployment/cert-manager-cainjector -n openshift-cert-manager --timeout=120s || $(MAKE) test-e2e-debug-cluster
	oc wait --for=condition=Available=true deployment/cert-manager-controller -n openshift-cert-manager --timeout=120s || $(MAKE) test-e2e-debug-cluster
	oc wait --for=condition=Available=true deployment/cert-manager-webhook -n openshift-cert-manager --timeout=120s || $(MAKE) test-e2e-debug-cluster
	@echo "---- /Waiting for stable state ----"
.PHONY: test-e2e-wait-for-stable-state

test-e2e-debug-cluster:
	@echo "---- Debugging the current state ----"
	- oc get pod -n openshift-cert-manager-operator
	- oc get pod -n openshift-cert-manager
	- oc get co
	- oc get csv --all-namespaces
	- oc get crd | grep -i cert
	- oc get subscriptions --all-namespaces
	- oc logs deployment/cert-manager-operator -n openshift-cert-manager-operator
	@echo "---- /Debugging the current state ----"
.PHONY: test-e2e-debug-cluster

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

local-run: build
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
