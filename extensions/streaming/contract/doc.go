// Package contract is the streaming extension's dashboard contract contributor.
// It is the slice (f) migration target — a parallel implementation of the
// streaming dashboard surface using the new declarative contract instead of
// templ rendering. Both paths coexist during the migration window:
//
//	/dashboard/ext/streaming/...      (legacy templ contributor — slice f keeps unchanged)
//	/dashboard/contract/streaming-contract/...  (this package — new contract path)
//
// See SLICE_F_DESIGN.md in extensions/dashboard/contract/ for the design spec.
package contract
