package mounts

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
	if runtime.GOOS == "windows" {
		t.Skip("deviceOf has no device-number equivalent on Windows")
	}
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

	groups, err := DetectDuplicates(mountsFile, nil, nil)
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

	groups, err := DetectDuplicates(mountsFile, nil, nil)
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

	groups, err := DetectDuplicates(mountsFile, nil, nil)
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

	groups, err := DetectDuplicates(mountsFile, []string{filepath.Join(base, "data")}, nil)
	if err != nil {
		t.Fatalf("DetectDuplicates: %v", err)
	}
	if len(groups) != 0 {
		t.Errorf("expected no duplicate reported when only one copy is in scope, got %+v", groups)
	}
}

// Ein Mountpoint, den die Ignore-Muster des Profils ausschliessen, wird
// nicht gemeldet. Sonst warnt kopiaprofile ueber eine Doppelung, deren
// Inhalt es nie liest - eine Warnung, die sich durch keine Konfiguration
// abstellen laesst. Genau so gesehen auf srv-mog-prod-kubernetes-master-01
// am 2026-07-31: /var/lib/kubelet und /mnt/HC_Volume_102132025 waren
// ausgeschlossen und wurden weiter gemeldet.
func TestDetectDuplicatesSkipsExcludedMountpoints(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "data")
	auto := filepath.Join(base, "mnt", "HC_Volume_1")
	for _, d := range []string{real, auto} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mountsFile := writeMountsFile(t, [][2]string{
		{"/dev/sdb", real},
		{"/dev/sdb", auto},
	})

	// Ohne Excludes: beide Pfade, also eine Meldung.
	groups, err := DetectDuplicates(mountsFile, nil, nil)
	if err != nil {
		t.Fatalf("DetectDuplicates: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("ohne Excludes wird eine Doppelung erwartet, got %+v", groups)
	}

	// Mit Exclude auf die automatisch gemountete Kopie: keine Meldung.
	groups, err = DetectDuplicates(mountsFile, nil, []string{auto})
	if err != nil {
		t.Fatalf("DetectDuplicates: %v", err)
	}
	if len(groups) != 0 {
		t.Errorf("ausgeschlossener Mountpoint darf nicht gemeldet werden, got %+v", groups)
	}
}

func TestIsExcluded(t *testing.T) {
	cases := []struct {
		name       string
		mountpoint string
		patterns   []string
		want       bool
	}{
		{"exakt", "/mnt/HC_Volume_102132025", []string{"/mnt/HC_Volume_102132025"}, true},
		{"Vorfahre", "/var/lib/kubelet/pods/abc/volumes/x", []string{"/var/lib/kubelet"}, true},
		{"Vorfahre mit Schraegstrich", "/var/lib/kubelet/pods/abc", []string{"/var/lib/kubelet/"}, true},
		{"Glob auf den Pfad", "/mnt/HC_Volume_9", []string{"/mnt/HC_Volume_*"}, true},
		{"Glob auf einen Vorfahren", "/var/lib/rancher/k3s/storage/pvc-1_db/sub", []string{"/var/lib/rancher/k3s/storage/*_db"}, true},
		{"kein Treffer", "/data", []string{"/mnt/HC_Volume_1", "/var/lib/kubelet"}, false},
		// Praefix-Vergleich darf nicht auf Namensteilen greifen.
		{"Namensteil ist kein Vorfahre", "/var/lib/kubelet-extra", []string{"/var/lib/kubelet"}, false},
		// Unanchored bleibt bewusst unberuecksichtigt, siehe isExcluded.
		{"unanchored wird ignoriert", "/var/cache", []string{"cache"}, false},
		{"leeres Muster", "/data", []string{"", "/"}, false},
	}
	for _, c := range cases {
		if got := isExcluded(c.mountpoint, c.patterns); got != c.want {
			t.Errorf("%s: isExcluded(%q, %v) = %v, want %v", c.name, c.mountpoint, c.patterns, got, c.want)
		}
	}
}

func TestUnescapeMount(t *testing.T) {
	got := unescapeMount(`/mnt/my\040volume`)
	if got != "/mnt/my volume" {
		t.Errorf("got %q", got)
	}
}
