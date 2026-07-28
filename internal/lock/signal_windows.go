//go:build windows

package lock

import "os"

// pidAliveOS is the Windows implementation. Windows has no equivalent
// of POSIX signal 0: os.Process.Signal only implements os.Interrupt
// and os.Kill, and os.Kill actually terminates the process - NOT a
// safe existence probe. An earlier version of this file used
// proc.Signal(os.Kill) here, which meant checking whether a PID was
// still alive would, on success, kill it - found live via `go test
// -race` on windows-latest: a test that called this against its own
// (very much alive) PID terminated its own test binary mid-run with
// no further output. FindProcess alone is the safe option: on Windows
// it actually opens a handle to the process (unlike Unix, where
// FindProcess always trivially succeeds and the real check is the
// signal send) and fails if the PID doesn't exist, without touching
// the process in any way otherwise.
var pidAliveOS = func(pid int) bool {
	_, err := os.FindProcess(pid)
	return err == nil
}
