package logger

import (
	"fmt"
	"testing"
	"time"
)

func TestRateLimitedLogsOncePerInterval(t *testing.T) {
	capture, ok := NewTestLogger().(*TestLogger)
	if !ok {
		t.Fatal("NewTestLogger did not return *TestLogger")
	}

	now := time.Unix(0, 0)

	r := NewRateLimited(capture, "profiling: upload failed", time.Minute)
	r.now = func() time.Time { return now }

	r.Report("first")
	r.Report("second")
	r.Report("third")

	now = now.Add(61 * time.Second)

	r.Report("fourth")

	warns := capture.GetLogsByLevel("WARN")
	if len(warns) != 2 {
		t.Fatalf("warn lines = %d, want 2", len(warns))
	}

	if warns[0].Message != "profiling: upload failed" {
		t.Fatalf("message = %q", warns[0].Message)
	}

	if got := fmt.Sprint(fieldOf(warns[1].Fields, "suppressed")); got != "2" {
		t.Fatalf("suppressed = %v, want 2", got)
	}
}

func fieldOf(fields []Field, key string) any {
	for _, f := range fields {
		if f.Key() == key {
			return f.Value()
		}
	}

	return nil
}
