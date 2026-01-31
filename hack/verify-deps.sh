#!/usr/bin/env bash

set -o nounset
set -o pipefail
set -o errexit

# Check for packages with multiple versions in vendor/modules.txt
# This can cause build failures due to API incompatibilities between versions
function check_vendor_version_conflicts {
  local modules_txt="vendor/modules.txt"
  if [[ ! -f "$modules_txt" ]]; then
    echo "Warning: $modules_txt not found, skipping version conflict check"
    return 0
  fi

  # Extract package names (without versions) and find duplicates
  # Exclude lines with "=>" (replace directives) as they create expected "duplicates"
  local duplicates
  duplicates=$(grep "^# " "$modules_txt" | grep -v "=>" | awk '{print $2}' | sort | uniq -d)

  if [[ -z "$duplicates" ]]; then
    echo "No duplicate package versions found in vendor"
    return 0
  fi

  # All duplicates are now treated as errors to keep versions uniform
  echo "ERROR: Found packages with multiple versions in vendor!"
  echo "This can cause build failures or unexpected behavior."
  echo ""
  echo "Affected packages:"
  for pkg in $duplicates; do
    grep "^# $pkg " "$modules_txt"
  done
  echo ""
  echo "To fix, align package versions across all workspace modules:"
  echo "  1. Identify which module has the older version"
  echo "  2. Run: go get -C <module_dir> <package>@<newer_version>"
  echo "  3. Run: go mod tidy && go mod tidy -C tools && go mod tidy -C test"
  echo "  4. Run: go work vendor"
  return 1
}

# Updates the dependencies in all go modules to verify, there are no
# uncommitted changed.
function update_deps {
  if [ "${OPENSHIFT_CI:-false}" = true ]; then
    # Clear GOFLAGS to ensure workspace mode works correctly with go.work
    # (CI sets GOFLAGS=-mod=vendor which conflicts with go.work)
    export GOFLAGS=""

    # Tidy all modules
    go mod tidy
    go mod tidy -C ./test
    go mod tidy -C ./tools

    # Sync all module versions in the workspace
    go work sync

    # Vendor all workspace modules into a single vendor directory
    go work vendor

    check_vendor_version_conflicts

    # Check for any changes including untracked files (for CI environments)
    changes=$(git status --porcelain)

    if [[ -n "${changes}" ]]; then
      echo "ERROR: There are uncommitted or untracked changes. Please commit or remove them."
      echo "Changed files:"
      git status --short
      exit 1
    fi
  fi
}

##############################################
###############  MAIN  #######################
##############################################

# Allow running just the version check locally with: ./hack/verify-deps.sh --check-versions
if [[ "${1:-}" == "--check-versions" ]]; then
  check_vendor_version_conflicts
  exit $?
fi

# Check all dependenices are committed
update_deps
