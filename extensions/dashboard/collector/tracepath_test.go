package collector

import "testing"

func TestSkipTracePath(t *testing.T) {
	cases := map[string]bool{
		"/dashboard/static/app.js": true,
		"/dashboard/sse":           true,
		"/dashboard/bridge/call":   true,
		"/dashboard":               false,
		"/dashboard/api/contract":  false,
		"/assets/static/logo.png":  true,
		"/app.css":                 true,
		"/events/sse":              true,
		"/api/users":               false,
		"":                         false,
	}

	for path, want := range cases {
		if got := SkipTracePath(path, "/dashboard"); got != want {
			t.Errorf("SkipTracePath(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := Truncate("short", 10); got != "short" {
		t.Fatalf("got %q", got)
	}

	if got := Truncate("0123456789abc", 10); got != "0123456..." {
		t.Fatalf("got %q", got)
	}
}
