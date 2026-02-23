#!/bin/bash
#
# Downloads and verifies external tool binaries.
# Usage:
#   ./hack/download-tools.sh [tool] [output_path]
#   ./hack/download-tools.sh                    # Download all tools to default paths
#   ./hack/download-tools.sh helm               # Download helm to default path
#   ./hack/download-tools.sh helm ./bin/helm    # Download helm to specified path
#
# Supported tools: helm, opm, operator-sdk

set -eou pipefail

# Tool versions
HELM_VERSION="${HELM_VERSION:-v3.17.1}"
OPM_VERSION="${OPM_VERSION:-v1.23.0}"
OPERATOR_SDK_VERSION="${OPERATOR_SDK_VERSION:-v1.25.1}"

# Default output directory
DEFAULT_BIN_DIR="${BIN_DIR:-./bin}"

# Platform detection
GOOS=$(go env GOOS)
GOARCH=$(go env GOARCH)

# Validate platform
validate_platform() {
  if [[ "$GOOS" != "linux" && "$GOOS" != "darwin" ]]; then
    echo "ERROR: Unsupported OS: $GOOS"
    exit 1
  fi
  if [[ "$GOARCH" != "amd64" && "$GOARCH" != "arm64" && "$GOARCH" != "ppc64le" && "$GOARCH" != "s390x" ]]; then
    echo "ERROR: Unsupported architecture: $GOARCH"
    exit 1
  fi
}

# Verify checksum of a downloaded file
# Args: $1 = file path, $2 = expected hash
verify_checksum() {
  local file="$1"
  local expected_hash="$2"
  
  if [[ -z "${expected_hash}" ]]; then
    echo "ERROR: Empty checksum provided"
    return 1
  fi
  
  if command -v sha256sum >/dev/null 2>&1; then
    echo "${expected_hash}  ${file}" | sha256sum -c -
  elif command -v shasum >/dev/null 2>&1; then
    echo "${expected_hash}  ${file}" | shasum -a 256 -c -
  else
    echo "ERROR: sha256sum or shasum is required for checksum verification"
    return 1
  fi
}

# Extract checksum from checksums file
# Args: $1 = checksums file, $2 = binary name pattern
extract_checksum() {
  local checksums_file="$1"
  local pattern="$2"
  
  grep "${pattern}$" "${checksums_file}" | awk '{print $1}'
}

# Download helm
download_helm() {
  local output_path="${1:-${DEFAULT_BIN_DIR}/helm}"
  local version="${HELM_VERSION}"
  
  local tar_filename="helm-${version}-${GOOS}-${GOARCH}.tar.gz"
  local download_url="https://get.helm.sh/${tar_filename}"
  local checksum_url="${download_url}.sha256sum"
  
  local tempdir
  tempdir=$(mktemp -d)
  # shellcheck disable=SC2064  # Intentionally expand tempdir now, not at trap time
  trap "rm -rf '${tempdir}'" RETURN
  
  echo "> downloading helm ${version}"
  curl --silent --location -o "${tempdir}/${tar_filename}" "${download_url}"
  
  echo "> verifying checksum"
  curl --silent --location -o "${tempdir}/checksum.sha256" "${checksum_url}"
  local expected_hash
  expected_hash=$(awk '{print $1}' "${tempdir}/checksum.sha256")
  verify_checksum "${tempdir}/${tar_filename}" "${expected_hash}"
  
  echo "> extracting binary"
  tar -C "${tempdir}" -xzf "${tempdir}/${tar_filename}"
  mkdir -p "$(dirname "${output_path}")"
  mv "${tempdir}/${GOOS}-${GOARCH}/helm" "${output_path}"
  chmod +x "${output_path}"
  
  echo "> helm available at ${output_path}"
}

# Download opm
download_opm() {
  local output_path="${1:-${DEFAULT_BIN_DIR}/opm}"
  local version="${OPM_VERSION}"
  
  local bin_name="${GOOS}-${GOARCH}-opm"
  local download_url="https://github.com/operator-framework/operator-registry/releases/download/${version}"
  
  local tempdir
  tempdir=$(mktemp -d)
  # shellcheck disable=SC2064  # Intentionally expand tempdir now, not at trap time
  trap "rm -rf '${tempdir}'" RETURN
  
  echo "> downloading opm ${version}"
  curl --silent --location -o "${tempdir}/${bin_name}" "${download_url}/${bin_name}"
  
  echo "> verifying checksum"
  curl --silent --location -o "${tempdir}/checksums.txt" "${download_url}/checksums.txt"
  local expected_hash
  expected_hash=$(extract_checksum "${tempdir}/checksums.txt" "${bin_name}")
  if [[ -z "${expected_hash}" ]]; then
    echo "ERROR: Could not find checksum for ${bin_name}"
    exit 1
  fi
  verify_checksum "${tempdir}/${bin_name}" "${expected_hash}"
  
  echo "> installing binary"
  mkdir -p "$(dirname "${output_path}")"
  mv "${tempdir}/${bin_name}" "${output_path}"
  chmod +x "${output_path}"
  
  echo "> opm available at ${output_path}"
}

# Download operator-sdk
download_operator_sdk() {
  local output_path="${1:-${DEFAULT_BIN_DIR}/operator-sdk}"
  local version="${OPERATOR_SDK_VERSION}"
  
  local bin_name="operator-sdk_${GOOS}_${GOARCH}"
  local download_url="https://github.com/operator-framework/operator-sdk/releases/download/${version}"
  
  local tempdir
  tempdir=$(mktemp -d)
  # shellcheck disable=SC2064  # Intentionally expand tempdir now, not at trap time
  trap "rm -rf '${tempdir}'" RETURN
  
  echo "> downloading operator-sdk ${version}"
  curl --silent --location -o "${tempdir}/${bin_name}" "${download_url}/${bin_name}"
  
  echo "> verifying checksum"
  curl --silent --location -o "${tempdir}/checksums.txt" "${download_url}/checksums.txt"
  local expected_hash
  expected_hash=$(extract_checksum "${tempdir}/checksums.txt" "${bin_name}")
  if [[ -z "${expected_hash}" ]]; then
    echo "ERROR: Could not find checksum for ${bin_name}"
    exit 1
  fi
  verify_checksum "${tempdir}/${bin_name}" "${expected_hash}"
  
  echo "> installing binary"
  mkdir -p "$(dirname "${output_path}")"
  mv "${tempdir}/${bin_name}" "${output_path}"
  chmod +x "${output_path}"
  
  echo "> operator-sdk available at ${output_path}"
}

# Download all tools
download_all() {
  download_helm
  download_opm
  download_operator_sdk
}

# Main
main() {
  command -v curl &> /dev/null || { echo "ERROR: curl command not found" && exit 1; }
  validate_platform
  
  local tool="${1:-all}"
  local output_path="${2:-}"
  
  case "${tool}" in
    helm)
      download_helm "${output_path}"
      ;;
    opm)
      download_opm "${output_path}"
      ;;
    operator-sdk)
      download_operator_sdk "${output_path}"
      ;;
    all)
      download_all
      ;;
    *)
      echo "ERROR: Unknown tool '${tool}'"
      echo "Supported tools: helm, opm, operator-sdk, all"
      exit 1
      ;;
  esac
}

main "$@"
