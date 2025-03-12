#!/bin/bash

set -e

source "$(dirname "${BASH_SOURCE}")/lib/init.sh"
source "$(dirname "${BASH_SOURCE}")/lib/yq.sh"

MANIFEST_SOURCE=${1:?"missing Cert Manager manifest url. You can use either http:// or file://"}

mkdir -p ./_output

echo "---- Downloading manifest file from $MANIFEST_SOURCE ----"
curl -NLs "$MANIFEST_SOURCE" -o ./_output/manifest.yaml

go install ./vendor/github.com/google/go-jsonnet/cmd/jsonnet

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
jsonnet \
    --tla-code-file manifest=_output/manifest_as_array.json \
    jsonnet/main.jsonnet \
    | ./bin/yq e '.' - \
    > _output/targets_as_map.json

# regenerate all bindata
rm -rf bindata/cert-manager-deployment
# regenerate all cert manager crds
rm -rf config/crd/bases/*-crd.yaml

# Split the produced target items in separate files and convert back to yaml.
for file in $(./bin/yq eval 'keys | join(" ")' _output/targets_as_map.json)
do
    dir=$(dirname "${file}")
    mkdir -p "${dir}"
    echo "${file}"
    ./bin/yq \
        --output-format yaml --prettyPrint \
        eval ".[\"${file}\"]" _output/targets_as_map.json \
        > "${file}"
done
