//go:build windows

package collectors

import "syscall"

// clientGoneErrnos are the Winsock codes a dropped client surfaces as. They
// don't match syscall.ECONNRESET on Windows, which is a made-up errno there.
var clientGoneErrnos = []error{syscall.WSAECONNRESET, syscall.WSAECONNABORTED}
