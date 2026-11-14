//go:build !windows

package process

import "os/exec"

// setupJob is a no-op on non-Windows platforms: process-tree termination is
// handled by the Unix process-group (Setpgid + negative kill) primitives, so no
// Job Object is needed. It exists so the process manager compiles on every
// platform regardless of build tags.
func setupJob(cmd *exec.Cmd) (func(), error) {
	return func() {}, nil
}
