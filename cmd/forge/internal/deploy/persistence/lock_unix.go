//go:build unix

package persistence

import (
	"golang.org/x/sys/unix"
	"os"
)

func authorityLock(file *os.File, exclusive bool) (func(), error) {
	mode := unix.LOCK_SH
	if exclusive {
		mode = unix.LOCK_EX
	}

	if err := unix.Flock(int(file.Fd()), mode|unix.LOCK_NB); err != nil {
		_ = file.Close()

		if err == unix.EWOULDBLOCK {
			return nil, ErrLocked
		}

		return nil, err
	}

	return func() { _ = unix.Flock(int(file.Fd()), unix.LOCK_UN); _ = file.Close() }, nil
}
