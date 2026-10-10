//go:build !unix

package execx

import "os/exec"

// The default cancellation kills the direct child. WaitDelay bounds inherited
// pipe waiting on platforms without the Unix process-group contract.
func configureProcessGroup(_ *exec.Cmd) {}
