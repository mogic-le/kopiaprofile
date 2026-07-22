package monitor

import (
	"os"
	"path/filepath"
	"testing"
)

// The status file must be world-readable (0644) so a non-root monitoring
// user (e.g. Icinga) can read it, matching resticprofile's own status
// file. os.CreateTemp defaults to 0600 and a rename doesn't change that,
// so writeStatusFile must explicitly chmod before renaming - regression
// test for that fix.
func TestWriteStatusFilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backup-status.json")

	if err := writeStatusFile(path, Status{Profile: "test"}); err != nil {
		t.Fatalf("writeStatusFile: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Errorf("status file mode = %o, want 0644", perm)
	}
}
