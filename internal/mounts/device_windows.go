//go:build windows

package mounts

// deviceOf always reports "unknown" on Windows: the mounted-twice
// pattern this package targets is specific to /proc/mounts-style Unix
// hosts, and Windows has no equivalent os.FileInfo.Sys() device number
// to compare mountpoints by.
func deviceOf(_ string) (uint64, bool) {
	return 0, false
}
