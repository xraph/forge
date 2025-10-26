#!/bin/bash

# find_noisy_tests.sh
# Finds test files that use noisy loggers and could benefit from migration

echo "🔍 Finding test files with noisy logger usage..."
echo ""

# Find all test files that create apps without using forgetesting
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "📁 Test files using forge.NewApp (may need migration):"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

# Search for forge.NewApp usage in test files
FORGE_NEWAPP_FILES=$(grep -r "forge\.NewApp" --include="*_test.go" . 2>/dev/null | \
    grep -v "forgetesting" | \
    cut -d: -f1 | \
    sort -u)

if [ -z "$FORGE_NEWAPP_FILES" ]; then
    echo "✅ No files found - all tests are clean!"
else
    echo "$FORGE_NEWAPP_FILES" | while read file; do
        count=$(grep -c "forge\.NewApp" "$file" 2>/dev/null || echo 0)
        echo "  📝 $file ($count occurrences)"
    done
    
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "📊 Statistics:"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    
    total_files=$(echo "$FORGE_NEWAPP_FILES" | wc -l | tr -d ' ')
    total_occurrences=$(echo "$FORGE_NEWAPP_FILES" | xargs grep -h "forge\.NewApp" 2>/dev/null | wc -l | tr -d ' ')
    
    echo "  Total files: $total_files"
    echo "  Total occurrences: $total_occurrences"
    echo "  Average per file: $((total_occurrences / total_files))"
fi

echo ""

# Find tests that already use forgetesting (migrated)
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "✅ Test files already using forgetesting (migrated):"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

MIGRATED_FILES=$(grep -r "forgetesting\." --include="*_test.go" . 2>/dev/null | \
    cut -d: -f1 | \
    sort -u)

if [ -z "$MIGRATED_FILES" ]; then
    echo "  (none yet)"
else
    echo "$MIGRATED_FILES" | while read file; do
        echo "  ✓ $file"
    done
fi

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "🎯 Migration Suggestions:"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

if [ -z "$FORGE_NEWAPP_FILES" ]; then
    echo "  🎉 All tests are already migrated!"
else
    echo ""
    echo "  To migrate a test file:"
    echo ""
    echo "  1. Add import:"
    echo "     import forgetesting \"github.com/xraph/forge/testing\""
    echo ""
    echo "  2. Replace:"
    echo "     app := forge.NewApp(forge.AppConfig{Name: \"test\", Version: \"1.0.0\"})"
    echo ""
    echo "  3. With:"
    echo "     app := forgetesting.NewTestApp(\"test\", \"1.0.0\")"
    echo ""
    echo "  See testing/README.md for full documentation"
fi

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

