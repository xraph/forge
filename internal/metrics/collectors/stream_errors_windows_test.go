//go:build windows

package collectors_test

import (
	"net"
	"os"
	"syscall"
)

// platformErrorTypeCases holds the Winsock codes a dropped client surfaces as
// on Windows, wrapped the way a failed read or write wraps them.
func platformErrorTypeCases() map[string]errorTypeCase {
	return map[string]errorTypeCase{
		"wsa conn reset": {
			&net.OpError{Op: "read", Net: "tcp", Err: os.NewSyscallError("wsarecv", syscall.WSAECONNRESET)}, "",
		},
		"wsa conn aborted": {
			&net.OpError{Op: "write", Net: "tcp", Err: os.NewSyscallError("wsasend", syscall.WSAECONNABORTED)}, "",
		},
	}
}
