package checks

import (
	"errors"
	"syscall"
	"testing"
)

func TestDiskUsageBytesNegativeBlockSize(t *testing.T) {
	total, free, err := diskUsageBytes(&syscall.Statfs_t{Bsize: -1, Blocks: 10})
	if !errors.Is(err, errInvalidDiskStats) {
		t.Fatalf("error = %v, want invalid disk stats", err)
	}

	if total != 0 || free != 0 {
		t.Errorf("invalid stats returned byte counts: total=%d free=%d", total, free)
	}
}
