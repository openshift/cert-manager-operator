#!/bin/bash
set -euo pipefail

function print_failure {
  echo "There are unexpected changes to the vendor tree following 'go work vendor' and 'go mod tidy'. Please"
  echo "run these commands locally and double-check the Git repository for unexpected changes which may"
  echo "need to be committed."
  exit 1
}

if [ "${OPENSHIFT_CI:-false}" = true ]; then
  # Clear GOFLAGS to ensure workspace mode works correctly with go.work
  # (CI sets GOFLAGS=-mod=vendor which conflicts with go.work)
  export GOFLAGS=""

  # Tidy all modules
  go mod tidy
  (cd tools && go mod tidy)
  (cd test/e2e && go mod tidy)

  # Vendor all workspace modules into a single vendor directory
  go work vendor

  test -z "$(git status --porcelain | \grep -v '^??')" || print_failure
  echo "verified Go modules"
fi
