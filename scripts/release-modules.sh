#!/bin/bash
# Multi-Module Release Helper Script
# Usage: ./scripts/release-modules.sh <version> [modules]

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored output
print_info() {
    echo -e "${BLUE}ℹ${NC} $1"
}

print_success() {
    echo -e "${GREEN}✓${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}⚠${NC} $1"
}

print_error() {
    echo -e "${RED}✗${NC} $1"
}

# Check if we're in the root of the repo
if [ ! -f "go.mod" ]; then
    print_error "Must be run from repository root"
    exit 1
fi

# Parse arguments
VERSION=$1
MODULES=$2

if [ -z "$VERSION" ]; then
    echo "Usage: $0 <version> [modules]"
    echo ""
    echo "Examples:"
    echo "  $0 1.0.0                    # Release only main module"
    echo "  $0 1.0.0 grpc,kafka         # Release main + specific extensions"
    echo "  $0 1.0.0 all                # Release all modules"
    echo "  $0 1.0.0-beta.1 all         # Pre-release all modules"
    echo ""
    echo "Available extensions: graphql, grpc, hls, kafka, mqtt"
    exit 1
fi

# Validate semantic version
if [[ ! "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9.]+)?$ ]]; then
    print_error "Invalid semantic version: $VERSION"
    echo "Expected format: 1.2.3 or 1.2.3-beta.1"
    exit 1
fi

# Check if version already exists
if git tag -l "v$VERSION" | grep -q "v$VERSION"; then
    print_error "Tag v$VERSION already exists!"
    exit 1
fi

# Determine which modules to release
EXTENSION_LIST=()

if [ "$MODULES" = "all" ]; then
    EXTENSION_LIST=(graphql grpc hls kafka mqtt)
    print_info "Releasing ALL modules"
elif [ -n "$MODULES" ]; then
    IFS=',' read -ra EXTENSION_LIST <<< "$MODULES"
    print_info "Releasing main + selected extensions: ${EXTENSION_LIST[*]}"
else
    print_info "Releasing main module only"
fi

# Check if working directory is clean
if ! git diff-index --quiet HEAD --; then
    print_error "Working directory is not clean. Commit or stash changes first."
    git status --short
    exit 1
fi

print_success "Working directory is clean"

# Verify all extensions exist
for ext in "${EXTENSION_LIST[@]}"; do
    if [ ! -d "extensions/$ext" ] || [ ! -f "extensions/$ext/go.mod" ]; then
        print_error "Extension not found: $ext"
        exit 1
    fi
    
    # Check if extension tag already exists
    if git tag -l "extensions/$ext/v$VERSION" | grep -q "extensions/$ext/v$VERSION"; then
        print_error "Tag extensions/$ext/v$VERSION already exists!"
        exit 1
    fi
done

# Run tests before tagging
print_info "Running tests for main module..."
if go test -race ./... > /dev/null 2>&1; then
    print_success "Main module tests passed"
else
    print_error "Main module tests failed"
    exit 1
fi

# Test extensions
for ext in "${EXTENSION_LIST[@]}"; do
    print_info "Running tests for $ext extension..."
    if (cd "extensions/$ext" && go test -race ./... > /dev/null 2>&1); then
        print_success "$ext extension tests passed"
    else
        print_error "$ext extension tests failed"
        exit 1
    fi
done

echo ""
print_info "Creating tags..."
echo ""

# Create main module tag
MAIN_TAG="v$VERSION"
print_info "Creating tag: $MAIN_TAG"
git tag -a "$MAIN_TAG" -m "Release v$VERSION"
print_success "Created $MAIN_TAG"

# Create extension tags
for ext in "${EXTENSION_LIST[@]}"; do
    EXT_TAG="extensions/$ext/v$VERSION"
    print_info "Creating tag: $EXT_TAG"
    git tag -a "$EXT_TAG" -m "Release $ext extension v$VERSION"
    print_success "Created $EXT_TAG"
done

echo ""
print_success "All tags created successfully!"
echo ""

# Show created tags
print_info "Created tags:"
git tag -l "*$VERSION" | while read -r tag; do
    echo "  - $tag"
done

echo ""
print_warning "Tags created locally. Review them before pushing."
echo ""

# Ask for confirmation to push
read -p "$(echo -e ${YELLOW}Push these tags to origin? [y/N]${NC} )" -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    print_info "Pushing tags to origin..."
    
    # Push main tag
    git push origin "$MAIN_TAG"
    print_success "Pushed $MAIN_TAG"
    
    # Push extension tags
    for ext in "${EXTENSION_LIST[@]}"; do
        EXT_TAG="extensions/$ext/v$VERSION"
        git push origin "$EXT_TAG"
        print_success "Pushed $EXT_TAG"
    done
    
    echo ""
    print_success "All tags pushed successfully!"
    echo ""
    print_info "GitHub Actions will now create releases automatically."
    print_info "Monitor progress: https://github.com/$(git remote get-url origin | sed 's/.*github.com[:/]\(.*\)\.git/\1/')/actions"
else
    echo ""
    print_warning "Tags not pushed. You can push later with:"
    echo "  git push origin --tags"
    echo ""
    print_info "Or delete the tags with:"
    echo "  git tag -d v$VERSION"
    for ext in "${EXTENSION_LIST[@]}"; do
        echo "  git tag -d extensions/$ext/v$VERSION"
    done
fi

echo ""
print_success "Done!"

