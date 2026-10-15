package collectors

import (
	"math"
	"strings"
	"testing"
)

func TestParseProcStat(t *testing.T) {
	data := []byte("cpu  100 20 30 400 50 6 7 8 90 10\ncpu0 1 2 3 4 5 6 7 8 9 10\n")

	stats, err := parseProcStat(data)
	if err != nil {
		t.Fatalf("parseProcStat: %v", err)
	}

	want := CPUStats{
		User: 100, Nice: 20, System: 30, Idle: 400, IOWait: 50, IRQ: 6, SoftIRQ: 7, Steal: 8,
		Guest: 90, GuestNice: 10,
		Total: 100 + 20 + 30 + 400 + 50 + 6 + 7 + 8, // guest time sits inside user and nice
	}

	if *stats != want {
		t.Errorf("got %+v, want %+v", *stats, want)
	}
}

func TestParseProcStatShortLine(t *testing.T) {
	// Kernels before 2.6.11 stop after iowait.
	stats, err := parseProcStat([]byte("cpu 1 2 3 4 5\n"))
	if err != nil {
		t.Fatalf("parseProcStat: %v", err)
	}

	if stats.IOWait != 5 || stats.Steal != 0 || stats.Total != 15 {
		t.Errorf("got %+v", *stats)
	}
}

func TestParseProcStatRejectsGarbage(t *testing.T) {
	for _, in := range []string{"", "intr 1 2 3\n", "cpu 1 2\n", "cpu 1 x 3 4\n"} {
		if _, err := parseProcStat([]byte(in)); err == nil {
			t.Errorf("parseProcStat(%q) succeeded, want an error", in)
		}
	}
}

func TestParseMeminfo(t *testing.T) {
	data := []byte(`MemTotal:       16000 kB
MemFree:         2000 kB
MemAvailable:    9000 kB
Buffers:          500 kB
Cached:          6000 kB
SwapTotal:       4000 kB
SwapFree:        3000 kB
`)

	stats, err := parseMeminfo(data)
	if err != nil {
		t.Fatalf("parseMeminfo: %v", err)
	}

	want := MemoryStats{
		Total: 16000 * 1024, Available: 9000 * 1024, Used: 7000 * 1024, Free: 2000 * 1024,
		Buffers: 500 * 1024, Cached: 6000 * 1024,
		SwapTotal: 4000 * 1024, SwapFree: 3000 * 1024, SwapUsed: 1000 * 1024,
		HasPageCache: true, HasSwap: true,
	}

	if *stats != want {
		t.Errorf("got %+v, want %+v", *stats, want)
	}
}

func TestParseMeminfoWithoutMemAvailable(t *testing.T) {
	stats, err := parseMeminfo([]byte("MemTotal: 1000 kB\nMemFree: 100 kB\nBuffers: 50 kB\nCached: 250 kB\n"))
	if err != nil {
		t.Fatalf("parseMeminfo: %v", err)
	}

	if stats.Available != 400*1024 || stats.Used != 600*1024 {
		t.Errorf("got available %d used %d, want %d and %d", stats.Available, stats.Used, 400*1024, 600*1024)
	}
}

func TestParseMeminfoWithoutTotal(t *testing.T) {
	if _, err := parseMeminfo([]byte("MemFree: 100 kB\n")); err == nil {
		t.Error("parseMeminfo succeeded without MemTotal, want an error")
	}
}

func TestParseLoadavg(t *testing.T) {
	stats, err := parseLoadavg([]byte("0.52 1.25 2.00 3/812 41234\n"))
	if err != nil {
		t.Fatalf("parseLoadavg: %v", err)
	}

	if *stats != (LoadStats{Load1: 0.52, Load5: 1.25, Load15: 2}) {
		t.Errorf("got %+v", *stats)
	}

	if _, err := parseLoadavg([]byte("0.5 1.0")); err == nil {
		t.Error("parseLoadavg accepted two fields, want an error")
	}
}

func TestCPUUtilizationNeedsTwoSamples(t *testing.T) {
	sc := &SystemCollector{}
	out := map[string]any{}

	sc.addCPUMetrics(out, &CPUStats{User: 100, Idle: 100, Total: 200})

	if len(out) != 0 {
		t.Fatalf("first sample reported %v, want nothing", out)
	}

	sc.addCPUMetrics(out, &CPUStats{User: 175, Idle: 125, Total: 300})

	if got := out[`system.cpu.utilization{mode="user"}`]; got != 0.75 {
		t.Errorf("user = %v, want 0.75", got)
	}

	if got := out[`system.cpu.utilization{mode="idle"}`]; got != 0.25 {
		t.Errorf("idle = %v, want 0.25", got)
	}

	var sum float64
	for _, v := range out {
		sum += v.(float64)
	}

	if math.Abs(sum-1) > 1e-9 {
		t.Errorf("modes sum to %v, want 1", sum)
	}
}

func TestDiskSeriesUseMountpointLabel(t *testing.T) {
	out := map[string]any{}
	addDiskMetrics(out, []DiskStats{{MountPoint: "/var", Total: 10, Used: 4, Available: 6}})

	want := map[string]uint64{
		`system.filesystem.size_bytes{mountpoint="/var"}`:      10,
		`system.filesystem.used_bytes{mountpoint="/var"}`:      4,
		`system.filesystem.available_bytes{mountpoint="/var"}`: 6,
	}

	if len(out) != len(want) {
		t.Fatalf("got %v", out)
	}

	for k, v := range want {
		if out[k] != v {
			t.Errorf("%s = %v, want %d", k, out[k], v)
		}
	}
}

func TestMemorySeriesSkipUnmeasuredFields(t *testing.T) {
	out := map[string]any{}
	addMemoryMetrics(out, &MemoryStats{Total: 10, Available: 6, Used: 4, Free: 6})

	for k := range out {
		if strings.Contains(k, "swap") || strings.Contains(k, "buffers") || strings.Contains(k, "cached") {
			t.Errorf("reported %s without measuring it", k)
		}
	}

	if out["system.memory.size_bytes"] != uint64(10) {
		t.Errorf("size = %v, want 10", out["system.memory.size_bytes"])
	}
}
