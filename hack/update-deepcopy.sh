#!/bin/bash

source "$(dirname "${BASH_SOURCE}")/lib/init.sh"

SCRIPT_ROOT=$(dirname ${BASH_SOURCE})/..
CODEGEN_PKG=${CODEGEN_PKG:-$(cd ${SCRIPT_ROOT}; ls -d -1 ./vendor/k8s.io/code-generator 2>/dev/null || echo ../../../k8s.io/code-generator)}

verify="${VERIFY:-}"

for group in ${API_GROUP_VERSIONS}; do
  GOFLAGS="" bash ${CODEGEN_PKG}/kube_codegen.sh "deepcopy" \
    github.com/openshift/cert-manager-operator/api \
    github.com/openshift/cert-manager-operator/api \
    "${group/\//:}" \
    --go-header-file ${SCRIPT_ROOT}/hack/empty.txt \
    --output-base ../../.. \
    ${verify}
done
