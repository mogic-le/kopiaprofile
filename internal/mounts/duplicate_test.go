package mounts

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// writeMountsFile writes a synthetic /proc/mounts-style file listing the
// given (source, mountpoint) pairs as /dev/-backed entries.
func writeMountsFile(t *testing.T, entries [][2]string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "mounts")
	var content string
	for _, e := range entries {
		content += fmt.Sprintf("%s %s ext4 rw,relatime 0 0\n", e[0], e[1])
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDetectDuplicatesSameFilesystemTwoMountpoints(t *testing.T) {
	// a and b live in the same t.TempDir() and therefore share the same
	// underlying filesystem (st_dev) - exactly what a bind mount or a
	// second independent mount of the same block device looks like.
	base := t.TempDir()
	a := filepath.Join(base, "mnt-copy")
	b := filepath.Join(base, "opt-copy")
	if err := os.Mkdir(a, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(b, 0o755); err != nil {
		t.Fatal(err)
	}

	mountsFile := writeMountsFile(t, [][2]string{
		{"/dev/sdb", a},
		{"/dev/sdb", b},
	})

	groups, err := DetectDuplicates(mountsFile, nil)
	if err != nil {
		t.Fatalf("DetectDuplicates: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 duplicate group, got %d: %+v", len(groups), groups)
	}
	if len(groups[0].Paths) != 2 {
		t.Errorf("expected 2 paths in the group, got %+v", groups[0].Paths)
	}
}

func TestDetectDuplicatesSingleMountIsNotDuplicate(t *testing.T) {
	base := t.TempDir()
	mountsFile := writeMountsFile(t, [][2]string{
		{"/dev/sdb", base},
	})

	groups, err := DetectDuplicates(mountsFile, nil)
	if err != nil {
		t.Fatalf("DetectDuplicates: %v", err)
	}
	if len(groups) != 0 {
		t.Errorf("expected no duplicate groups, got %+v", groups)
	}
}

func TestDetectDuplicatesSkipsNonDeviceSources(t *testing.T) {
	base := t.TempDir()
	a := filepath.Join(base, "a")
	b := filepath.Join(base, "b")
	if err := os.Mkdir(a, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(b, 0o755); err != nil {
		t.Fatal(err)
	}
	// Same underlying filesystem, but neither source is a real block
	// device (proc/tmpfs/overlay-style entries) - must not be reported.
	mountsFile := writeMountsFile(t, [][2]string{
		{"tmpfs", a},
		{"overlay", b},
	})

	groups, err := DetectDuplicates(mountsFile, nil)
	if err != nil {
		t.Fatalf("DetectDuplicates: %v", err)
	}
	if len(groups) != 0 {
		t.Errorf("expected non-/dev/ sources to be skipped, got %+v", groups)
	}
}

func TestDetectDuplicatesRootsFilter(t *testing.T) {
	base := t.TempDir()
	inside := filepath.Join(base, "data", "mnt-copy")
	outside := filepath.Join(base, "elsewhere", "opt-copy")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	// A third mountpoint of the same filesystem, but outside the given
	// root - a duplicate exists on disk, but only one copy falls inside
	// the profile's backup scope, so it must not be reported.
	mountsFile := writeMountsFile(t, [][2]string{
		{"/dev/sdb", inside},
		{"/dev/sdb", outside},
	})

	groups, err := DetectDuplicates(mountsFile, []string{filepath.Join(base, "data")})
	if err != nil {
		t.Fatalf("DetectDuplicates: %v", err)
	}
	if len(groups) != 0 {
		t.Errorf("expected no duplicate reported when only one copy is in scope, got %+v", groups)
	}
}

func TestUnescapeMount(t *testing.T) {
	got := unescapeMount(`/mnt/my\040volume`)
	if got != "/mnt/my volume" {
		t.Errorf("got %q", got)
	}
}
