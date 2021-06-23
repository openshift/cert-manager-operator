#!/bin/bash

source "$(dirname "${BASH_SOURCE}")/lib/init.sh"

SCRIPT_ROOT=$(dirname ${BASH_SOURCE})/..
CODEGEN_PKG=${CODEGEN_PKG:-$(cd ${SCRIPT_ROOT}; ls -d -1 ./vendor/k8s.io/code-generator 2>/dev/null || echo ../../../k8s.io/code-generator)}

verify="${VERIFY:-}"

for group in operator; do
  bash ${CODEGEN_PKG}/generate-groups.sh "client,lister,informer" \
    github.com/openshift/cert-manager-operator/pkg/${group} \
    github.com/openshift/cert-manager-operator/api \
    "${group}:v1alpha1" \
    --go-header-file ${SCRIPT_ROOT}/hack/empty.txt \
    --plural-exceptions=DNS:DNSes,DNSList:DNSList,Endpoints:Endpoints,Features:Features,FeaturesList:FeaturesList,SecurityContextConstraints:SecurityContextConstraints \
    ${verify}
done
