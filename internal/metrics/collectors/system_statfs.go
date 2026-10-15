//go:build !windows

package collectors

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"syscall"
)

func defaultMountPoints() []string {
	return []string{"/", "/home", "/var", "/tmp"}
}

// isMountPoint reports whether path is the root of a filesystem: either / or
// a directory on a different device from its parent.
func isMountPoint(path string) bool {
	path = filepath.Clean(path)
	if path == "/" {
		return true
	}

	var self, parent syscall.Stat_t
	if syscall.Stat(path, &self) != nil || syscall.Stat(filepath.Dir(path), &parent) != nil {
		return false
	}

	return self.Dev != parent.Dev
}

// readDiskStats collects filesystem usage for path.
func readDiskStats(path string) (*DiskStats, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return nil, fmt.Errorf("failed to get disk stats for %s: %w", path, err)
	}

	stats, err := diskStatsFromStat(path, &stat)
	if err != nil {
		return nil, fmt.Errorf("failed to get disk stats for %s: %w", path, err)
	}

	return stats, nil
}

// diskStatsFromStat validates filesystem counters before calculating disk usage.
func diskStatsFromStat(path string, stat *syscall.Statfs_t) (*DiskStats, error) {
	size := stat.Bsize
	if size <= 0 {
		return nil, errors.New("invalid disk stats: block size must be positive")
	}

	if stat.Blocks == 0 {
		return nil, errors.New("invalid disk stats: total blocks must be positive")
	}

	if stat.Bavail > stat.Blocks {
		return nil, errors.New("invalid disk stats: available blocks exceed total blocks")
	}

	blockSize := uint64(size)
	if stat.Blocks > math.MaxUint64/blockSize {
		return nil, errors.New("invalid disk stats: disk byte count overflows uint64")
	}

	total := stat.Blocks * blockSize
	available := stat.Bavail * blockSize

	return &DiskStats{
		MountPoint: path,
		Total:      total,
		Used:       total - available,
		Available:  available,
	}, nil
}
