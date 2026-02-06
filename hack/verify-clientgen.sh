#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_ROOT=$(git rev-parse --show-toplevel)

"${SCRIPT_ROOT}/hack/update-clientgen.sh"

ret=0
git diff --exit-code --quiet || ret=$?
if [[ $ret -ne 0 ]]; then
  echo "Generated clients are out of date. Please run hack/update-clientgen.sh"
  exit 1
fi
echo "clientgen up to date."