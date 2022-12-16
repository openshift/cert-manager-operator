#!/bin/sh

set -e

VERSION="1.25.1"

OUTPUT_PATH=${1:-./bin/operator-sdk}
VERIFY=${2:-yes}

GOOS=$(go env GOOS)
GOARCH=$(go env GOARCH)
BIN="operator-sdk"
BIN_ARCH="${BIN}_${GOOS}_${GOARCH}"
OPERATOR_SDK_DL_URL="https://github.com/operator-framework/operator-sdk/releases/download/v${VERSION}"

case ${GOOS} in
  linux)
    CHECKSUM="9596b2894b4b1d7f1c3b54cb1ec317b86494dbc00718b48561dfbcb232477c26"
    ;;
  darwin)
    CHECKSUM="bb54842b92efee4ea1071373f7482b62a8130cc3dc4d45a5be50041ae7c81e7c"
    ;;
    *)
    echo "Unsupported OS $GOOS"
    exit 1
    ;;
esac

if [ "$GOARCH" != "amd64" ]; then
  echo "Unsupported architecture $GOARCH"
  exit 1
fi

command -v curl &> /dev/null || { echo "can't find curl command" && exit 1; }
command -v sha256sum &> /dev/null || { echo "can't find sha256sum command" && exit 1; }

TEMPDIR=$(mktemp -d)
BIN_PATH="${TEMPDIR}/${BIN_ARCH}"

echo "> downloading binary"
curl --silent --location -o "${BIN_PATH}" "${OPERATOR_SDK_DL_URL}/operator-sdk_${GOOS}_${GOARCH}"

if [ "${VERIFY}" == "yes" ]; then
    echo "> verifying binary"
    echo "${CHECKSUM} ${BIN_PATH}" | sha256sum -c --quiet
fi

echo "> installing binary"
mv "${BIN_PATH}" "${OUTPUT_PATH}"
chmod +x "${OUTPUT_PATH}"
rm -rf "${TEMPDIR}"
