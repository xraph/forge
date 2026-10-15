//go:build race

package telemetry

// raceEnabled reports a -race build, where the detector adds allocations of
// its own and allocation counts mean nothing.
const raceEnabled = true
