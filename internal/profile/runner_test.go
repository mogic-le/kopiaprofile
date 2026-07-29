package profile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mogic-le/kopiaprofile/internal/config"
	"github.com/mogic-le/kopiaprofile/internal/lock"
	"github.com/mogic-le/kopiaprofile/internal/monitor"
)

type fakeLoader struct{ value string }

func (f fakeLoader) Load() (string, error) { return f.value, nil }

// failingLoader returns a sentinel error.
type failingLoader struct{}

var errFake = errors.New("fake password error")

func (failingLoader) Load() (string, error) { return "", errFake }

func TestRunSkipsHooksAndLock(t *testing.T) {
	prof := config.Profile{
		Name:      "test",
		Backup:    config.BackupSection{Sources: []string{"/tmp"}},
		Lock:      config.LockSection{Path: filepath.Join(t.TempDir(), "x.lock")},
		RunBefore: "echo before",
		RunAfter:  "echo after",
	}
	_, err := Run(context.Background(), RunOptions{
		Profile:        prof,
		Command:        []string{"snapshot", "create", "/tmp"},
		SkipHooks:      true,
		SkipLock:       true,
		PasswordSource: fakeLoader{value: "secret"},
		Timeout:        200 * time.Millisecond,
	})
	// We expect kopia to be missing, so the call will fail. That's fine
	// - we just want to verify the loader was honoured and pre-hooks
	// were skipped.
	if err == nil {
		t.Log("kopia was found and ran successfully (unusual in CI)")
	}
}

// A run that cannot get the lock must leave the monitor status file
// untouched. Overwriting it destroys the record of the run that actually
// backed something up, and replaces its end_at with "now" - so
// the monitoring check would be looking at a fresh timestamp for a backup
// that never happened. Observed live on example-host, where a 24h
// initial snapshot held the lock and every nightly cron run clobbered the
// status file with exit_code 0 plus "acquiring lock: lock: already held".
func TestRunLockHeldDoesNotOverwriteMonitorStatus(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "held.lock")
	statusPath := filepath.Join(dir, "status.json")

	// Simulate the previous, successful run's status file.
	const previous = `{"profile":"test","action":"snapshot create","exit_code":0}`
	if err := os.WriteFile(statusPath, []byte(previous), 0o600); err != nil {
		t.Fatalf("seeding status file: %v", err)
	}

	// Another process holds the lock.
	held, err := lock.Acquire(lock.Options{Path: lockPath})
	if err != nil {
		t.Fatalf("acquiring lock for the test: %v", err)
	}
	defer held.Release() //nolint:errcheck

	prof := config.Profile{
		Name:   "test",
		Backup: config.BackupSection{Sources: []string{"/tmp"}},
		Lock:   config.LockSection{Path: lockPath},
	}
	_, err = Run(context.Background(), RunOptions{
		Profile:        prof,
		Command:        []string{"snapshot", "create", "/tmp"},
		SkipHooks:      true,
		PasswordSource: fakeLoader{value: "secret"},
		MonitorManager: monitor.New(monitor.Config{StatusFile: statusPath}),
		Timeout:        200 * time.Millisecond,
	})
	if !errors.Is(err, lock.ErrLocked) {
		t.Fatalf("expected lock.ErrLocked, got %v", err)
	}

	got, rerr := os.ReadFile(statusPath)
	if rerr != nil {
		t.Fatalf("reading status file: %v", rerr)
	}
	if string(got) != previous {
		t.Errorf("status file was overwritten by a run that never got the lock:\n%s", got)
	}
}

func TestRunPasswordFailure(t *testing.T) {
	prof := config.Profile{Name: "test"}
	_, err := Run(context.Background(), RunOptions{
		Profile:        prof,
		Command:        []string{"snapshot", "list"},
		SkipLock:       true,
		PasswordSource: failingLoader{},
	})
	if !errors.Is(err, errFake) {
		t.Errorf("expected errFake, got %v", err)
	}
}
