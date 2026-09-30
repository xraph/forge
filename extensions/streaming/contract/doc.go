// Package contract is the streaming extension's dashboard contract contributor.
// It registers the streaming intents (channels, rooms, connections, presence,
// stats) with the dashboard's dispatcher. The UI that reads them is the
// @forge-go/dashboard-plugin-streaming React plugin; the Go side serves data
// only.
//
// See SLICE_F_DESIGN.md in extensions/dashboard/contract/ for the design spec.
package contract
