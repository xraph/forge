//go:build !windows

package collectors_test

import (
	"net"
	"os"
	"syscall"
)

// platformErrorTypeCases checks that the Winsock numbers aren't treated as a
// client leaving anywhere but Windows, where they're unrelated errno values.
func platformErrorTypeCases() map[string]errorTypeCase {
	return map[string]errorTypeCase{
		"errno 10054": {
			&net.OpError{Op: "read", Net: "tcp", Err: os.NewSyscallError("read", syscall.Errno(10054))}, "handler_error",
		},
		"errno 10053": {
			&net.OpError{Op: "write", Net: "tcp", Err: os.NewSyscallError("write", syscall.Errno(10053))}, "handler_error",
		},
	}
}
