// Package mounts detects when the same filesystem is reachable from a
// backup source through more than one path - the common pattern on
// providers that auto-mount every new volume under a fixed path: the
// volume then also gets mounted or bind-mounted wherever the application
// actually expects it, and the auto-mounted copy is never unmounted.
// Kopia's content-addressed storage would still deduplicate the bytes on
// the backend, but scanning and hashing the same terabytes twice per run
// wastes real time.
package mounts

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"
	"syscall"
)

// DuplicateGroup is a set of mountpoints that all resolve to the same
// underlying filesystem.
type DuplicateGroup struct {
	Paths []string
}

// DetectDuplicates scans mountsFile (pass "" for the live "/proc/mounts")
// for block-device-backed filesystems mounted at more than one path
// under any of roots. Only mounts whose source starts with "/dev/" are
// considered - this naturally excludes proc, sysfs, tmpfs, overlay,
// cgroup and network filesystems, which are not the mounted-twice
// pattern this package targets and would otherwise be noisy to report.
func DetectDuplicates(mountsFile string, roots []string) ([]DuplicateGroup, error) {
	if mountsFile == "" {
		mountsFile = "/proc/mounts"
	}
	f, err := os.Open(mountsFile) // #nosec G304 -- caller-controlled, defaults to /proc/mounts
	if err != nil {
		return nil, fmt.Errorf("opening %q: %w", mountsFile, err)
	}
	defer f.Close()

	byDev := make(map[uint64][]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		source, mountpoint := fields[0], unescapeMount(fields[1])
		if !strings.HasPrefix(source, "/dev/") {
			continue
		}
		if !underAnyRoot(mountpoint, roots) {
			continue
		}
		info, statErr := os.Stat(mountpoint)
		if statErr != nil {
			// Gone, or not reachable from here (e.g. a stale entry) -
			// nothing to deduplicate against.
			continue
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			continue
		}
		dev := uint64(st.Dev) // #nosec G115 -- st.Dev is already unsigned on Linux
		byDev[dev] = append(byDev[dev], mountpoint)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading %q: %w", mountsFile, err)
	}

	var groups []DuplicateGroup
	for _, paths := range byDev {
		if len(paths) < 2 {
			continue
		}
		sort.Strings(paths)
		groups = append(groups, DuplicateGroup{Paths: paths})
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Paths[0] < groups[j].Paths[0] })
	return groups, nil
}

// underAnyRoot reports whether mountpoint is one of roots or a
// descendant of one of them. An empty roots list matches everything
// (the common case: backup.sources is just "/").
func underAnyRoot(mountpoint string, roots []string) bool {
	if len(roots) == 0 {
		return true
	}
	for _, root := range roots {
		root = strings.TrimSuffix(root, "/")
		if root == "" || root == "/" {
			return true
		}
		if mountpoint == root || strings.HasPrefix(mountpoint, root+"/") {
			return true
		}
	}
	return false
}

// unescapeMount reverses the octal escaping /proc/mounts uses for
// spaces, tabs, newlines and backslashes in paths.
func unescapeMount(s string) string {
	replacer := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	return replacer.Replace(s)
}
