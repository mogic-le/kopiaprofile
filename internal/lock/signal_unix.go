//go:build unix || linux || darwin

package lock

import (
	"os"
	"syscall"
)

// testSignal returns a signal value that is safe to use for
// process-existence probes. Signal 0 is the conventional choice on
// POSIX systems.
func testSignal() os.Signal { return syscall.Signal(0) }
