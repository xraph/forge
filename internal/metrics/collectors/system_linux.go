package collectors

import (
	"fmt"
	"os"
)

func readCPUStats() (*CPUStats, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return nil, fmt.Errorf("failed to read /proc/stat: %w", err)
	}

	return parseProcStat(data)
}

func readMemoryStats() (*MemoryStats, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return nil, fmt.Errorf("failed to read /proc/meminfo: %w", err)
	}

	return parseMeminfo(data)
}

func readLoadStats() (*LoadStats, error) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return nil, fmt.Errorf("failed to read /proc/loadavg: %w", err)
	}

	return parseLoadavg(data)
}
