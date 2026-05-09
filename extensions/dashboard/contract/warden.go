package contract

// Decision is the result returned by a Warden's second-pass authorization
// check. Phase 4 will extend this file with the Warden interface, Principal,
// Action, and the warden registry. For now only the Decision value type is
// declared so the predicate evaluator can reference it.
type Decision struct {
	// Allow reports whether access is granted.
	Allow bool
	// Reason is a short, human-readable explanation. Surfaced in audit logs
	// and (optionally) in error responses.
	Reason string
	// Redactions lists field paths that must be redacted from the response
	// payload even when Allow is true. Empty when no redactions apply.
	Redactions []string
}
