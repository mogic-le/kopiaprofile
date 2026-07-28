package lock

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadInfoMissingFile(t *testing.T) {
	dir := t.TempDir()
	_, err := ReadInfo(filepath.Join(dir, "nope.lock"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected os.ErrNotExist, got %v", err)
	}
}

func TestReadInfoParsesAcquiredLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lock")
	l, err := Acquire(Options{Path: path, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Release() })

	info, err := ReadInfo(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.PID != os.Getpid() {
		t.Errorf("PID: got %d want %d", info.PID, os.Getpid())
	}
	if info.Host == "" {
		t.Error("expected Host to be set")
	}
	if info.At.IsZero() {
		t.Error("expected At to be set")
	}
	if time.Since(info.At) > time.Minute {
		t.Errorf("At looks wrong: %v", info.At)
	}
}

func TestIsRunningNoLockFile(t *testing.T) {
	dir := t.TempDir()
	running, info, err := IsRunning(filepath.Join(dir, "nope.lock"))
	if err != nil {
		t.Fatalf("expected no error for a missing lock file, got %v", err)
	}
	if running {
		t.Error("expected running=false for a missing lock file")
	}
	if info != nil {
		t.Errorf("expected nil info for a missing lock file, got %+v", info)
	}
}

func TestIsRunningLiveProcess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lock")
	l, err := Acquire(Options{Path: path, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Release() })

	// Our own PID is always alive for the duration of the test.
	running, info, err := IsRunning(path)
	if err != nil {
		t.Fatal(err)
	}
	if !running {
		t.Error("expected running=true for a lock held by this (live) process")
	}
	if info == nil || info.PID != os.Getpid() {
		t.Errorf("expected info.PID=%d, got %+v", os.Getpid(), info)
	}
}

func TestIsRunningStalePID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.lock")
	// Write a lock file by hand recording a PID that pidAlive will
	// report as dead, without going through Acquire (which would
	// record our own, genuinely-live PID).
	body := "pid=999999\nhost=test-host\nat=" + time.Now().UTC().Format(time.RFC3339) + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	orig := pidAlive
	pidAlive = func(int) bool { return false }
	t.Cleanup(func() { pidAlive = orig })

	running, info, err := IsRunning(path)
	if err != nil {
		t.Fatal(err)
	}
	if running {
		t.Error("expected running=false for a stale PID")
	}
	if info == nil || info.PID != 999999 || info.Host != "test-host" {
		t.Errorf("expected parsed info despite stale PID, got %+v", info)
	}
}
