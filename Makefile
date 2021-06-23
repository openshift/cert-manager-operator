GO_REQUIRED_MIN_VERSION = 1.16

# Include the library makefiles
include $(addprefix ./vendor/github.com/openshift/build-machinery-go/make/, \
	golang.mk \
	targets/openshift/bindata.mk \
	targets/openshift/images.mk \
	targets/openshift/deps.mk \
	targets/openshift/operator/telepresence.mk \
	targets/openshift/operator/profile-manifests.mk \
	targets/openshift/crd-schema-gen.mk \
)

CONTROLLER_GEN_VERSION :=v0.2.5

# $1 - target name
# $2 - apis
# $3 - manifests
# $4 - output
$(call add-crd-gen,operator-alpha,./api/operator/v1alpha1,./bundle/cert-manager-operator/manifests,./bundle/cert-manager-operator/manifests)

# generate bindata targets
$(call add-bindata,assets,./bindata/...,bindata,assets,pkg/operator/assets/bindata.go)

# generate image targets
IMAGE_REGISTRY :=registry.svc.ci.openshift.org
$(call build-image,cert-manager-operator,$(IMAGE_REGISTRY)/ocp/4.8:cert-manager-operator,./images/ci/Dockerfile,.)

# exclude e2e test from unit tests
GO_TEST_PACKAGES :=./pkg/... ./cmd/...

# re-use test-unit target for e2e tests
.PHONY: test-e2e
test-e2e: GO_TEST_PACKAGES :=./test/e2e/...
test-e2e: test-unit

update-scripts:
	hack/update-deepcopy.sh
	hack/update-clientgen.sh
.PHONY: update-scripts
update: update-scripts update-codegen-crds

verify-scripts:
	hack/verify-deepcopy.sh
	hack/verify-clientgen.sh
.PHONY: verify-scripts
verify: verify-scripts verify-codegen-crds
