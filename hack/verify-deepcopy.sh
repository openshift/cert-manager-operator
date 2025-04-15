#!/bin/bash

set -euo pipefail

function print_failure {
  echo "There are unexpected changes to the tree when running 'make generate'. Please"
  echo "run these commands locally and double-check the Git repository for unexpected changes which may"
  echo "need to be committed."
  exit 1
}

echo "> generating deepcopy"
make generate
git status
git diff
test -z "$(git status --porcelain | \grep -v '^??')" || print_failure
