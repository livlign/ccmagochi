//go:build windows

package daemon

import (
	"os"
	"os/exec"
	"syscall"
)

// processAlive: best-effort on Windows — if FindProcess + Signal(0) errors, treat dead.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

const detachedProcess = 0x00000008 // DETACHED_PROCESS

func spawnDetached(exe string, args ...string) error {
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detachedProcess}
	return cmd.Start()
}
