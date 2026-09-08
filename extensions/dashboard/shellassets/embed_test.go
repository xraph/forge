package shellassets

import (
	"io/fs"
	"testing"
)

// TestIsPlaceholder pins the sentinel to the committed placeholder. Edit the
// meta tag in dist/index.html without editing placeholderSentinel and the match
// stops silently: the extension's startup warning stops firing, and the only
// symptom is a log line that is not there. Nothing else would catch that.
//
// It also checks the other direction. A real artifact unpacked over dist/ must
// not be reported as the placeholder, or the warning cries wolf on every
// release build.
func TestIsPlaceholder(t *testing.T) {
	sub, err := FS()
	if err != nil {
		t.Fatalf("FS(): %v", err)
	}

	if _, err := fs.Stat(sub, "index.html"); err != nil {
		t.Fatalf("no index.html in the embedded shell: %v", err)
	}

	// A real Vite build always emits an assets/ directory; the placeholder
	// never has one. That tells the two apart without going through the
	// sentinel, which is the thing under test.
	if entries, dirErr := fs.ReadDir(sub, "assets"); dirErr == nil && len(entries) > 0 {
		if IsPlaceholder() {
			t.Fatal("a real shell artifact is embedded, but IsPlaceholder() reports the placeholder")
		}

		t.Skip("a real shell artifact is unpacked over dist/; nothing to pin here")
	}

	if !IsPlaceholder() {
		t.Fatalf("the committed placeholder is embedded but IsPlaceholder() says otherwise; "+
			"dist/index.html has probably lost the %q sentinel", placeholderSentinel)
	}
}
