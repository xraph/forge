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

	h := newRateLimitedErrorHandler(capture, time.Minute)

	h.Handle(errors.New("first"))
	h.Handle(errors.New("second"))

	warns := capture.GetLogsByLevel("WARN")
	if len(warns) != 1 || warns[0].Message != "telemetry: export failed" {
		t.Fatalf("warn lines = %v, want one telemetry: export failed", warns)
	}
}
