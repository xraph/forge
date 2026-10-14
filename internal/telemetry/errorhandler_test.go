package telemetry

import (
	"errors"
	"testing"
	"time"

	"github.com/xraph/go-utils/log"

	"github.com/xraph/forge/internal/logger"
)

func TestErrorHandlerRateLimits(t *testing.T) {
	capture, ok := logger.NewTestLogger().(*log.TestLogger)
	if !ok {
		t.Fatal("NewTestLogger did not return *log.TestLogger")
	}

	now := time.Unix(0, 0)

	h := newRateLimitedErrorHandler(capture, time.Minute)
	h.now = func() time.Time { return now }

	h.Handle(errors.New("first"))
	h.Handle(errors.New("second"))
	h.Handle(errors.New("third"))

	now = now.Add(61 * time.Second)

	h.Handle(errors.New("fourth"))

	if got := capture.CountLogs("WARN"); got != 2 {
		t.Fatalf("warn lines = %d, want 2", got)
	}
}
