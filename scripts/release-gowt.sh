#!/bin/bash
#
# Release script for gowt
# Generates git tag command with changelog from previous version
#

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
GOWT_DIR="$REPO_ROOT/gowt"

# Extract version from meta/version.go
VERSION=$(grep -oP 'Version\s*=\s*"\K[^"]+' "$GOWT_DIR/meta/version.go")
if [[ -z "$VERSION" ]]; then
    echo "Error: Could not extract version from gowt/meta/version.go"
    exit 1
fi

TAG_NAME="gowt/v$VERSION"

# Check if tag already exists
if git tag -l "$TAG_NAME" | grep -q "$TAG_NAME"; then
    echo "Error: Tag $TAG_NAME already exists"
    exit 1
fi

# Find the previous gowt tag
PREV_TAG=$(git tag -l "gowt/v*" --sort=-version:refname | head -1)

echo "========================================"
echo "  gowt Release Script"
echo "========================================"
echo ""
echo "Current version: $VERSION"
echo "New tag:         $TAG_NAME"
echo "Previous tag:    ${PREV_TAG:-"(none)"}"
echo ""

# Generate changelog
echo "========================================"
echo "  Changelog (commits since $PREV_TAG)"
echo "========================================"
echo ""

if [[ -n "$PREV_TAG" ]]; then
    # Get commits since previous tag, filtered to gowt/ changes
    CHANGELOG=$(git log "$PREV_TAG"..HEAD --pretty=format:"- %s" -- "$GOWT_DIR")
else
    # No previous tag, get all commits for gowt/
    CHANGELOG=$(git log --pretty=format:"- %s" -- "$GOWT_DIR")
fi

if [[ -z "$CHANGELOG" ]]; then
    CHANGELOG="- No changes recorded"
fi

echo "$CHANGELOG"
echo ""

# Build the tag message
TAG_MESSAGE="gowt $VERSION

Changes since ${PREV_TAG:-"initial"}:

$CHANGELOG"

echo "========================================"
echo "  Tag Message Preview"
echo "========================================"
echo ""
echo "$TAG_MESSAGE"
echo ""

echo "========================================"
echo "  Commands to create and push release"
echo "========================================"
echo ""
echo "# Step 1: Create the annotated tag:"
echo "git tag -a \"$TAG_NAME\" -m \"$TAG_MESSAGE\""
echo ""
echo "# Step 2: Push the tag to remote:"
echo "git push origin \"$TAG_NAME\""
echo ""
echo "# Step 3: Trigger Go proxy to index the new version:"
echo "GOPROXY=https://proxy.golang.org go list -m github.com/rickchristie/govner/gowt@v$VERSION"
echo ""
echo "# After indexing, users can install with:"
echo "# go install github.com/rickchristie/govner/gowt@latest"
echo ""
