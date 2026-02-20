//go:build !forge_debug

package forge

// initDebugServer is a no-op in release builds.
// Compiled with -tags forge_debug to get the real implementation.
func initDebugServer(_ *app) {}
