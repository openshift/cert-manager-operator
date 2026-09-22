#!/bin/bash

set -e

# cleanup handled by trap
cleanup() {
  # cleanup created temp files
  rm -f _output/manifest.yaml _output/manifest_as_array.json _output/targets_as_map.json
}
trap cleanup EXIT

source "$(dirname "${BASH_SOURCE[0]}")/lib/init.sh"

CERT_MANAGER_VERSION=${1:?"missing cert-manager version. Please specify a version from https://github.com/cert-manager/cert-manager/releases"}
MANIFEST_SOURCE="https://github.com/cert-manager/cert-manager/releases/download/${CERT_MANAGER_VERSION}/cert-manager.yaml"

mkdir -p ./_output

echo "---- Downloading manifest file from $MANIFEST_SOURCE ----"
curl -NLs "$MANIFEST_SOURCE" -o ./_output/manifest.yaml

echo "---- Patching manifest ----"
# Upstream manifest includes yaml items in a single file as separate yaml documents.
# JSON cannot handle this so create one yaml document which includes an array of items instead.
./bin/yq \
    --output-format json \
    eval-all '. as $item ireduce ([]; . + $item)' \
    _output/manifest.yaml \
    >_output/manifest_as_array.json

# Patch manifest using jsonnet.
# This produces a map of patched target items having the filename as key and the patched item as value.
./bin/jsonnet \
    --tla-code-file manifest=_output/manifest_as_array.json \
    jsonnet/main.jsonnet \
    | ./bin/yq e '.' - \
    > _output/targets_as_map.json

# regenerate upstream-rendered operand bindata; preserve operator-owned manifests
# that are not present in the upstream cert-manager.yaml (metrics HTTPS RBAC).
OPERATOR_OWNED_DIR=metrics-dynamic-serving
mkdir -p "bindata/cert-manager-deployment/${OPERATOR_OWNED_DIR}"
shopt -s extglob nullglob
(
	cd bindata/cert-manager-deployment
	for item in !(${OPERATOR_OWNED_DIR}); do
		rm -rf -- "${item}"
	done
)
shopt -u extglob nullglob
# regenerate all cert manager crds
rm -rf config/crd/bases/*-crd.yaml

# Split the produced target items in separate files and convert back to yaml.
for file in $(./bin/yq eval --unwrapScalar 'keys | join(" ")' _output/targets_as_map.json)
do
    dir=$(dirname "${file}")
    mkdir -p "${dir}"
    echo "${file}"
    ./bin/yq \
        --output-format yaml --prettyPrint \
        eval ".[\"${file}\"]" _output/targets_as_map.json \
        > "${file}"
done

# Keep operator-owned extra RBAC labels aligned with the regenerated operand version.
OPERAND_VERSION=$(./bin/yq e '.metadata.labels."app.kubernetes.io/version"' bindata/cert-manager-deployment/controller/cert-manager-deployment.yaml)
if [ -n "${OPERAND_VERSION}" ] && [ "${OPERAND_VERSION}" != "null" ]; then
	for manifest in bindata/cert-manager-deployment/${OPERATOR_OWNED_DIR}/*.yaml; do
		./bin/yq e -i ".metadata.labels.\"app.kubernetes.io/version\" = \"${OPERAND_VERSION}\"" "${manifest}"
	done
fi
