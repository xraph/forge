//go:build unix

package state

import (
	"golang.org/x/sys/unix"
	"os"
)

func fileLock(f *os.File) (func(), error) {
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()

		if err == unix.EWOULDBLOCK {
			return nil, ErrLocked
		}

		return nil, err
	}

	return func() { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN); _ = f.Close() }, nil
}
