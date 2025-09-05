#!/bin/bash

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Global variables
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$SCRIPT_DIR"

# Default environment variables (can be overridden)
NEW_CERT_MANAGER_VERSION="${NEW_CERT_MANAGER_VERSION:-}"
NEW_BUNDLE_VERSION="${NEW_BUNDLE_VERSION:-}"
OLD_BUNDLE_VERSION="${OLD_BUNDLE_VERSION:-}"
OLD_CERT_MANAGER_VERSION="${OLD_CERT_MANAGER_VERSION:-}"

# Logging functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Function to display usage
usage() {
    cat << EOF
Usage: $0 [OPTIONS]

Automates the cert-manager-operator rebase process.

Environment Variables:
  NEW_CERT_MANAGER_VERSION    New cert-manager version (e.g., 1.18.2)
  NEW_BUNDLE_VERSION         New bundle version (e.g., 1.18.0)
  OLD_BUNDLE_VERSION         Old bundle version to replace (optional - auto-detected)
  OLD_CERT_MANAGER_VERSION   Old cert-manager version to replace (optional - auto-detected)

Options:
  -h, --help                 Show this help message
  -d, --dry-run             Show what would be done without making changes
  -s, --step STEP           Run only specific step (1-4)
  --skip-commit             Skip git commits (useful for testing)

Examples:
  # Basic usage with environment variables
  NEW_CERT_MANAGER_VERSION=1.19.0 NEW_BUNDLE_VERSION=1.19.0 $0
  
  # Dry run to see what would be changed
  NEW_CERT_MANAGER_VERSION=1.19.0 NEW_BUNDLE_VERSION=1.19.0 $0 --dry-run
  
  # Run only step 2
  NEW_CERT_MANAGER_VERSION=1.19.0 NEW_BUNDLE_VERSION=1.19.0 $0 --step 2

Steps:
  1. Bump deps with upstream cert-manager
  2. Update Makefile: BUNDLE_VERSION, CERT_MANAGER_VERSION, CHANNELS  
  3. Update CSV: OLM bundle name, version, replaces, skipRange
  4. More manual replacements
EOF
}

# Function to check prerequisites
check_prerequisites() {
    log_info "Checking prerequisites..."
    
    # Check if we're in a git repository
    if ! git rev-parse --git-dir > /dev/null 2>&1; then
        log_error "Not in a git repository"
        exit 1
    fi
    
    # Check for required tools
    local required_tools=("go" "make" "sed" "grep")
    for tool in "${required_tools[@]}"; do
        if ! command -v "$tool" &> /dev/null; then
            log_error "$tool is not installed"
            exit 1
        fi
    done
    
    # Check if required environment variables are set
    if [[ -z "$NEW_CERT_MANAGER_VERSION" ]]; then
        log_error "NEW_CERT_MANAGER_VERSION is not set"
        log_info "Example: export NEW_CERT_MANAGER_VERSION=1.19.0"
        exit 1
    fi
    
    if [[ -z "$NEW_BUNDLE_VERSION" ]]; then
        log_error "NEW_BUNDLE_VERSION is not set"
        log_info "Example: export NEW_BUNDLE_VERSION=1.19.0"
        exit 1
    fi
    
    log_success "Prerequisites check passed"
}

# Function to auto-detect current versions
detect_current_versions() {
    log_info "Auto-detecting current versions..."
    
    # Extract current bundle version from Makefile
    if [[ -z "$OLD_BUNDLE_VERSION" ]]; then
        OLD_BUNDLE_VERSION=$(grep "^BUNDLE_VERSION" Makefile | cut -d'=' -f2 | tr -d ' ?')
        log_info "Auto-detected OLD_BUNDLE_VERSION: $OLD_BUNDLE_VERSION"
    fi
    
    # Extract current cert-manager version from Makefile
    if [[ -z "$OLD_CERT_MANAGER_VERSION" ]]; then
        OLD_CERT_MANAGER_VERSION=$(grep "^CERT_MANAGER_VERSION" Makefile | cut -d'=' -f2 | tr -d ' ?"v')
        log_info "Auto-detected OLD_CERT_MANAGER_VERSION: $OLD_CERT_MANAGER_VERSION"
    fi
    
    # Validate detected versions
    if [[ -z "$OLD_BUNDLE_VERSION" || -z "$OLD_CERT_MANAGER_VERSION" ]]; then
        log_error "Failed to auto-detect current versions"
        exit 1
    fi
    
    log_success "Version detection completed"
    log_info "OLD_BUNDLE_VERSION: $OLD_BUNDLE_VERSION"
    log_info "OLD_CERT_MANAGER_VERSION: $OLD_CERT_MANAGER_VERSION"
    log_info "NEW_BUNDLE_VERSION: $NEW_BUNDLE_VERSION"
    log_info "NEW_CERT_MANAGER_VERSION: $NEW_CERT_MANAGER_VERSION"
}

# Function to backup current state
backup_current_state() {
    log_info "Creating backup of current state..."
    local backup_branch="backup-$(date +%Y%m%d-%H%M%S)"
    git branch "$backup_branch"
    log_success "Created backup branch: $backup_branch"
}

# Step 1: Bump deps with upstream cert-manager
step1_bump_deps() {
    log_info "Step 1: Bumping deps with upstream cert-manager@v$NEW_CERT_MANAGER_VERSION"
    
    if [[ "$DRY_RUN" == "true" ]]; then
        log_warning "[DRY RUN] Would execute:"
        echo "  go get github.com/cert-manager/cert-manager@v$NEW_CERT_MANAGER_VERSION"
        echo "  go mod edit -replace github.com/cert-manager/cert-manager=github.com/openshift/jetstack-cert-manager@v$NEW_CERT_MANAGER_VERSION"
        echo "  go mod tidy && go mod vendor"
        return 0
    fi
    
    # Update cert-manager dependency
    log_info "Running: go get github.com/cert-manager/cert-manager@v$NEW_CERT_MANAGER_VERSION"
    go get "github.com/cert-manager/cert-manager@v$NEW_CERT_MANAGER_VERSION"
    
    # Update replace directive
    log_info "Running: go mod edit -replace github.com/cert-manager/cert-manager=github.com/openshift/jetstack-cert-manager@v$NEW_CERT_MANAGER_VERSION"
    go mod edit -replace "github.com/cert-manager/cert-manager=github.com/openshift/jetstack-cert-manager@v$NEW_CERT_MANAGER_VERSION"
    
    # Tidy and vendor
    log_info "Running: go mod tidy && go mod vendor"
    go mod tidy
    go mod vendor
    
    # Commit changes
    if [[ "$SKIP_COMMIT" != "true" ]]; then
        git add go.mod go.sum vendor/
        git commit -m "Bump deps with upstream cert-manager@v$NEW_CERT_MANAGER_VERSION"
        log_success "Step 1 committed"
    fi
    
    log_success "Step 1 completed"
}

# Step 2: Update Makefile
step2_update_makefile() {
    log_info "Step 2: Update Makefile: BUNDLE_VERSION, CERT_MANAGER_VERSION, CHANNELS"
    
    if [[ "$DRY_RUN" == "true" ]]; then
        log_warning "[DRY RUN] Would update Makefile:"
        echo "  BUNDLE_VERSION: $OLD_BUNDLE_VERSION -> $NEW_BUNDLE_VERSION"
        echo "  CERT_MANAGER_VERSION: v$OLD_CERT_MANAGER_VERSION -> v$NEW_CERT_MANAGER_VERSION"
        echo "  CHANNELS: stable-v1,stable-v$(echo $OLD_BUNDLE_VERSION | cut -d'.' -f1,2) -> stable-v1,stable-v$(echo $NEW_BUNDLE_VERSION | cut -d'.' -f1,2)"
        echo "  Would run: make update && make bundle"
        return 0
    fi
    
    # Extract major.minor versions for channels
    local old_channel_version=$(echo "$OLD_BUNDLE_VERSION" | cut -d'.' -f1,2)
    local new_channel_version=$(echo "$NEW_BUNDLE_VERSION" | cut -d'.' -f1,2)
    
    # Update BUNDLE_VERSION
    log_info "Updating BUNDLE_VERSION: $OLD_BUNDLE_VERSION -> $NEW_BUNDLE_VERSION"
    sed -i "s/^BUNDLE_VERSION ?= $OLD_BUNDLE_VERSION/BUNDLE_VERSION ?= $NEW_BUNDLE_VERSION/" Makefile
    
    # Update CERT_MANAGER_VERSION
    log_info "Updating CERT_MANAGER_VERSION: v$OLD_CERT_MANAGER_VERSION -> v$NEW_CERT_MANAGER_VERSION"
    sed -i "s/^CERT_MANAGER_VERSION ?= \"v$OLD_CERT_MANAGER_VERSION\"/CERT_MANAGER_VERSION ?= \"v$NEW_CERT_MANAGER_VERSION\"/" Makefile
    
    # Update CHANNELS
    log_info "Updating CHANNELS: stable-v1,stable-v$old_channel_version -> stable-v1,stable-v$new_channel_version"
    sed -i "s/^CHANNELS ?= \"stable-v1,stable-v$old_channel_version\"/CHANNELS ?= \"stable-v1,stable-v$new_channel_version\"/" Makefile
    
    # Run make update and make bundle
    log_info "Running: make update"
    make update
    
    log_info "Running: make bundle"
    make bundle
    
    # Commit changes
    if [[ "$SKIP_COMMIT" != "true" ]]; then
        git add .
        git commit -m "Update Makefile: BUNDLE_VERSION, CERT_MANAGER_VERSION, CHANNELS"
        log_success "Step 2 committed"
    fi
    
    log_success "Step 2 completed"
}

# Step 3: Update CSV
step3_update_csv() {
    log_info "Step 3: Update CSV: OLM bundle name, version, replaces, skipRange and skips"
    
    if [[ "$DRY_RUN" == "true" ]]; then
        log_warning "[DRY RUN] Would update CSV files:"
        echo "  version: $OLD_BUNDLE_VERSION -> $NEW_BUNDLE_VERSION"
        echo "  name: cert-manager-operator.v$OLD_BUNDLE_VERSION -> cert-manager-operator.v$NEW_BUNDLE_VERSION"
        echo "  replaces: cert-manager-operator.v[previous] -> cert-manager-operator.v$OLD_BUNDLE_VERSION"
        echo "  skipRange: >=1.17.0 <1.18.0 -> >=$OLD_BUNDLE_VERSION <$NEW_BUNDLE_VERSION"
        echo "  Would run: make update-bindata"
        return 0
    fi
    
    # Files to update
    local csv_files=(
        "config/manifests/bases/cert-manager-operator.clusterserviceversion.yaml"
        "bundle/manifests/cert-manager-operator.clusterserviceversion.yaml"
    )
    
    for csv_file in "${csv_files[@]}"; do
        if [[ -f "$csv_file" ]]; then
            log_info "Updating $csv_file"
            
            # Update version
            sed -i "s/version: $OLD_BUNDLE_VERSION/version: $NEW_BUNDLE_VERSION/" "$csv_file"
            
            # Update name
            sed -i "s/name: cert-manager-operator.v$OLD_BUNDLE_VERSION/name: cert-manager-operator.v$NEW_BUNDLE_VERSION/" "$csv_file"
            
            # Update replaces (should point to the old version that we're replacing)
            sed -i "s/replaces: cert-manager-operator\.v[0-9]\+\.[0-9]\+\.[0-9]\+/replaces: cert-manager-operator.v$OLD_BUNDLE_VERSION/" "$csv_file"
            
            # Update skipRange
            sed -i "s/olm.skipRange: '>=.*<.*'/olm.skipRange: '>=$OLD_BUNDLE_VERSION <$NEW_BUNDLE_VERSION'/" "$csv_file"
            
            # Note: Description updates will be handled in Step 4 (manual replacements)
        fi
    done
    
    # Update bundle.Dockerfile
    if [[ -f "bundle.Dockerfile" ]]; then
        log_info "Updating bundle.Dockerfile"
        local old_channel_version=$(echo "$OLD_BUNDLE_VERSION" | cut -d'.' -f1,2)
        local new_channel_version=$(echo "$NEW_BUNDLE_VERSION" | cut -d'.' -f1,2)
        sed -i "s/stable-v1,stable-v$old_channel_version/stable-v1,stable-v$new_channel_version/" bundle.Dockerfile
    fi
    
    # Update bundle metadata
    if [[ -f "bundle/metadata/annotations.yaml" ]]; then
        log_info "Updating bundle/metadata/annotations.yaml"
        local old_channel_version=$(echo "$OLD_BUNDLE_VERSION" | cut -d'.' -f1,2)
        local new_channel_version=$(echo "$NEW_BUNDLE_VERSION" | cut -d'.' -f1,2)
        sed -i "s/stable-v1,stable-v$old_channel_version/stable-v1,stable-v$new_channel_version/" bundle/metadata/annotations.yaml
    fi
    
    # Run make update-bindata
    log_info "Running: make update-bindata"
    make update-bindata
    
    # Commit changes
    if [[ "$SKIP_COMMIT" != "true" ]]; then
        git add .
        git commit -m "Update CSV: OLM bundle name, version, replaces, skipRange and skips"
        log_success "Step 3 committed"
    fi
    
    log_success "Step 3 completed"
}

# Step 4: More manual replacements
step4_manual_replacements() {
    log_info "Step 4: More manual replacements"
    
    if [[ "$DRY_RUN" == "true" ]]; then
        log_warning "[DRY RUN] Would perform manual replacements:"
        echo "  Replace $OLD_BUNDLE_VERSION -> $NEW_BUNDLE_VERSION (operator version)"
        echo "  Replace $OLD_CERT_MANAGER_VERSION -> $NEW_CERT_MANAGER_VERSION (operand version)"
        echo "  Update CSV descriptions: [cert-manager v$OLD_CERT_MANAGER_VERSION](...) -> [cert-manager v$NEW_CERT_MANAGER_VERSION](...)"
        echo "  Update container images and Dockerfiles"
        echo "  Would run: make manifests bundle"
        return 0
    fi
    
    # Find files that might contain version references (excluding vendor and .git)
    local files_to_check=(
        $(find . -type f \( -name "*.go" -o -name "*.yaml" -o -name "*.yml" -o -name "*.json" -o -name "*.md" -o -name "*.Dockerfile" \) \
          -not -path "./vendor/*" \
          -not -path "./.git/*" \
          -not -path "./testbin/*" \
          | grep -v "go.sum")
    )
    
    local changed_files=()
    
    # Function to safely replace versions (avoiding URLs and specific patterns)
    safe_replace_version() {
        local file="$1"
        local old_version="$2"
        local new_version="$3"
        local context="$4"
        
        # Skip if file doesn't exist or isn't readable
        [[ ! -f "$file" || ! -r "$file" ]] && return
        
        # Create a temporary file for processing
        local temp_file=$(mktemp)
        cp "$file" "$temp_file"
        
        # Specific patterns to replace (avoiding URLs and comments)
        case "$context" in
            "cert-manager")
                # Replace cert-manager version in specific contexts (avoid URLs and comments)
                sed -i "s/cert-manager v${old_version}/cert-manager v${new_version}/g" "$temp_file"
                sed -i "s/cert-manager@v${old_version}/cert-manager@v${new_version}/g" "$temp_file"
                # Update CSV description links - match any existing version and replace with new version
                sed -i "s|\[cert-manager v[0-9]\+\.[0-9]\+\.[0-9]\+\](https://github.com/cert-manager/cert-manager/tree/v[0-9]\+\.[0-9]\+\.[0-9]\+)|\[cert-manager v${new_version}\](https://github.com/cert-manager/cert-manager/tree/v${new_version})|g" "$temp_file"
                sed -i "s/cert-manager-acmesolver:v${old_version}/cert-manager-acmesolver:v${new_version}/g" "$temp_file"
                sed -i "s/cert-manager-controller:v${old_version}/cert-manager-controller:v${new_version}/g" "$temp_file"
                sed -i "s/cert-manager-webhook:v${old_version}/cert-manager-webhook:v${new_version}/g" "$temp_file"
                sed -i "s/cert-manager-cainjector:v${old_version}/cert-manager-cainjector:v${new_version}/g" "$temp_file"
                sed -i "s/cert-manager\/tree\/v${old_version}/cert-manager\/tree\/v${new_version}/g" "$temp_file"
                sed -i "s/app\.kubernetes\.io\/version: v${old_version}/app.kubernetes.io\/version: v${new_version}/g" "$temp_file"
                sed -i "s/OPERAND_IMAGE_VERSION[[:space:]]*=[[:space:]]*${old_version}/OPERAND_IMAGE_VERSION = ${new_version}/g" "$temp_file"
                sed -i "s/value: ${old_version}$/value: ${new_version}/g" "$temp_file"
                sed -i "s/RELEASE_BRANCH=v${old_version}/RELEASE_BRANCH=v${new_version}/g" "$temp_file"
                # Avoid corrupting URLs in comments - only replace in specific image contexts
                sed -i "s/\(quay\.io\/jetstack\/.*:\)v${old_version}/\1v${new_version}/g" "$temp_file"
                ;;
            "bundle")
                # Replace bundle version in specific contexts (avoid URLs)
                sed -i "s/\b${old_version}\b/${new_version}/g" "$temp_file"
                ;;
        esac
        
        # Check if file was actually modified
        if ! cmp -s "$file" "$temp_file"; then
            mv "$temp_file" "$file"
            return 0
        else
            rm -f "$temp_file"
            return 1
        fi
    }
    
    # Replace cert-manager version references in specific contexts
    log_info "Searching for cert-manager v$OLD_CERT_MANAGER_VERSION references..."
    for file in "${files_to_check[@]}"; do
        # Skip files that contain URLs or comments that might be corrupted
        if grep -q "$OLD_CERT_MANAGER_VERSION" "$file" 2>/dev/null; then
            # Skip if this looks like a URL corruption case
            if grep -q "https://.*${OLD_CERT_MANAGER_VERSION}" "$file" 2>/dev/null; then
                log_info "Skipping $file - contains URLs that might be corrupted"
                continue
            fi
            
            if safe_replace_version "$file" "$OLD_CERT_MANAGER_VERSION" "$NEW_CERT_MANAGER_VERSION" "cert-manager"; then
                log_info "Updated cert-manager version in $file"
                changed_files+=("$file")
            fi
        fi
    done
    
    # Replace bundle version references (more careful replacement)
    log_info "Searching for bundle version $OLD_BUNDLE_VERSION references..."
    for file in "${files_to_check[@]}"; do
        # Skip files that already contain the new version (to avoid double replacement)
        if grep -q "$OLD_BUNDLE_VERSION" "$file" 2>/dev/null && ! grep -q "$NEW_BUNDLE_VERSION" "$file" 2>/dev/null; then
            if safe_replace_version "$file" "$OLD_BUNDLE_VERSION" "$NEW_BUNDLE_VERSION" "bundle"; then
                log_info "Updated bundle version in $file"
                changed_files+=("$file")
            fi
        fi
    done
    
    # Remove duplicates from changed_files array and report changes
    if [[ ${#changed_files[@]} -gt 0 ]]; then
        # Sort and remove duplicates
        local unique_files=($(printf '%s\n' "${changed_files[@]}" | sort -u))
        changed_files=("${unique_files[@]}")
        
        log_info "Modified files:"
        printf '  %s\n' "${changed_files[@]}"
    else
        log_info "No additional files needed manual replacement"
    fi
    
    # Always run make manifests bundle to ensure generated files are updated
    log_info "Running: make manifests bundle"
    make manifests bundle
    
    # Commit all changes (manual replacements + generated files)
    if [[ "$SKIP_COMMIT" != "true" ]] && [[ -n "$(git status --porcelain)" ]]; then
        git add .
        git commit -m "More manual replacements"
        log_success "Step 4 committed"
    else
        log_info "No changes to commit in Step 4"
    fi
    
    log_success "Step 4 completed"
}

# Function to run all steps
run_all_steps() {
    log_info "Running all rebase steps..."
    
    step1_bump_deps
    step2_update_makefile  
    step3_update_csv
    step4_manual_replacements
    
    log_success "All steps completed successfully!"
    log_info "Summary of changes:"
    log_info "  - Bumped cert-manager from v$OLD_CERT_MANAGER_VERSION to v$NEW_CERT_MANAGER_VERSION"
    log_info "  - Updated bundle version from $OLD_BUNDLE_VERSION to $NEW_BUNDLE_VERSION"
    log_info "  - Updated CSV metadata and skipRange"
    log_info "  - Performed manual replacements across codebase"
}

# Main execution
main() {
    local DRY_RUN=false
    local SKIP_COMMIT=false
    local SPECIFIC_STEP=""
    
    # Parse command line arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            -h|--help)
                usage
                exit 0
                ;;
            -d|--dry-run)
                DRY_RUN=true
                shift
                ;;
            -s|--step)
                SPECIFIC_STEP="$2"
                shift 2
                ;;
            --skip-commit)
                SKIP_COMMIT=true
                shift
                ;;
            *)
                log_error "Unknown option: $1"
                usage
                exit 1
                ;;
        esac
    done
    
    # Export variables for use in functions
    export DRY_RUN SKIP_COMMIT
    
    log_info "Starting cert-manager-operator rebase automation"
    
    # Run checks and setup
    check_prerequisites
    detect_current_versions
    
    if [[ "$DRY_RUN" != "true" ]]; then
        backup_current_state
    fi
    
    # Run specific step or all steps
    if [[ -n "$SPECIFIC_STEP" ]]; then
        case "$SPECIFIC_STEP" in
            1)
                step1_bump_deps
                ;;
            2)
                step2_update_makefile
                ;;
            3)
                step3_update_csv
                ;;
            4)
                step4_manual_replacements
                ;;
            *)
                log_error "Invalid step: $SPECIFIC_STEP. Must be 1-4"
                exit 1
                ;;
        esac
    else
        run_all_steps
    fi
    
    if [[ "$DRY_RUN" == "true" ]]; then
        log_info "Dry run completed. No changes were made."
    else
        log_success "Rebase automation completed successfully!"
    fi
}

# Run main function with all arguments
main "$@" 