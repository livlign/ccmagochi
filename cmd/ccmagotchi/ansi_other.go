//go:build !windows

package main

// enableANSI is a no-op on macOS/Linux — terminals handle UTF-8 + ANSI natively.
func enableANSI() {}
