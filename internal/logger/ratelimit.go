package logger

import (
	"sync"
	"time"
)

// RateLimited logs a failure that tends to repeat (an export or an upload to
// a backend that is down) at warn, at most once per interval, with the number
// of reports it swallowed since.
type RateLimited struct {
	log        Logger
	msg        string
	every      time.Duration
	now        func() time.Time
	mu         sync.Mutex
	last       time.Time
	suppressed int
}

// NewRateLimited returns a RateLimited that logs msg through log.
func NewRateLimited(log Logger, msg string, every time.Duration) *RateLimited {
	return &RateLimited{log: log, msg: msg, every: every, now: time.Now}
}

// Report logs detail as the error field, unless the last line went out less
// than an interval ago.
func (r *RateLimited) Report(detail string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	if !r.last.IsZero() && now.Sub(r.last) < r.every {
		r.suppressed++

		return
	}

	r.log.Warn(r.msg, String("error", detail), Int("suppressed", r.suppressed))

	r.last = now
	r.suppressed = 0
}
