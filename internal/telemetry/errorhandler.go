package telemetry

import (
	"time"

	"github.com/xraph/forge/internal/logger"
)

// rateLimitedErrorHandler logs OTel errors (failed exports, mostly) at warn,
// at most once per interval. A collector that is down would otherwise log on
// every batch.
type rateLimitedErrorHandler struct {
	limiter *logger.RateLimited
}

func newRateLimitedErrorHandler(log logger.Logger, every time.Duration) *rateLimitedErrorHandler {
	return &rateLimitedErrorHandler{limiter: logger.NewRateLimited(log, "telemetry: export failed", every)}
}

// Handle implements otel.ErrorHandler.
func (h *rateLimitedErrorHandler) Handle(err error) { h.limiter.Report(err.Error()) }
