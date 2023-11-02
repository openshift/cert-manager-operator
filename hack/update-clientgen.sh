#!/bin/bash

# This script is inspired by https://github.com/openshift/client-go/blob/master/hack/update-codegen.sh

source "$(dirname "${BASH_SOURCE}")/lib/init.sh"

SCRIPT_ROOT=$(dirname ${BASH_SOURCE})/..
CODEGEN_PKG=${CODEGEN_PKG:-$(cd ${SCRIPT_ROOT}; ls -d -1 ./vendor/k8s.io/code-generator 2>/dev/null || echo ../../../k8s.io/code-generator)}

verify="${VERIFY:-}"

set -x
# Because go mod sux, we have to fake the vendor for generator in order to be able to build it...
mv ${CODEGEN_PKG}/kube_codegen.sh ${CODEGEN_PKG}/kube_codegen.sh.orig
sed 's/  GO111MODULE=on go install/  #GO111MODULE=on go install/g' ${CODEGEN_PKG}/kube_codegen.sh.orig > ${CODEGEN_PKG}/kube_codegen.sh
function cleanup {
  mv ${CODEGEN_PKG}/kube_codegen.sh.orig ${CODEGEN_PKG}/kube_codegen.sh
}
trap cleanup EXIT

go install ./${CODEGEN_PKG}/cmd/{defaulter-gen,client-gen,lister-gen,informer-gen,deepcopy-gen}

for group in ${API_GROUP_VERSIONS}; do
  bash ${CODEGEN_PKG}/kube_codegen.sh "client,lister,informer" \
    github.com/openshift/cert-manager-operator/pkg/"${group%\/*}" \
    github.com/openshift/cert-manager-operator/api \
    "${group/\//:}" \
    --go-header-file ${SCRIPT_ROOT}/hack/empty.txt \
    --plural-exceptions=DNS:DNSes,DNSList:DNSList,Endpoints:Endpoints,Features:Features,FeaturesList:FeaturesList,SecurityContextConstraints:SecurityContextConstraints \
    --output-base ../../.. \
    ${verify}
done
