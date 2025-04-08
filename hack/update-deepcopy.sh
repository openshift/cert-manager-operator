#!/bin/bash

source "$(dirname "${BASH_SOURCE}")/lib/init.sh"

# Define SCRIPT_ROOT
#SCRIPT_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)


GENERATOR=deepcopy ${SCRIPT_ROOT}/hack/update-codegen.sh