package dashboard

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/dashboard/collector"
	forge_http "github.com/xraph/go-utils/http"
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
		{"/dashboards-elsewhere", false}, // segment-boundary match, not a bare prefix
		{"/dashboard-admin", false},      // unrelated sibling route must not hold the gate open
	}

	for _, c := range cases {
		if got := isDashboardPath(c.path, base); got != c.want {
			t.Errorf("isDashboardPath(%q, %q) = %v, want %v", c.path, base, got, c.want)
		}
	}
}

// TestTracingMiddleware_StampsAccessOnDashboardRequests drives the real
// TracingMiddleware end to end (not just the isDashboardPath helper) to prove
// the stamp is actually wired into the request path. This is the wiring that
// TestIsDashboardPath cannot see: deleting the "if isDashboardPath(...) {
// store.MarkAccessed() }" block in TracingMiddleware leaves that test green
// (isDashboardPath itself is untouched) while silently breaking the whole
// gate — the trace list would stay empty forever. Static assets and SSE
// requests under the dashboard are not themselves traced (TracingMiddleware
// returns early for them) but must still count as activity; a request outside
// the dashboard must not.
func TestTracingMiddleware_StampsAccessOnDashboardRequests(t *testing.T) {
	const basePath = "/dashboard"

	noopNext := func(ctx forge.Context) error { return nil }

	cases := []struct {
		name string
		path string
		want bool // whether LastAccessed should be non-zero after the request
	}{
		{"static asset under dashboard counts as activity", basePath + "/static/app.css", true},
		{"SSE stream under dashboard counts as activity", basePath + "/sse", true},
		{"unrelated API route does not count as activity", "/api/users", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Fresh store per case so the cases cannot contaminate each other.
			store := collector.NewTraceStore(10, time.Hour)

			req := httptest.NewRequest("GET", c.path, nil)
			w := httptest.NewRecorder()
			ctx := forge_http.NewContext(w, req, nil)

			middleware := TracingMiddleware(store, basePath)
			handler := middleware(noopNext)

			if err := handler(ctx); err != nil {
				t.Fatalf("unexpected error handling %s: %v", c.path, err)
			}

			gotAccessed := !store.LastAccessed().IsZero()
			if gotAccessed != c.want {
				t.Errorf("after request to %q, LastAccessed().IsZero() reports accessed=%v, want %v", c.path, gotAccessed, c.want)
			}
		})
	}
}
