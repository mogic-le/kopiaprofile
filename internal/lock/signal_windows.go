//go:build windows

package lock

import "os"

// testSignal on Windows is a no-op. Process existence is approximated
// by os.FindProcess always succeeding; in practice kopiaprofile is
// rarely run on Windows and the MVP is supported primarily on Unix.
func testSignal() os.Signal { return os.Kill }
