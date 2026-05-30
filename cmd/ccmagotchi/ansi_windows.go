//go:build windows

package main

// enableANSI: on Windows, kaomoji + ANSI need UTF-8 output and virtual-terminal
// processing. Modern Windows Terminal enables VT by default; legacy conhost may
// not. v1 is a no-op to keep the build zero-dependency.
//
// TODO (Phase 5 / Windows verify): enable VT + UTF-8 here. Either import
// golang.org/x/sys/windows (SetConsoleMode|ENABLE_VIRTUAL_TERMINAL_PROCESSING +
// SetConsoleOutputCP 65001) — accepting one dep on Windows only — or call the
// kernel32 procs via syscall. Confirm against real PowerShell before shipping.
func enableANSI() {}
