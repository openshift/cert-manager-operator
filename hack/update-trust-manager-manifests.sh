#!/bin/bash

set -e

# cleanup handled by trap
cleanup() {
  # cleanup created temp files
  rm -rf _output/manifests
}
trap cleanup EXIT

source "$(dirname "${BASH_SOURCE[0]}")/lib/init.sh"

TRUST_MANAGER_VERSION=${1:?"missing trust-manager version. Please specify a version from https://github.com/cert-manager/trust-manager/releases"}
MANIFESTS_PATH=./_output/manifests

mkdir -p ${MANIFESTS_PATH}

echo "---- Downloading trust-manager manifests ${TRUST_MANAGER_VERSION} ----"

./bin/helm repo add cert-manager https://charts.jetstack.io --force-update
./bin/helm template trust-manager cert-manager/trust-manager -n trust-manager \
  --version "${TRUST_MANAGER_VERSION}" \
  --set defaultPackage.enabled=false \
  --set namespace=cert-manager \
  > ${MANIFESTS_PATH}/manifests.yaml

echo "---- Patching manifest ----"

# remove non-essential fields from each resource manifests.
./bin/yq e 'del(.metadata.labels."helm.sh/chart")' -i ${MANIFESTS_PATH}/manifests.yaml
./bin/yq e 'del(.spec.template.metadata.labels."helm.sh/chart")' -i ${MANIFESTS_PATH}/manifests.yaml

# update all occurrences of standard labels using recursive descent
# this finds and updates labels wherever they appear (metadata.labels, spec.template.metadata.labels, spec.selector.matchLabels, etc.)
./bin/yq e '(.. | select(has("app.kubernetes.io/managed-by"))."app.kubernetes.io/managed-by") = "cert-manager-operator"' -i ${MANIFESTS_PATH}/manifests.yaml
./bin/yq e '(.. | select(has("app.kubernetes.io/name"))."app.kubernetes.io/name") = "cert-manager-trust-manager"' -i ${MANIFESTS_PATH}/manifests.yaml
./bin/yq e '(.. | select(has("app.kubernetes.io/instance"))."app.kubernetes.io/instance") = "cert-manager-trust-manager"' -i ${MANIFESTS_PATH}/manifests.yaml
./bin/yq e '(.. | select(has("app"))."app") = "cert-manager-trust-manager"' -i ${MANIFESTS_PATH}/manifests.yaml

# add app.kubernetes.io/part-of to all labels objects (wherever app.kubernetes.io/name exists)
./bin/yq e '(.. | select(has("app.kubernetes.io/name"))."app.kubernetes.io/part-of") = "cert-manager-operator"' -i ${MANIFESTS_PATH}/manifests.yaml

# regenerate all bindata
rm -rf bindata/trust-manager/resources
rm -f config/crd/bases/customresourcedefinition_bundles.trust.cert-manager.io.yml

# split into individual manifest files
./bin/yq e '... comments=""' -s '"_output/manifests/" + .kind + "_" + .metadata.name + ".yml" | downcase' ${MANIFESTS_PATH}/manifests.yaml

# Move resource manifests to appropriate location
mkdir -p bindata/trust-manager/resources
mv ${MANIFESTS_PATH}/customresourcedefinition_* config/crd/bases/
mv ${MANIFESTS_PATH}/*.yml bindata/trust-manager/resources
