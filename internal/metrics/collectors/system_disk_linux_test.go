package collectors

import (
	"syscall"
	"testing"
)

func TestDiskStatsFromStatNegativeBlockSize(t *testing.T) {
	stats, err := diskStatsFromStat("test", &syscall.Statfs_t{Bsize: -1, Blocks: 10})
	if err == nil || stats != nil {
		t.Fatalf("got stats=%+v error=%v, want no stats and an error", stats, err)
	}
}
