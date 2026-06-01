//go:build windows

package daemon

import (
	"os/exec"
	"syscall"
)

// processAlive reports whether pid names a live process. On Windows,
// (*os.Process).Signal(0) returns "not supported by windows" even for live
// processes, so we query the OS directly: open the process and ask whether its
// wait handle is still unsignaled (WAIT_TIMEOUT = still running).
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	const synchronize = 0x00100000  // SYNCHRONIZE — required to wait on the handle
	const waitTimeout = 0x00000102 // STATUS_TIMEOUT — handle not signaled → alive
	h, err := syscall.OpenProcess(synchronize, false, uint32(pid))
	if err != nil {
		return false // no such process (or no rights) → treat as dead
	}
	defer syscall.CloseHandle(h)
	ev, err := syscall.WaitForSingleObject(h, 0)
	if err != nil {
		return false
	}
	return ev == waitTimeout
}

const detachedProcess = 0x00000008 // DETACHED_PROCESS

func spawnDetached(exe string, args ...string) error {
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detachedProcess}
	return cmd.Start()
}
