package postgres

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// listener holds one dedicated connection for LISTEN. A pooled connection can't be used:
// PgBouncer in transaction mode would drop the registration between statements.
type listener struct {
	config    *pgx.ConnConfig
	logger    *slog.Logger
	mu        sync.Mutex
	waiters   map[watchKey]map[chan struct{}]struct{}
	connected atomic.Bool
}

// watchKey is a channel alone, or a channel and the one payload a watcher waits for.
type watchKey struct{ channel, id string }

// newListener takes a nil logger to log nothing.
func newListener(config *pgx.ConnConfig, logger *slog.Logger) *listener {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	return &listener{config: config, logger: logger, waiters: map[watchKey]map[chan struct{}]struct{}{}}
}

func listenerName(owner string) string { return "forge-conduit-listener-" + owner }

// watch returns a signal for each notification on channel, and for every reconnect.
func (l *listener) watch(channel string) (<-chan struct{}, func()) {
	return l.add(watchKey{channel: channel})
}

// watchID returns a signal for each notification on channel whose payload is id, and for
// every reconnect.
func (l *listener) watchID(channel, id string) (<-chan struct{}, func()) {
	return l.add(watchKey{channel: channel, id: id})
}

func (l *listener) add(key watchKey) (<-chan struct{}, func()) {
	signal := make(chan struct{}, 1)

	l.mu.Lock()
	if l.waiters[key] == nil {
		l.waiters[key] = map[chan struct{}]struct{}{}
	}

	l.waiters[key][signal] = struct{}{}
	l.mu.Unlock()

	return signal, func() {
		l.mu.Lock()
		delete(l.waiters[key], signal)

		if len(l.waiters[key]) == 0 {
			delete(l.waiters, key)
		}
		l.mu.Unlock()
	}
}

// notify wakes the channel's watchers, and the watchers waiting for this payload.
func (l *listener) notify(channel, payload string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	wake(l.waiters[watchKey{channel: channel}])

	if payload != "" {
		wake(l.waiters[watchKey{channel: channel, id: payload}])
	}
}

func (l *listener) notifyAll() {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, set := range l.waiters {
		wake(set)
	}
}

func wake(set map[chan struct{}]struct{}) {
	for signal := range set {
		select {
		case signal <- struct{}{}:
		default:
		}
	}
}

func (l *listener) channels() []string {
	l.mu.Lock()
	defer l.mu.Unlock()

	seen := make(map[string]bool, len(l.waiters))
	channels := make([]string, 0, len(l.waiters))

	for key := range l.waiters {
		if !seen[key.channel] {
			seen[key.channel] = true
			channels = append(channels, key.channel)
		}
	}

	return channels
}

// run keeps a listening connection open until ctx ends, reconnecting with backoff.
// While it is down, subscribers keep delivering by polling.
//
// Outages are logged with the error alone. pgx connect errors name the host, user and
// database but never the password, so nothing here can leak it.
func (l *listener) run(ctx context.Context) {
	var (
		limit warnLimit
		down  time.Time // when the current outage began; zero while connected
	)

	backoff := 100 * time.Millisecond

	for ctx.Err() == nil {
		conn, err := pgx.ConnectConfig(ctx, l.config)
		if err != nil {
			if ctx.Err() != nil {
				return
			}

			if down.IsZero() {
				down = time.Now()
			}

			if limit.allow(time.Now(), backoff) {
				l.logger.WarnContext(ctx, "conduit/postgres: listener cannot connect; delivery continues by polling", "error", err, "retry", backoff)
			}

			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}

			backoff = min(backoff*2, 5*time.Second)

			continue
		}

		if !down.IsZero() {
			l.logger.InfoContext(ctx, "conduit/postgres: listener reconnected", "down", time.Since(down).Round(time.Millisecond))
		}

		backoff, limit, down = 100*time.Millisecond, warnLimit{}, time.Time{}

		l.connected.Store(true)
		l.notifyAll() // anything published while disconnected is found by the next read
		err = l.serve(ctx, conn)
		l.connected.Store(false)

		closing, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		_ = conn.Close(closing)

		cancel()

		if ctx.Err() == nil {
			down = time.Now()

			l.logger.WarnContext(ctx, "conduit/postgres: listener connection dropped; delivery continues by polling", "error", err)
		}
	}
}

// serve listens on conn until ctx ends or the connection fails, and returns the failure.
func (l *listener) serve(ctx context.Context, conn *pgx.Conn) error {
	listening := map[string]bool{}

	for ctx.Err() == nil {
		// Channels nobody watches any more stay registered: LISTEN is cheap, and UNLISTEN
		// would race a watcher arriving for the same channel.
		for _, channel := range l.channels() {
			if listening[channel] {
				continue
			}

			if _, err := conn.Exec(ctx, "LISTEN "+pgx.Identifier{channel}.Sanitize()); err != nil {
				return err
			}

			listening[channel] = true
		}

		wait, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
		notification, err := conn.WaitForNotification(wait)

		cancel()

		if err == nil {
			if notification != nil {
				l.notify(notification.Channel, notification.Payload)
			}

			continue
		}

		if ctx.Err() != nil || conn.IsClosed() || (!errors.Is(err, context.DeadlineExceeded) && !pgconn.Timeout(err)) {
			return err
		}
	}

	return ctx.Err()
}

// warnInterval is how often a database that stays down is warned about once the backoff
// has stopped growing.
const warnInterval = 30 * time.Second

// warnLimit decides which failed connect attempts get a warning: one for each new
// backoff step, then one per warnInterval.
type warnLimit struct {
	last    time.Time
	backoff time.Duration
}

func (w *warnLimit) allow(now time.Time, backoff time.Duration) bool {
	if backoff == w.backoff && now.Sub(w.last) < warnInterval {
		return false
	}

	w.last, w.backoff = now, backoff

	return true
}
