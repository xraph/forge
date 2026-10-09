//go:build !unix && !windows

package state

import (
	"errors"
	"os"
)

func fileLock(f *os.File) (func(), error) {
	f.Close()
	return nil, errors.New("deployment locking unavailable on this operating system")
}
