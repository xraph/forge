// Package idempotency provides command deduplication for the dashboard
// contract: a Store interface shaped for the dispatcher, and InMemoryStore,
// which keeps its entries in a middleware.IdempotencyStore. That is the same
// store the HTTP idempotency middleware (forge.WithIdempotency) uses, so one
// backend can serve both. Wrappers around dispatcher.Dispatch consult the
// store before invoking command handlers and return cached envelopes when the
// (key, identity) tuple matches a recent invocation. InMemoryStore is also a
// Claimer: it holds a key while a command runs, through the shared store's own
// claims, so two overlapping dispatches with one key never both run it.
package idempotency
