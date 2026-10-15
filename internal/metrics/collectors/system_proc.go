package collectors

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// The /proc parsers take bytes rather than paths so they can be tested on any
// platform. system_linux.go feeds them the real files.

// parseProcStat reads the aggregate "cpu " line of /proc/stat.
func parseProcStat(data []byte) (*CPUStats, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	if !scanner.Scan() {
		return nil, errors.New("empty /proc/stat")
	}

	line := scanner.Text()
	if !strings.HasPrefix(line, "cpu ") {
		return nil, errors.New("invalid format in /proc/stat")
	}

	// cpu user nice system idle iowait irq softirq steal guest guest_nice
	fields := strings.Fields(line)
	if len(fields) < 5 {
		return nil, errors.New("insufficient fields in /proc/stat")
	}

	values := make([]uint64, 10)

	for i := 1; i < len(fields) && i <= len(values); i++ {
		val, err := strconv.ParseUint(fields[i], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse CPU stat: %w", err)
		}

		values[i-1] = val
	}

	stats := &CPUStats{
		User:      values[0],
		Nice:      values[1],
		System:    values[2],
		Idle:      values[3],
		IOWait:    values[4],
		IRQ:       values[5],
		SoftIRQ:   values[6],
		Steal:     values[7],
		Guest:     values[8],
		GuestNice: values[9],
	}

	stats.Total = stats.User + stats.Nice + stats.System + stats.Idle +
		stats.IOWait + stats.IRQ + stats.SoftIRQ + stats.Steal

	return stats, nil
}

// parseMeminfo reads /proc/meminfo, whose values are in kB.
func parseMeminfo(data []byte) (*MemoryStats, error) {
	values := make(map[string]uint64)

	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}

		values[strings.TrimSuffix(fields[0], ":")] = value * 1024
	}

	total, ok := values["MemTotal"]
	if !ok || total == 0 {
		return nil, errors.New("no MemTotal in /proc/meminfo")
	}

	stats := &MemoryStats{
		Total:        total,
		Free:         values["MemFree"],
		Buffers:      values["Buffers"],
		Cached:       values["Cached"],
		SwapTotal:    values["SwapTotal"],
		SwapFree:     values["SwapFree"],
		HasPageCache: true,
		HasSwap:      true,
	}

	// Kernels before 3.14 have no MemAvailable. Free plus the page cache is
	// the estimate tools used before it existed.
	if available, ok := values["MemAvailable"]; ok {
		stats.Available = available
	} else {
		stats.Available = stats.Free + stats.Buffers + stats.Cached
	}

	if stats.Available > stats.Total {
		stats.Available = stats.Total
	}

	stats.Used = stats.Total - stats.Available

	if stats.SwapFree <= stats.SwapTotal {
		stats.SwapUsed = stats.SwapTotal - stats.SwapFree
	}

	return stats, nil
}

// parseLoadavg reads the first three fields of /proc/loadavg.
func parseLoadavg(data []byte) (*LoadStats, error) {
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return nil, errors.New("invalid format in /proc/loadavg")
	}

	var loads [3]float64

	for i := range loads {
		v, err := strconv.ParseFloat(fields[i], 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse load average: %w", err)
		}

		loads[i] = v
	}

	return &LoadStats{Load1: loads[0], Load5: loads[1], Load15: loads[2]}, nil
}
