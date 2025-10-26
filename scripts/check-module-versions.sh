#!/bin/bash
# Check module versions and dependencies
# Usage: ./scripts/check-module-versions.sh

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}=== Forge Multi-Module Version Check ===${NC}"
echo ""

# Check main module
echo -e "${BLUE}Main Module (forge)${NC}"
echo "  Location: ."
MAIN_VERSION=$(grep "^go " go.mod | awk '{print $2}')
echo "  Go Version: $MAIN_VERSION"
MAIN_TAG=$(git tag -l "v*" --sort=-version:refname | grep -v "extensions" | head -1)
if [ -n "$MAIN_TAG" ]; then
    echo "  Latest Tag: $MAIN_TAG"
else
    echo -e "  Latest Tag: ${YELLOW}(none)${NC}"
fi
echo ""

# Check extensions
EXTENSIONS=(graphql grpc hls kafka mqtt)
ISSUES_FOUND=0

for ext in "${EXTENSIONS[@]}"; do
    if [ -d "extensions/$ext" ] && [ -f "extensions/$ext/go.mod" ]; then
        echo -e "${BLUE}Extension: $ext${NC}"
        echo "  Location: extensions/$ext"
        
        # Get Go version
        EXT_GO_VERSION=$(grep "^go " "extensions/$ext/go.mod" | awk '{print $2}')
        echo -n "  Go Version: $EXT_GO_VERSION"
        
        # Check if it matches main
        if [ "$EXT_GO_VERSION" != "$MAIN_VERSION" ]; then
            echo -e " ${RED}⚠ MISMATCH${NC} (main uses $MAIN_VERSION)"
            ISSUES_FOUND=$((ISSUES_FOUND + 1))
        else
            echo -e " ${GREEN}✓${NC}"
        fi
        
        # Get forge dependency version
        FORGE_DEP=$(grep "github.com/xraph/forge " "extensions/$ext/go.mod" | awk '{print $2}' | head -1)
        echo -n "  Forge Dependency: $FORGE_DEP"
        
        # Check if it's a valid version
        if [[ "$FORGE_DEP" == "v0.0.0" ]] || [[ "$FORGE_DEP" == "v0.0.1" ]]; then
            echo -e " ${YELLOW}⚠ PLACEHOLDER${NC}"
            ISSUES_FOUND=$((ISSUES_FOUND + 1))
        elif [ -n "$MAIN_TAG" ] && [ "$FORGE_DEP" != "$MAIN_TAG" ]; then
            echo -e " ${YELLOW}⚠ OUTDATED${NC} (main is at $MAIN_TAG)"
            ISSUES_FOUND=$((ISSUES_FOUND + 1))
        else
            echo -e " ${GREEN}✓${NC}"
        fi
        
        # Check for replace directive
        if grep -q "^replace github.com/xraph/forge" "extensions/$ext/go.mod"; then
            echo -e "  Replace Directive: ${GREEN}✓ Present${NC}"
        else
            echo -e "  Replace Directive: ${RED}✗ MISSING${NC}"
            ISSUES_FOUND=$((ISSUES_FOUND + 1))
        fi
        
        # Get latest tag for extension
        EXT_TAG=$(git tag -l "extensions/$ext/v*" --sort=-version:refname | head -1)
        if [ -n "$EXT_TAG" ]; then
            echo "  Latest Tag: $EXT_TAG"
        else
            echo -e "  Latest Tag: ${YELLOW}(none)${NC}"
        fi
        
        echo ""
    fi
done

# Summary
echo -e "${BLUE}=== Summary ===${NC}"
if [ $ISSUES_FOUND -eq 0 ]; then
    echo -e "${GREEN}✓ All modules are properly configured${NC}"
    exit 0
else
    echo -e "${RED}✗ Found $ISSUES_FOUND issue(s)${NC}"
    echo ""
    echo "Common fixes:"
    echo ""
    echo "1. Align Go versions:"
    echo "   cd extensions/<module>"
    echo "   go mod edit -go=$MAIN_VERSION"
    echo ""
    echo "2. Update forge dependency (after main release):"
    echo "   cd extensions/<module>"
    echo "   go get github.com/xraph/forge@$MAIN_TAG"
    echo "   go mod tidy"
    echo ""
    echo "3. Add replace directive:"
    echo "   cd extensions/<module>"
    echo "   go mod edit -replace=github.com/xraph/forge=../.."
    echo ""
    exit 1
fi

