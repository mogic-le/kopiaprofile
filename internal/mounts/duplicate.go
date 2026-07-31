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
	gopath "path"
	"sort"
	"strings"
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
// excludes are the profile's effective ignore patterns (see
// wrapper.EffectiveIgnorePatterns). A mountpoint that those patterns keep
// out of the backup is not reported: warning about a duplicate path whose
// contents are never read would be a false positive, and one that cannot be
// silenced by fixing the configuration.
func DetectDuplicates(mountsFile string, roots, excludes []string) ([]DuplicateGroup, error) {
	if mountsFile == "" {
		mountsFile = "/proc/mounts"
	}
	f, err := os.Open(mountsFile) // #nosec G304 -- caller-controlled, defaults to /proc/mounts
	if err != nil {
		return nil, fmt.Errorf("opening %q: %w", mountsFile, err)
	}
	defer func() { _ = f.Close() }()

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
		if isExcluded(mountpoint, excludes) {
			continue
		}
		dev, ok := deviceOf(mountpoint)
		if !ok {
			// Gone, not reachable from here, or (on a platform with no
			// concept of a device number, e.g. Windows) undetectable -
			// nothing to deduplicate against.
			continue
		}
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

// isExcluded reports whether an ignore pattern keeps mountpoint out of the
// backup. Deliberately conservative: only anchored patterns (starting with
// "/") are considered, matched against the mountpoint itself and against
// each of its ancestor directories, because excluding a directory excludes
// everything below it. Globs are matched with path.Match.
//
// Unanchored patterns are ignored here even though kopia's gitignore-style
// rules would match them at any depth. Reimplementing those semantics would
// duplicate kopia's matcher and could drift from it, and the two possible
// mistakes are not equally bad: failing to suppress a warning is noise,
// wrongly suppressing one hides a filesystem that really is being read
// twice. So this errs towards still warning.
func isExcluded(mountpoint string, excludes []string) bool {
	for _, pattern := range excludes {
		if !strings.HasPrefix(pattern, "/") {
			continue
		}
		pattern = strings.TrimSuffix(pattern, "/")
		if pattern == "" {
			continue
		}
		if pattern == mountpoint || strings.HasPrefix(mountpoint, pattern+"/") {
			return true
		}
		if !strings.ContainsAny(pattern, "*?[") {
			continue
		}
		// Glob: test the mountpoint and every ancestor, so that a pattern
		// like /var/lib/rancher/*/storage also covers paths below it.
		//
		// Deliberately the "path" package, not "path/filepath": mountpoints
		// come from /proc/mounts and are always slash-separated, while
		// filepath is separator-aware. On Windows filepath.Dir returns
		// backslashes, so a loop terminating on `!= "/"` never ends -
		// filepath.Dir(`\`) is `\` forever. That hung the whole package's
		// test binary for the full 10 minute timeout on windows-latest.
		// The parent == p guard makes termination independent of any
		// separator assumption.
		for p := mountpoint; ; {
			if ok, err := gopath.Match(pattern, p); err == nil && ok {
				return true
			}
			parent := gopath.Dir(p)
			if parent == p {
				break
			}
			p = parent
		}
	}
	return false
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
