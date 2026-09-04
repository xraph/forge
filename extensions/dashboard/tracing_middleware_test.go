package dashboard

import (
	"strings"
	"testing"
	"unicode/utf8"
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
	got := truncateAttr(strings.Repeat("é", 500), 258)

	if !utf8.ValidString(got) {
		t.Errorf("truncation produced invalid UTF-8: %q", got[len(got)-8:])
	}
	if len(got) > 258 {
		t.Errorf("truncateAttr returned %d bytes, want at most 258", len(got))
	}
	// cut lands mid-rune at 255, so the backtrack must have moved it to 254.
	if want := 254 + len("..."); len(got) != want {
		t.Errorf("truncateAttr returned %d bytes, want %d — the rune backtrack did not run", len(got), want)
	}
}

func TestMaxAttrValueLen_IsSane(t *testing.T) {
	if maxAttrValueLen < 64 || maxAttrValueLen > 4096 {
		t.Errorf("maxAttrValueLen is %d, which is outside a sensible range", maxAttrValueLen)
	}
}

func TestTruncateAttr_SmallMaxDoesNotPanic(t *testing.T) {
	for _, max := range []int{0, 1, 2, 3, 4} {
		got := truncateAttr("hello world, this is long", max)
		if len(got) > max {
			t.Errorf("truncateAttr with max=%d returned %d bytes", max, len(got))
		}
	}
}

func TestTruncateAttr_InvalidUTF8DoesNotPanic(t *testing.T) {
	// A raw query string or user agent can carry arbitrary bytes.
	got := truncateAttr(strings.Repeat("\xff\xfe", 500), 257)

	if len(got) > 257 {
		t.Errorf("truncateAttr returned %d bytes, want at most 257", len(got))
	}
}

func TestIsDashboardPath(t *testing.T) {
	const base = "/dashboard"

	cases := []struct {
		path string
		want bool
	}{
		{"/dashboard", true},
		{"/dashboard/", true},
		{"/dashboard/ui", true},
		{"/dashboard/api/dashboard/v1", true},
		{"/dashboard/static/app.css", true},
		{"/api/users", false},
		{"/", false},
		{"/dashboards-elsewhere", true}, // prefix match, documented below
	}

	for _, c := range cases {
		if got := isDashboardPath(c.path, base); got != c.want {
			t.Errorf("isDashboardPath(%q, %q) = %v, want %v", c.path, base, got, c.want)
		}
	}
}
