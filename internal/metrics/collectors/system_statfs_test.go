//go:build !windows

package collectors

import (
	"strings"
	"testing"
)

func TestCollectDiskStatsSkipsUnsafeAndMissingPaths(t *testing.T) {
	stats := collectDiskStats([]string{`/a"b`, "/no/such/mount/point", "/"})

	if len(stats) != 1 || stats[0].MountPoint != "/" {
		t.Errorf("got %+v, want only /", stats)
	}
}

// Every key the collector reports must belong to the documented set, and no
// key may carry a host detail in its name.
func TestCollectReportsOnlyDocumentedSeries(t *testing.T) {
	sc := NewSystemCollector().(*SystemCollector)
	sc.config.Interval = 0

	_ = sc.Collect()
	out := sc.Collect() // the second pass has a CPU delta to report

	allowed := []string{
		"system.cpu.utilization{mode=",
		"system.memory.size_bytes", "system.memory.available_bytes", "system.memory.used_bytes",
		"system.memory.free_bytes", "system.memory.buffers_bytes", "system.memory.cached_bytes",
		"system.swap.size_bytes", "system.swap.free_bytes", "system.swap.used_bytes",
		"system.load1", "system.load5", "system.load15",
		"system.filesystem.size_bytes{mountpoint=", "system.filesystem.used_bytes{mountpoint=",
		"system.filesystem.available_bytes{mountpoint=",
	}

	for key := range out {
		ok := false

		for _, a := range allowed {
			if key == a || (strings.HasSuffix(a, "=") && strings.HasPrefix(key, a)) {
				ok = true

				break
			}
		}

		if !ok {
			t.Errorf("undocumented series %q", key)
		}
	}

	if _, ok := out[`system.filesystem.size_bytes{mountpoint="/"}`]; !ok {
		t.Errorf("no filesystem series for /: %v", out)
	}
}
