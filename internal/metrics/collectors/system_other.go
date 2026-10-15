//go:build !linux && !windows

package collectors

// CPU, memory and load have no implementation outside Linux and Windows yet.
// macOS needs Mach host statistics for these, which take cgo.

func readCPUStats() (*CPUStats, error) { return nil, errUnsupported }

func readMemoryStats() (*MemoryStats, error) { return nil, errUnsupported }

func readLoadStats() (*LoadStats, error) { return nil, errUnsupported }
