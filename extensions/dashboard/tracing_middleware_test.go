package dashboard

import (
	"strings"
	"testing"
)

func TestTruncateAttr_LeavesShortValuesAlone(t *testing.T) {
	if got := truncateAttr("hello", 256); got != "hello" {
		t.Errorf("truncateAttr shortened a short value to %q", got)
	}
}

func TestTruncateAttr_CutsLongValuesAndMarksThem(t *testing.T) {
	got := truncateAttr(strings.Repeat("a", 1000), 256)

	if len(got) > 256 {
		t.Errorf("truncateAttr returned %d bytes, want at most 256", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("truncated value %q does not end in an ellipsis marker", got[len(got)-10:])
	}
}

// A truncation boundary in the middle of a multi-byte rune must not produce
// invalid UTF-8, because these values are marshalled to JSON for the UI.
func TestTruncateAttr_DoesNotSplitRunes(t *testing.T) {
	got := truncateAttr(strings.Repeat("é", 500), 257)

	for i, r := range got {
		if r == '�' {
			t.Fatalf("truncation produced an invalid rune at byte %d", i)
		}
	}
}

func TestMaxAttrValueLen_IsSane(t *testing.T) {
	if maxAttrValueLen < 64 || maxAttrValueLen > 4096 {
		t.Errorf("maxAttrValueLen is %d, which is outside a sensible range", maxAttrValueLen)
	}
}
