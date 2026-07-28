//go:build unix || linux || darwin

package lock

import (
	"os"
	"syscall"
)

// pidAliveOS is the Unix implementation. Signal 0 is the conventional
// POSIX probe for "does this process exist" - sending it never
// affects the target process, it only reports delivery success/failure.
var pidAliveOS = func(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}
