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

	l, err := Acquire(Options{Path: path, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if l.Path() != path {
		t.Errorf("path: got %q want %q", l.Path(), path)
	}
	// The lock file is a single file (not a lock + .meta pair) that
	// contains the PID/host/timestamp metadata, so it should be
	// non-empty and parseable.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("expected lock file at %q, got: %v", path, err)
	}
	if len(data) == 0 {
		t.Errorf("expected lock file %q to contain metadata, got empty", path)
	}
	if !contains(string(data), "pid=") {
		t.Errorf("expected lock file %q to contain a pid= line, got: %q", path, string(data))
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

	// After release the file should be gone (best-effort cleanup).
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected lock file removed after Release, got err=%v", err)
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

func TestReleaseIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lock")
	l, err := Acquire(Options{Path: path, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	// A second Release must be a no-op (not a use-after-close
	// panic, not an error). This matters for code paths that call
	// Release from a defer AND from a runHook handler.
	if err := l.Release(); err != nil {
		t.Errorf("second Release returned error: %v", err)
	}
}

func TestAcquireRejectsWhenLocked(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lock")
	l, err := Acquire(Options{Path: path, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Release() })

	// RetryAfter > 0 + short Timeout should give us ErrLocked
	// after exhausting the timeout, not just on first attempt.
	start := time.Now()
	_, err = Acquire(Options{Path: path, RetryAfter: 20 * time.Millisecond, Timeout: 60 * time.Millisecond})
	elapsed := time.Since(start)
	if !errors.Is(err, ErrLocked) {
		t.Errorf("expected ErrLocked, got %v", err)
	}
	if elapsed < 50*time.Millisecond {
		t.Errorf("expected to wait ~60ms before giving up, took %v", elapsed)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("expected to give up within ~60ms, took %v", elapsed)
	}
}

// contains is a tiny helper to keep this test free of the strings
// import (the package only uses strings for the metadata parser).
func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
