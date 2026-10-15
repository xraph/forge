//go:build windows

package collectors

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

var (
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	getDiskFreeSpaceExProc   = kernel32.NewProc("GetDiskFreeSpaceExW")
	globalMemoryStatusExProc = kernel32.NewProc("GlobalMemoryStatusEx")
)

// memoryStatusEx is the Win32 MEMORYSTATUSEX structure.
type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

func defaultMountPoints() []string {
	return []string{`C:\`}
}

// isMountPoint treats every configured path as a volume root; readDiskStats
// fails for a path that is not on one.
func isMountPoint(string) bool { return true }

// CPU time and load have no Windows implementation yet.
func readCPUStats() (*CPUStats, error) { return nil, errUnsupported }

func readLoadStats() (*LoadStats, error) { return nil, errUnsupported }

// readMemoryStats reports physical memory. The page file is a commit limit,
// not swap, so no swap series is reported.
func readMemoryStats() (*MemoryStats, error) {
	status := memoryStatusEx{}
	status.Length = uint32(unsafe.Sizeof(status))

	ret, _, err := globalMemoryStatusExProc.Call(uintptr(unsafe.Pointer(&status)))
	if ret == 0 {
		return nil, fmt.Errorf("GlobalMemoryStatusEx: %w", err)
	}

	if status.AvailPhys > status.TotalPhys {
		return nil, errors.New("GlobalMemoryStatusEx: available memory exceeds total")
	}

	return &MemoryStats{
		Total:     status.TotalPhys,
		Available: status.AvailPhys,
		Used:      status.TotalPhys - status.AvailPhys,
		Free:      status.AvailPhys,
	}, nil
}

// readDiskStats reports usage for the volume at path. Available is what the
// calling user may still write, as statfs's f_bavail is on Unix.
func readDiskStats(path string) (*DiskStats, error) {
	pathW, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("failed to convert %s to UTF-16: %w", path, err)
	}

	var availToCaller, total, totalFree uint64

	ret, _, callErr := getDiskFreeSpaceExProc.Call(
		uintptr(unsafe.Pointer(pathW)),
		uintptr(unsafe.Pointer(&availToCaller)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if ret == 0 {
		return nil, fmt.Errorf("GetDiskFreeSpaceExW %s: %w", path, callErr)
	}

	if total == 0 || availToCaller > total {
		return nil, fmt.Errorf("invalid disk stats for %s", path)
	}

	return &DiskStats{
		MountPoint: path,
		Total:      total,
		Used:       total - availToCaller,
		Available:  availToCaller,
	}, nil
}
