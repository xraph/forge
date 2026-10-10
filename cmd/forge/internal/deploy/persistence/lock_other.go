//go:build !unix && !windows

package persistence

import (
	"errors"
	"os"
)

func authorityLock(file *os.File, exclusive bool) (func(), error) {
	_ = file.Close()
	return nil, errors.New("deployment authority locking unsupported on this platform")
}
