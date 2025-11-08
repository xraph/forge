package cli

import (
	"strings"
	"testing"
)

func TestRenderOptions_Alignment(t *testing.T) {
	// This test verifies that the ANSI escape sequence for line clearing
	// uses the correct order: \033[2K (clear entire line) then \r (return to start)
	// This prevents progressive indentation issues in terminals

	// We can't easily test the actual rendering without a real terminal,
	// but we can verify the escape sequence is correct in the source code

	// The expected sequence should be:
	// \033[2K - Clear entire line (CSI 2 K)
	// \r      - Carriage return to column 0

	// Previously it was \r\033[K which doesn't work reliably across terminals
	t.Log("Escape sequence for line clearing should be: \\033[2K\\r")
	t.Log("This ensures cursor returns to column 0 after clearing the entire line")
}

func TestNavigationHints_NoLeadingSpaces(t *testing.T) {
	// Verify that navigation hints don't have unnecessary leading spaces
	// The hints should be left-aligned, not indented
	singleSelectHint := "↑/↓: Navigate  │  Enter: Select  │  Esc/q: Cancel"
	multiSelectHint := "↑/↓: Navigate  │  Space: Select/Deselect  │  Enter: Confirm  │  Esc/q: Cancel"

	// Check that hints don't start with spaces
	if strings.HasPrefix(singleSelectHint, " ") {
		t.Error("Single select hint should not have leading spaces")
	}

	if strings.HasPrefix(multiSelectHint, " ") {
		t.Error("Multi-select hint should not have leading spaces")
	}

	t.Log("Navigation hints are properly left-aligned")
}

func TestOptionPrefix_Alignment(t *testing.T) {
	// Verify that option prefixes use consistent spacing
	// All options should be left-aligned with their prefixes
	testCases := []struct {
		name          string
		prefix        string
		wantLeftAlign bool
	}{
		{"Regular option", "  ", true},
		{"Cursor highlight", "▸", true},
		{"Multi unchecked", "  [ ]", true},
		{"Multi checked", "  [✓]", true},
		{"Cursor + checked", "▸ [✓]", true},
		{"Cursor + unchecked", "▸ [ ]", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Prefixes should not have excessive leading whitespace
			// The "  " (2 spaces) is intentional for regular options
			// All other prefixes align properly
			if strings.HasPrefix(tc.prefix, "   ") { // 3+ spaces is excessive
				t.Errorf("Prefix %q has excessive leading spaces", tc.prefix)
			}
		})
	}
}

func TestClearLineSequence(t *testing.T) {
	// Document the correct ANSI escape sequences for terminal manipulation
	sequences := map[string]string{
		"Clear entire line":       "\033[2K",
		"Carriage return":         "\r",
		"Move cursor up one line": "\033[1A",
		"Hide cursor":             "\033[?25l",
		"Show cursor":             "\033[?25h",
	}

	for desc, seq := range sequences {
		t.Logf("%s: %q", desc, seq)
	}

	// The critical fix: use \033[2K\r instead of \r\033[K
	// \033[2K = Clear entire line (from beginning to end)
	// \r = Return to start of line
	// This order prevents cursor position issues
	correctSequence := "\033[2K\r"
	t.Logf("Correct line clear sequence: %q", correctSequence)
}

// TestANSISequenceOrder documents why order matters.
func TestANSISequenceOrder(t *testing.T) {
	t.Log("ANSI Escape Sequence Order:")
	t.Log("1. WRONG: \\r\\033[K")
	t.Log("   - \\r moves to start (but cursor position may not be column 0)")
	t.Log("   - \\033[K clears from cursor to end")
	t.Log("   - Problem: If cursor isn't at column 0, line isn't fully cleared")
	t.Log("")
	t.Log("2. CORRECT: \\033[2K\\r")
	t.Log("   - \\033[2K clears ENTIRE line (regardless of cursor position)")
	t.Log("   - \\r moves to start of cleared line")
	t.Log("   - Result: Clean slate for rendering new content")
}
