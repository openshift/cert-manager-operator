#!/bin/sh

set -eou pipefail

OUTPUT_PATH=${1:-./bin/helm}
VERSION="v3.17.1"

GOOS=$(go env GOOS)
GOARCH=$(go env GOARCH)

TAR_FILENAME="helm-${VERSION}-${GOOS}-${GOARCH}.tar.gz"
TAR_DL_URL="https://get.helm.sh/${TAR_FILENAME}"

TEMP_DIR=$(mktemp -d)

echo "> downloading helm binary"

curl --silent --location -o "${TEMP_DIR}/${TAR_FILENAME}" "${TAR_DL_URL}"
tar -C "${TEMP_DIR}" -xzf "${TEMP_DIR}/${TAR_FILENAME}"
mv "${TEMP_DIR}/${GOOS}-${GOARCH}/helm" "${OUTPUT_PATH}"

echo "> helm binary available at ${OUTPUT_PATH}"
