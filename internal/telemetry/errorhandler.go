package telemetry

import (
	"sync"
	"time"

	"github.com/xraph/forge/internal/logger"
)

// rateLimitedErrorHandler logs OTel errors (failed exports, mostly) at warn,
// at most once per interval, with the number of errors it swallowed since.
// A collector that is down would otherwise log on every batch.
type rateLimitedErrorHandler struct {
	log        logger.Logger
	every      time.Duration
	now        func() time.Time
	mu         sync.Mutex
	last       time.Time
	suppressed int
}

func newRateLimitedErrorHandler(log logger.Logger, every time.Duration) *rateLimitedErrorHandler {
	return &rateLimitedErrorHandler{log: log, every: every, now: time.Now}
}

// Handle implements otel.ErrorHandler.
func (h *rateLimitedErrorHandler) Handle(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := h.now()
	if !h.last.IsZero() && now.Sub(h.last) < h.every {
		h.suppressed++

		return
	}

	h.log.Warn("telemetry: export failed",
		logger.String("error", err.Error()),
		logger.Int("suppressed", h.suppressed),
	)

	h.last = now
	h.suppressed = 0
}
