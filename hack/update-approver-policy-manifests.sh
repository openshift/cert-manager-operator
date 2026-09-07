#!/bin/bash

set -e

# cleanup handled by trap
cleanup() {
  # cleanup created temp files
  rm -rf _output/manifests
}
trap cleanup EXIT

source "$(dirname "${BASH_SOURCE[0]}")/lib/init.sh"

APPROVER_POLICY_VERSION=${1:?"missing approver-policy version. Please specify a version from https://github.com/cert-manager/approver-policy/releases"}
MANIFESTS_PATH=./_output/manifests

mkdir -p ${MANIFESTS_PATH}

echo "---- Downloading approver-policy manifests ${APPROVER_POLICY_VERSION} ----"

# The approver-policy chart is published to charts.jetstack.io as cert-manager-approver-policy.
# Ref: https://cert-manager.io/docs/policy/approval/approver-policy/installation/
./bin/helm repo add cert-manager https://charts.jetstack.io --force-update
./bin/helm template cert-manager-approver-policy cert-manager/cert-manager-approver-policy \
    -n cert-manager \
    --version "${APPROVER_POLICY_VERSION}" \
    --set app.metrics.service.servicemonitor.enabled=true \
    > ${MANIFESTS_PATH}/manifests.yaml

echo "---- Patching manifest ----"

# remove non-essential helm-specific labels from resource metadata and pod templates
./bin/yq e 'del(.metadata.labels."helm.sh/chart")' -i ${MANIFESTS_PATH}/manifests.yaml
./bin/yq e 'del(.spec.template.metadata.labels."helm.sh/chart")' -i ${MANIFESTS_PATH}/manifests.yaml

# update all occurrences of standard labels using recursive descent
# this finds and updates labels wherever they appear (metadata.labels, spec.template.metadata.labels,
# spec.selector.matchLabels, etc.)
./bin/yq e '(.. | select(has("app.kubernetes.io/managed-by"))."app.kubernetes.io/managed-by") = "cert-manager-operator"' -i ${MANIFESTS_PATH}/manifests.yaml
./bin/yq e '(.. | select(has("app.kubernetes.io/name"))."app.kubernetes.io/name") = "cert-manager-approver-policy"' -i ${MANIFESTS_PATH}/manifests.yaml
./bin/yq e '(.. | select(has("app.kubernetes.io/instance"))."app.kubernetes.io/instance") = "cert-manager-approver-policy"' -i ${MANIFESTS_PATH}/manifests.yaml
./bin/yq e '(.. | select(has("app"))."app") = "cert-manager-approver-policy"' -i ${MANIFESTS_PATH}/manifests.yaml

# add app.kubernetes.io/part-of to all labels objects (wherever app.kubernetes.io/name exists)
./bin/yq e '(.. | select(has("app.kubernetes.io/name"))."app.kubernetes.io/part-of") = "cert-manager-operator"' -i ${MANIFESTS_PATH}/manifests.yaml

# regenerate all bindata
rm -rf bindata/approver-policy/resources
rm -f config/crd/bases/customresourcedefinition_certificaterequestpolicies.policy.cert-manager.io.yml

# split into individual manifest files
./bin/yq e '... comments=""' -s '"_output/manifests/" + .kind + "_" + .metadata.name + ".yml" | downcase' ${MANIFESTS_PATH}/manifests.yaml

# The CertificateRequestPolicy CRD is included in the operator OLM bundle and installed by OLM at
# operator installation time.
mv ${MANIFESTS_PATH}/customresourcedefinition_* config/crd/bases/

# Move remaining operand manifests to bindata — these are applied by the approver-policy-controller
# when the ApproverPolicy CR is created.
mkdir -p bindata/approver-policy/resources
mv ${MANIFESTS_PATH}/*.yml bindata/approver-policy/resources
