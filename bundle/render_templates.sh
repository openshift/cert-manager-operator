#!/usr/bin/env bash

set -x

declare MANIFESTS_DIR
declare METADATA_DIR
declare CERT_MANAGER_OPERATOR_IMAGE
declare CERT_MANAGER_WEBHOOK_IMAGE
declare CERT_MANAGER_CA_INJECTOR_IMAGE
declare CERT_MANAGER_CONTROLLER_IMAGE
declare CERT_MANAGER_ACMESOLVER_IMAGE
declare KUBE_RBAC_PROXY_IMAGE

CSV_FILE_NAME="cert-manager-operator.clusterserviceversion.yaml"
ANNOTATIONS_FILE_NAME="annotations.yaml"

update_csv_manifest()
{
  CSV_FILE="${MANIFESTS_DIR}/${CSV_FILE_NAME}"
  if [[ ! -f ${CSV_FILE} ]]; then
    echo "[$(date)] -- ERROR -- operator csv file \"${CSV_FILE}\" does not exist"
    exit 1
  fi

  ## replace cert-manager operand related images
  sed -i "s#quay.io/jetstack/cert-manager-webhook.*#${CERT_MANAGER_WEBHOOK_IMAGE}#g" ${CSV_FILE}
  sed -i "s#quay.io/jetstack/cert-manager-controller.*#${CERT_MANAGER_CONTROLLER_IMAGE}#g" ${CSV_FILE}
  sed -i "s#quay.io/jetstack/cert-manager-cainjector.*#${CERT_MANAGER_CA_INJECTOR_IMAGE}#g" ${CSV_FILE}
  sed -i "s#quay.io/jetstack/cert-manager-acmesolver.*#${CERT_MANAGER_ACMESOLVER_IMAGE}#g" ${CSV_FILE}

  ## replace kube-rbac-proxy image
  sed -i "s#gcr.io/kubebuilder/kube-rbac-proxy.*#${KUBE_RBAC_PROXY_IMAGE}#g" ${CSV_FILE}

  ## replace cert-manager-operator image
  sed -i "s#openshift.io/cert-manager-operator.*#${CERT_MANAGER_OPERATOR_IMAGE}#g" ${CSV_FILE}

  ## add annotations
  yq e -i ".metadata.annotations.createdAt=\"$(date -u +'%Y-%m-%dT%H:%M:%S')\"" "${CSV_FILE}"

  ## remove non-required fields
  yq -i 'del(.spec.relatedImages)' "${CSV_FILE}"
}

update_annotations_metadata() {
  ANNOTATION_FILE="${METADATA_DIR}/${ANNOTATIONS_FILE_NAME}"
  if [[ ! -f ${ANNOTATION_FILE} ]]; then
    echo "[$(date)] -- ERROR -- annotations metadata file \"${CSV_FILE}\" does not exist"
    exit 1
  fi

  # add annotations
  yq e -i '.annotations."operators.operatorframework.io.bundle.package.v1"="openshift-cert-manager-operator"' "${ANNOTATION_FILE}"
}

usage()
{
  echo -e "$(basename $BASH_SOURCE) <MANIFESTS_DIR> <METADATA_DIR> <CERT_MANAGER_OPERATOR_IMAGE> <CERT_MANAGER_WEBHOOK_IMAGE> <CERT_MANAGER_CA_INJECTOR_IMAGE> <CERT_MANAGER_CONTROLLER_IMAGE> <CERT_MANAGER_ACMESOLVER_IMAGE> <KUBE_RBAC_PROXY_IMAGE>"
  exit 1
}

##############################################
###############  MAIN  #######################
##############################################

if [[ $# -lt 8 ]]; then
  usage
fi

MANIFESTS_DIR=$1
METADATA_DIR=$2
CERT_MANAGER_OPERATOR_IMAGE=$3
CERT_MANAGER_WEBHOOK_IMAGE=$4
CERT_MANAGER_CA_INJECTOR_IMAGE=$5
CERT_MANAGER_CONTROLLER_IMAGE=$6
CERT_MANAGER_ACMESOLVER_IMAGE=$7
KUBE_RBAC_PROXY_IMAGE=$8

echo "[$(date)] -- INFO  -- $@"

if [[ ! -d ${MANIFESTS_DIR} ]]; then
  echo "[$(date)] -- ERROR -- manifests directory \"${MANIFESTS_DIR}\" does not exist"
	exit 1
fi

if [[ ! -d ${METADATA_DIR} ]]; then
  echo "[$(date)] -- ERROR -- metadata directory \"${METADATA_DIR}\" does not exist"
	exit 1
fi

update_csv_manifest
update_annotations_metadata

exit 0
