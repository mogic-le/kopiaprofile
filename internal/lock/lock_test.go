package lock

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireAndRelease(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lock")
	meta := metaPath(path)

	l, err := Acquire(Options{Path: path, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if l.Path() != path {
		t.Errorf("path: got %q want %q", l.Path(), path)
	}
	// The lock file itself should be a zero-byte flock target; the
	// PID/host/timestamp metadata lives in the sibling .meta file.
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected lock file at %q, got: %v", path, err)
	}
	if _, err := os.Stat(meta); err != nil {
		t.Errorf("expected metadata file at %q, got: %v", meta, err)
	}
	// Try to acquire again - should fail
	_, err = Acquire(Options{Path: path, Timeout: 100 * time.Millisecond})
	if !errors.Is(err, ErrLocked) {
		t.Errorf("expected ErrLocked, got %v", err)
	}
	// Release
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	// After release both the lock file and its metadata should be
	// gone (best-effort cleanup). The next acquisition should then
	// succeed without ErrLocked.
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected lock file removed after Release, got err=%v", err)
	}
	if _, err := os.Stat(meta); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected meta file removed after Release, got err=%v", err)
	}
	// Should be acquirable again
	l2, err := Acquire(Options{Path: path, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	// Release on the test goroutine instead of via defer so we can
	// fail the test if release fails. (Defer silently swallows
	// errors; that hid a flock-incompatibility regression during
	// the BSD-port work.)
	if err := l2.Release(); err != nil {
		t.Errorf("releasing l2: %v", err)
	}
}
