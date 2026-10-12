package postgres

import (
	"context"
	"errors"
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
	mu        sync.Mutex
	waiters   map[watchKey]map[chan struct{}]struct{}
	connected atomic.Bool
}

// watchKey is a channel alone, or a channel and the one payload a watcher waits for.
type watchKey struct{ channel, id string }

func newListener(config *pgx.ConnConfig) *listener {
	return &listener{config: config, waiters: map[watchKey]map[chan struct{}]struct{}{}}
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
func (l *listener) run(ctx context.Context) {
	backoff := 100 * time.Millisecond

	for ctx.Err() == nil {
		conn, err := pgx.ConnectConfig(ctx, l.config)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}

			backoff = min(backoff*2, 5*time.Second)

			continue
		}

		backoff = 100 * time.Millisecond

		l.connected.Store(true)
		l.notifyAll() // anything published while disconnected is found by the next read
		l.serve(ctx, conn)
		l.connected.Store(false)

		closing, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		_ = conn.Close(closing)

		cancel()
	}
}

func (l *listener) serve(ctx context.Context, conn *pgx.Conn) {
	listening := map[string]bool{}

	for ctx.Err() == nil {
		// Channels nobody watches any more stay registered: LISTEN is cheap, and UNLISTEN
		// would race a watcher arriving for the same channel.
		for _, channel := range l.channels() {
			if listening[channel] {
				continue
			}

			if _, err := conn.Exec(ctx, "LISTEN "+pgx.Identifier{channel}.Sanitize()); err != nil {
				return
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
			return
		}
	}
}
