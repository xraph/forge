// Package idempotency provides command deduplication for the dashboard
// contract: a Store interface plus an in-memory implementation. Wrappers
// around dispatcher.Dispatch consult the store before invoking command
// handlers and return cached envelopes when the (key, identity) tuple
// matches a recent invocation.
package idempotency
