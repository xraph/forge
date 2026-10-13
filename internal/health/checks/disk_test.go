//go:build !windows

package checks

import (
	"errors"
	"math"
	"strings"
	"syscall"
	"testing"

	health "github.com/xraph/forge/internal/health/internal"
)

func TestDiskUsageBytes(t *testing.T) {
	tests := []struct {
		name      string
		stat      syscall.Statfs_t
		wantTotal uint64
		wantFree  uint64
		wantError bool
	}{
		{
			name:      "normal usage",
			stat:      syscall.Statfs_t{Bsize: 4096, Blocks: 10, Bfree: 4},
			wantTotal: 40960,
			wantFree:  16384,
		},
		{
			name:      "full disk",
			stat:      syscall.Statfs_t{Bsize: 4096, Blocks: 10},
			wantTotal: 40960,
		},
		{
			name:      "all blocks free",
			stat:      syscall.Statfs_t{Bsize: 4096, Blocks: 10, Bfree: 10},
			wantTotal: 40960,
			wantFree:  40960,
		},
		{
			name:      "largest safe block count",
			stat:      syscall.Statfs_t{Bsize: 4096, Blocks: math.MaxUint64 / 4096, Bfree: math.MaxUint64 / 4096},
			wantTotal: math.MaxUint64 - 4095,
			wantFree:  math.MaxUint64 - 4095,
		},
		{
			name:      "maximum byte count",
			stat:      syscall.Statfs_t{Bsize: 1, Blocks: math.MaxUint64, Bfree: math.MaxUint64},
			wantTotal: math.MaxUint64,
			wantFree:  math.MaxUint64,
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
			name:      "free exceeds total",
			stat:      syscall.Statfs_t{Bsize: 4096, Blocks: 10, Bfree: 11},
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
			total, free, err := diskUsageBytes(&tt.stat)
			if tt.wantError {
				if !errors.Is(err, errInvalidDiskStats) {
					t.Fatalf("error = %v, want invalid disk stats", err)
				}

				if total != 0 || free != 0 {
					t.Fatalf("invalid stats returned byte counts: total=%d free=%d", total, free)
				}

				return
			}

			if err != nil {
				t.Fatalf("diskUsageBytes: %v", err)
			}

			if total != tt.wantTotal || free != tt.wantFree {
				t.Errorf("bytes = (%d, %d), want (%d, %d)", total, free, tt.wantTotal, tt.wantFree)
			}
		})
	}
}

func TestDiskHealthCheckInvalidStats(t *testing.T) {
	tests := []struct {
		name      string
		paths     []string
		usage     float64
		wantError bool
	}{
		{name: "only invalid path", paths: []string{"invalid"}, wantError: true},
		{name: "invalid and healthy paths", paths: []string{"invalid", "valid"}, usage: 10, wantError: true},
		{name: "invalid and warning paths", paths: []string{"invalid", "valid"}, usage: 85, wantError: true},
		{name: "unavailable path", paths: []string{"unavailable"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check := NewDiskHealthCheck(&DiskHealthCheckConfig{Paths: tt.paths})

			result := check.checkPaths(t.Context(), func(path string) (map[string]any, error) {
				switch path {
				case "invalid":
					_, _, err := diskUsageBytes(&syscall.Statfs_t{Blocks: 10})

					return nil, err
				case "unavailable":
					return nil, errors.New("path unavailable")
				default:
					return map[string]any{"usage_percent": tt.usage}, nil
				}
			})
			if tt.wantError {
				if result.Status != health.HealthStatusUnhealthy {
					t.Errorf("status = %s, want unhealthy", result.Status)
				}

				if !strings.Contains(result.Error, errInvalidDiskStats.Error()) {
					t.Errorf("error = %q, want invalid disk stats", result.Error)
				}

				if !strings.Contains(result.Message, errInvalidDiskStats.Error()) {
					t.Errorf("message = %q, want invalid disk stats", result.Message)
				}

				if _, ok := result.Details["path_error_invalid"]; !ok {
					t.Error("missing path error detail")
				}
			} else if result.Status != health.HealthStatusHealthy || result.Error != "" {
				t.Errorf("unavailable path changed health semantics: status=%s error=%q", result.Status, result.Error)
			}
		})
	}
}

func TestDiskHealthCheckPath(t *testing.T) {
	check := NewDiskHealthCheck(nil)

	info, err := check.checkPath(t.TempDir())
	if err != nil {
		t.Fatalf("checkPath: %v", err)
	}

	total, ok := info["total_bytes"].(uint64)
	if !ok || total == 0 {
		t.Fatalf("invalid total bytes: %v", info["total_bytes"])
	}

	free, ok := info["free_bytes"].(uint64)
	if !ok || free > total {
		t.Fatalf("invalid free bytes: %v", info["free_bytes"])
	}

	if used := info["used_bytes"]; used != total-free {
		t.Errorf("used bytes = %v, want %d", used, total-free)
	}
}
