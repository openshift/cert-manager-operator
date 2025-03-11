#!/bin/bash

set -euo pipefail

OUTPUT_PATH="./bin/yq"

if [ ! -f "${OUTPUT_PATH}" ]; then
    echo "---- Installing yq tooling ----"

    DIR=$(dirname "${OUTPUT_PATH}")
    mkdir -p "${DIR}"
    curl -s -f -L "https://github.com/mikefarah/yq/releases/download/v4.13.3/yq_$(go env GOHOSTOS)_$(go env GOHOSTARCH)" -o "${OUTPUT_PATH}"
    chmod +x "${OUTPUT_PATH}"

    echo "yq binary installed in ${OUTPUT_PATH}"
fi
