package streaming

import (
	"context"
	"errors"
	"testing"

	"github.com/xraph/forge/extensions/streaming/ratelimit"
)

// sendUntilLimited sends system frames until the limiter refuses one and
// returns how many got through, or -1 if none was refused.
func sendUntilLimited(m Manager, conn Connection, limit int) int {
	for i := range limit {
		_, err := m.ProcessInbound(context.Background(), &Message{Type: MessageTypeSystem, Data: "x"}, conn)
		if errors.Is(err, ErrRateLimitExceeded) {
			return i
		}
	}

	return -1
}

// ProcessInbound skipped the rate limiter whenever the sender had no user id,
// so an anonymous socket could send without limit.
func TestProcessInbound_RateLimitsAnonymousConnections(t *testing.T) {
	rl := ratelimit.NewTokenBucket(ratelimit.DefaultRateLimitConfig(), nil)
	m, _, _ := realManager(t, WithRateLimiter(rl))

	first := newTestConn("anon-1", "")
	second := newTestConn("anon-2", "")

	for _, c := range []*testConn{first, second} {
		assertNoError(t, m.Register(c))
	}

	if n := sendUntilLimited(m, first, 1000); n < 0 {
		t.Fatal("anonymous connection sent 1000 messages without being rate limited")
	}

	// The budget is per connection: one noisy anonymous client does not use
	// up another's, as it would if every client behind one proxy shared an
	// address-based bucket.
	if n := sendUntilLimited(m, second, 1); n == 0 {
		t.Fatal("a second anonymous connection inherited the first one's exhausted budget")
	}
}

func TestRateLimitKey(t *testing.T) {
	user := newTestConn("c1", "alice")
	anon := newTestConn("c2", "")

	if got := rateLimitKey(user); got != "alice" {
		t.Errorf("authenticated key = %q, want the user id", got)
	}

	if got := rateLimitKey(anon); got != "anon:conn:c2" {
		t.Errorf("anonymous key = %q, want one derived from the connection id", got)
	}

	if got := rateLimitKey(nil); got != "" {
		t.Errorf("nil sender key = %q, want empty", got)
	}
}
