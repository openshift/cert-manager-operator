#!/bin/bash

set -e

ISTIO_CSR_VERSION=${1:-}

echo "updating istio-csr manifests"

if [ ! -f ./_output/tools/bin/yq ]; then
    echo "---- Installing yq tooling ----"

    mkdir -p ./_output/tools/bin
    curl -s -f -L https://github.com/mikefarah/yq/releases/download/v4.13.3/yq_$(go env GOHOSTOS)_$(go env GOHOSTARCH) -o ./_output/tools/bin/yq
    chmod +x ./_output/tools/bin/yq
fi

./bin/helm repo add jetstack https://charts.jetstack.io --force-update
./bin/helm template cert-manager-istio-csr jetstack/cert-manager-istio-csr -n cert-manager --version "${ISTIO_CSR_VERSION}" > _output/istio-csr-manifest.yaml

./_output/tools/bin/yq \
    --output-format json \
    eval-all '. as $item ireduce ([]; . + $item)' \
    _output/manifest.yaml \
    >_output/manifest_as_array.json

# remove the helm label
./_output/tools/bin/yq e 'del(.metadata.labels."app.kubernetes.io/managed-by") | del(.metadata.labels."helm.sh/chart")' -i _output/istio-csr-manifest.yaml
# remove the helm label from deployment.spec.template.metadata also
./_output/tools/bin/yq e 'del(.spec.template.metadata.labels."app.kubernetes.io/managed-by") | del(.spec.template.metadata.labels."helm.sh/chart")' -i _output/istio-csr-manifest.yaml

./_output/tools/bin/yq e '.metadata.labels."app.kubernetes.io/managed-by" = "cert-manager-operator"' -i  _output/istio-csr-manifest.yaml

./_output/tools/bin/yq \
    --output-format json \
    eval-all '. as $item ireduce ([]; . + $item)' \
    _output/istio-csr-manifest.yaml \
    > _output/istio_manifest_as_array.json

# regenerate all bindata
rm -rf bindata/istio-csr

mkdir -p bindata/istio-csr

./_output/tools/bin/yq --output-format json \
    eval-all '.[]' -I 0 \
    _output/istio_manifest_as_array.json | while read -r item; do

  name=$(echo "$item" | ./_output/tools/bin/yq eval '.metadata.name' -)
  kind=$(echo "$item" | ./_output/tools/bin/yq eval '.kind' - | tr '[:upper:]' '[:lower:]')

  # skip unused manifests
  if [[ "${name}-${kind}" == "cert-manager-istio-csr-metrics-service" || \
        "${name}-${kind}" == "cert-manager-istio-csr-dynamic-istiod-rolebinding" \
    ]]; then
    
    continue
  fi

  output_file="bindata/istio-csr/${name}-${kind}.yaml"

  echo "$item" | ./_output/tools/bin/yq eval -P > "$output_file"
  echo "$output_file"
done
