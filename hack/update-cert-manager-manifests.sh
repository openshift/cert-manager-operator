#!/bin/bash

source "$(dirname "${BASH_SOURCE}")/lib/init.sh"

MANIFEST_SOURCE=${1:?"missing Cert Manager manifest url. You can use either http:// or file://"}

mkdir -p ./_output

echo "---- Downloading Manifest file from $MANIFEST_SOURCE ----"
curl -NLs "$MANIFEST_SOURCE" -o ./_output/manifest.yaml

echo "---- Installing YAML Spliiter util ----"
go install ./vendor/github.com/mogensen/kubernetes-split-yaml

echo "---- Removing old content of ./bindata ----"
rm -rf bindata
mkdir -p bindata/cert-manager-deployment/cert-manager-controller
mkdir -p bindata/cert-manager-deployment/cert-manager-webhook
mkdir -p bindata/cert-manager-deployment/cert-manager-cainjector
mkdir -p bindata/cert-manager-crds

echo "---- Splitting resources ----"
# Split generated resources into separate files
kubernetes-split-yaml --outdir bindata/cert-manager-deployment ./_output/manifest.yaml

echo "---- Processing resources ----"
# Remove colons from names
find ./bindata/ -type f -name '*:*' -execdir bash -c 'mv "$1" "${1//:/-}"' bash {} \;
# Move CRDs into a separate directory, we'll probably need to process them further?
grep -lir 'CustomResourceDefinition' ./bindata | xargs mv -t bindata/cert-manager-crds
# Remove lines containing word Helm
find ./bindata -name "*.yaml" -type f | xargs sed -i -e '/[hH]elm/d'
# Move files to their own directories
grep -lir "app.kubernetes.io/component: cainjector" ./bindata/ | xargs mv -t ./bindata/cert-manager-deployment/cert-manager-cainjector
grep -lir "app.kubernetes.io/component: controller" ./bindata/ | xargs mv -t ./bindata/cert-manager-deployment/cert-manager-controller
grep -lir "app.kubernetes.io/component: cert-manager" ./bindata/ | xargs mv -t ./bindata/cert-manager-deployment/cert-manager-controller
grep -lir "app.kubernetes.io/component: webhook" ./bindata/ | xargs mv -t ./bindata/cert-manager-deployment/cert-manager-webhook