//go:build !windows

package collectors

import (
	"math"
	"syscall"
	"testing"
)

func TestDiskStatsFromStat(t *testing.T) {
	tests := []struct {
		name          string
		stat          syscall.Statfs_t
		wantTotal     uint64
		wantAvailable uint64
		wantError     bool
	}{
		{
			name:          "normal usage",
			stat:          syscall.Statfs_t{Bsize: 4096, Blocks: 10, Bavail: 4},
			wantTotal:     40960,
			wantAvailable: 16384,
		},
		{
			name:      "full disk",
			stat:      syscall.Statfs_t{Bsize: 4096, Blocks: 10},
			wantTotal: 40960,
		},
		{
			name:          "all blocks available",
			stat:          syscall.Statfs_t{Bsize: 4096, Blocks: 10, Bavail: 10},
			wantTotal:     40960,
			wantAvailable: 40960,
		},
		{
			name:          "largest safe block count",
			stat:          syscall.Statfs_t{Bsize: 4096, Blocks: math.MaxUint64 / 4096, Bavail: math.MaxUint64 / 4096},
			wantTotal:     math.MaxUint64 - 4095,
			wantAvailable: math.MaxUint64 - 4095,
		},
		{
			name:          "maximum byte count",
			stat:          syscall.Statfs_t{Bsize: 1, Blocks: math.MaxUint64, Bavail: math.MaxUint64},
			wantTotal:     math.MaxUint64,
			wantAvailable: math.MaxUint64,
		},
		{
			name:      "zero block size",
			stat:      syscall.Statfs_t{Blocks: 10},
			wantError: true,
		},
		{
			name:      "zero total blocks",
			stat:      syscall.Statfs_t{Bsize: 4096},
			wantError: true,
		},
		{
			name:      "available exceeds total",
			stat:      syscall.Statfs_t{Bsize: 4096, Blocks: 10, Bavail: 11},
			wantError: true,
		},
		{
			name:      "byte count overflow",
			stat:      syscall.Statfs_t{Bsize: 4096, Blocks: math.MaxUint64/4096 + 1},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats, err := diskStatsFromStat("test", &tt.stat)
			if tt.wantError {
				if err == nil || stats != nil {
					t.Fatalf("got stats=%+v error=%v, want no stats and an error", stats, err)
				}

				return
			}

			if err != nil {
				t.Fatalf("diskStatsFromStat: %v", err)
			}

			if stats.Total != tt.wantTotal || stats.Available != tt.wantAvailable || stats.Used != tt.wantTotal-tt.wantAvailable {
				t.Errorf("unexpected byte counts: %+v", stats)
			}

			if stats.MountPoint != "test" {
				t.Errorf("mount point = %q, want test", stats.MountPoint)
			}
		})
	}
}

func TestReadDiskStats(t *testing.T) {
	stats, err := readDiskStats(t.TempDir())
	if err != nil {
		t.Fatalf("readDiskStats: %v", err)
	}

	if stats.Total == 0 || stats.Available > stats.Total || stats.Used != stats.Total-stats.Available {
		t.Errorf("invalid disk byte counts: %+v", stats)
	}
}
