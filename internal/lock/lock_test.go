package lock

import (
	"errors"
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
	// Try to acquire again - should fail
	_, err = Acquire(Options{Path: path, Timeout: 100 * time.Millisecond})
	if !errors.Is(err, ErrLocked) {
		t.Errorf("expected ErrLocked, got %v", err)
	}
	// Release
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	// Should be acquirable again
	l2, err := Acquire(Options{Path: path, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer l2.Release()
}
