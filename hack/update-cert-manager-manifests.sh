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

# regenerate all bindata
rm -rf bindata/cert-manager-deployment
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

echo "---- Patching accessKeyIDSecretRef CRD descriptions ----"
# Upstream cert-manager incorrectly documents accessKeyIDSecretRef as SecretAccessKey.
for crd in config/crd/bases/challenges.acme.cert-manager.io-crd.yaml \
           config/crd/bases/issuers.cert-manager.io-crd.yaml \
           config/crd/bases/clusterissuers.cert-manager.io-crd.yaml; do
    if [[ -f "$crd" ]]; then
        sed -i \
            's/The SecretAccessKey is used for authentication\. If set, pull the AWS/The AccessKeyID is used for authentication. If set, pull the AWS/' \
            "$crd"
    fi
done

echo "---- Patching Venafi cloud/tpp CRD descriptions ----"
# Upstream cert-manager does not clearly state that cloud and tpp are mutually exclusive.
for crd in config/crd/bases/issuers.cert-manager.io-crd.yaml \
           config/crd/bases/clusterissuers.cert-manager.io-crd.yaml; do
    if [[ -f "$crd" ]]; then
        sed -i \
            's/Only one of CyberArk Certificate Manager may be specified\./Only one of cloud or tpp mode may be specified at the same time./' \
            "$crd"
    fi
done
