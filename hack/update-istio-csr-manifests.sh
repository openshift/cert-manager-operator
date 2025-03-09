#!/bin/bash

set -e

source "$(dirname "${BASH_SOURCE}")/lib/init.sh"
source "$(dirname "${BASH_SOURCE}")/lib/yq.sh"

ISTIO_CSR_VERSION=${1:?"missing istio-csr version. Please specify a version from https://github.com/cert-manager/istio-csr/releases"}

mkdir -p ./_output

echo "---- Downloading istio-csr manifests ${ISTIO_CSR_VERSION} ----"

./bin/helm repo add jetstack https://charts.jetstack.io --force-update
./bin/helm template cert-manager-istio-csr jetstack/cert-manager-istio-csr \
    -n cert-manager --version "${ISTIO_CSR_VERSION}" > _output/istio-csr-manifest.yaml

echo "---- Patching manifest ----"

# remove the helm specific labels from .metadata.labels and .spec.template.metadata.labels
./bin/yq e 'del(.metadata.labels."helm.sh/chart")' -i _output/istio-csr-manifest.yaml
./bin/yq e 'del(.spec.template.metadata.labels."helm.sh/chart")' -i _output/istio-csr-manifest.yaml
./bin/yq e 'del(.spec.template.metadata.labels."app.kubernetes.io/managed-by")' -i _output/istio-csr-manifest.yaml

# update all occurences of app.kubernetes.io/managed-by label value.
./bin/yq e \
  '(.[][] | select(has("app.kubernetes.io/managed-by"))."app.kubernetes.io/managed-by") |= "cert-manager-operator"' \
  -i _output/istio-csr-manifest.yaml

# regenerate all bindata
rm -rf bindata/istio-csr
mkdir -p bindata/istio-csr

# split into individual manifest files
./bin/yq --output-format json \
    eval-all '.' -I 0 \
    _output/istio-csr-manifest.yaml | while read -r item; do

  name=$(echo "$item" | ./bin/yq eval '.metadata.name' -)
  kind=$(echo "$item" | ./bin/yq eval '.kind' - | tr '[:upper:]' '[:lower:]')

  # skip unused manifests
  if [[ "${name}-${kind}" == "cert-manager-istio-csr-metrics-service" || \
        "${name}-${kind}" == "cert-manager-istio-csr-dynamic-istiod-rolebinding" \
  ]]; then
    
    continue
  fi
  
  output_file="bindata/istio-csr/${name}-${kind}.yaml"

  echo "$item" | ./bin/yq eval -P > "$output_file"
  echo "$output_file"
done
