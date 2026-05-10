// Package dispatcher implements transport.Dispatcher and transport.SubscriptionSource
// against a function-table of registered handlers. Contributors register their
// intent handlers via Register / RegisterSubscription / RegisterContributor;
// the HTTP and SSE transports look them up at request time.
//
// See SLICE_C_DESIGN.md in the parent contract directory for the spec this implements.
package dispatcher
