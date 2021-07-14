GO_REQUIRED_MIN_VERSION = 1.16

MANIFEST_SOURCE := https://github.com/jetstack/cert-manager/releases/download/v1.4.0/cert-manager.yaml

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

CONTROLLER_GEN_VERSION :=v0.6.0

# $1 - target name
# $2 - apis
# $3 - manifests
# $4 - output
$(call add-crd-gen,operator-alpha,./apis/operator/v1alpha1,./bundle/cert-manager-operator/manifests,./bundle/cert-manager-operator/manifests)
$(call add-crd-gen,config-alpha,./apis/config/v1alpha1,./bundle/cert-manager-operator/manifests,./bundle/cert-manager-operator/manifests)

# generate bindata targets
$(call add-bindata,assets,./bindata/...,bindata,assets,pkg/operator/assets/bindata.go)

# generate image targets
IMAGE_REGISTRY :=registry.svc.ci.openshift.org
$(call build-image,cert-manager-operator,$(IMAGE_REGISTRY)/ocp/4.9:cert-manager-operator,./images/ci/Dockerfile,.)

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
update: update-scripts update-codegen-crds update-manifests update-crds

verify-scripts:
	hack/verify-deepcopy.sh
	hack/verify-clientgen.sh
.PHONY: verify-scripts
verify: verify-scripts verify-codegen-crds

local-deploy-manifests:
	kubectl apply -f ./manifests
	kubectl apply -f ./bundle/cert-manager-operator/manifests
.PHONY: local-deploy-manifests

local-run: local-deploy-manifests build
	./cert-manager-operator start --config=./hack/local-run-config.yaml --kubeconfig=$${KUBECONFIG:-$$HOME/.kube/config} --namespace=openshift-cert-manager-operator
.PHONY: local-run

local-clean:
	- oc delete namespace cert-manager
	- oc delete -f ./bindata/cert-manager-crds
.PHONY: local-clean

