//go:build !windows

package collectors

// clientGoneErrnos holds platform errno values that mean the client went
// away. Outside Windows, EPIPE and ECONNRESET in isClientGone cover it.
var clientGoneErrnos []error
