//go:build !windows

package mounts

import (
	"os"
	"syscall"
)

// deviceOf returns the filesystem device number backing path, so two
// mountpoints on the same filesystem (a bind mount, or the same block
// device mounted twice) compare equal.
func deviceOf(path string) (uint64, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return uint64(st.Dev), true // #nosec G115 -- Dev is a small device number, never negative
}
