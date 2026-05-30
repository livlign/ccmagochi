//go:build !windows

package daemon

import (
	"os/exec"
	"syscall"
)

// processAlive reports whether pid is a live process (signal 0 probe).
func processAlive(pid int) bool {
	return pid > 0 && syscall.Kill(pid, 0) == nil
}

// spawnDetached starts the daemon in its own session so it outlives the renderer.
func spawnDetached(exe string, args ...string) error {
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}
