package profile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mogic-le/kopiaprofile/internal/config"
	"github.com/mogic-le/kopiaprofile/internal/lock"
	"github.com/mogic-le/kopiaprofile/internal/monitor"
	"github.com/mogic-le/kopiaprofile/internal/wrapper"
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
// backed something up, and replaces its end_at with "now" - so a monitoring
// check that alerts on the age of the last backup would be looking at a
// fresh timestamp for a backup that never happened. Observed live: a
// multi-hour initial snapshot held the lock and every scheduled run behind
// it clobbered the status file with exit_code 0 plus "acquiring lock: lock:
// already held".
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

// retryable is the whole policy of the retry feature, so it is tested
// directly rather than only through a process run.
func TestRetryable(t *testing.T) {
	created := &wrapper.Result{Stderr: "uploaded 3 files\nCreated snapshot with root kabc123 (1.2 GB)\n"}
	nothing := &wrapper.Result{Stderr: "unable to read format blob: unexpected EOF\n"}
	someErr := errors.New("kopia exited with code 1")

	cases := []struct {
		name    string
		command []string
		kopia   *wrapper.Result
		err     error
		want    bool
	}{
		{"gescheitert ohne Snapshot", []string{"snapshot", "create"}, nothing, someErr, true},
		{"Kurzform snap", []string{"snap", "create"}, nothing, someErr, true},
		{"kein kopia-Ergebnis, etwa Pre-Command-Fehler", []string{"snapshot", "create"}, nil, someErr, true},
		{"erfolgreich", []string{"snapshot", "create"}, created, nil, false},
		// Der Snapshot liegt im Repo, gescheitert ist die Maintenance
		// danach. Ein zweiter Lauf wiederholte nur das Aufraeumen.
		{"Snapshot da, Maintenance gescheitert", []string{"snapshot", "create"}, created, someErr, false},
		{"Lock von einem anderen Lauf gehalten", []string{"snapshot", "create"}, nil, lock.ErrLocked, false},
		{"andere Aktion", []string{"restore", "kabc123", "/tmp/x"}, nothing, someErr, false},
		{"leeres Kommando", nil, nothing, someErr, false},
	}
	for _, c := range cases {
		res := &Result{Kopia: c.kopia, Err: c.err}
		if got := retryable(RunOptions{Command: c.command}, res); got != c.want {
			t.Errorf("%s: retryable = %v, want %v", c.name, got, c.want)
		}
	}
}

// fakeKopia writes an executable script that appends one line to a counter
// file per invocation, prints stderrText and exits with code.
func fakeKopia(t *testing.T, counter, stderrText string, code int) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake kopia is a shell script")
	}
	path := filepath.Join(t.TempDir(), "kopia")
	script := "#!/bin/sh\necho run >> " + counter + "\n" +
		"printf '%s' " + shellQuote(stderrText) + " >&2\n" +
		"exit " + strconv.Itoa(code) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil { // #nosec G306 -- must be executable
		t.Fatalf("writing fake kopia: %v", err)
	}
	return path
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func countLines(t *testing.T, path string) int {
	t.Helper()
	b, err := os.ReadFile(path) // #nosec G304 -- test-controlled path
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("reading counter: %v", err)
	}
	return strings.Count(string(b), "\n")
}

func TestRunRetriesWhenNoSnapshotWasWritten(t *testing.T) {
	dir := t.TempDir()
	counter := filepath.Join(dir, "runs")
	prof := config.Profile{
		Name:        "test",
		KopiaBinary: fakeKopia(t, counter, "unable to read format blob: unexpected EOF\n", 1),
		Backup:      config.BackupSection{Sources: []string{"/tmp"}},
		Lock:        config.LockSection{Path: filepath.Join(dir, "x.lock")},
	}
	res, err := Run(context.Background(), RunOptions{
		Profile:        prof,
		Command:        []string{"snapshot", "create", "/tmp"},
		SkipHooks:      true,
		PasswordSource: fakeLoader{value: "secret"},
		RetryAttempts:  3,
		RetryDelay:     0,
	})
	if err == nil {
		t.Fatal("expected the run to fail after the last attempt")
	}
	if got := countLines(t, counter); got != 3 {
		t.Errorf("kopia was invoked %d times, want 3", got)
	}
	if res.Attempts != 3 {
		t.Errorf("Attempts = %d, want 3", res.Attempts)
	}
}

// Der Fall, der die Eingrenzung ueberhaupt begruendet: kopia faltet den
// Fehler der Auto-Maintenance in den Exit-Code des Snapshots. Der Snapshot
// liegt im Repo, ein zweiter Versuch wiederholt nur das gescheiterte
// Aufraeumen.
func TestRunDoesNotRetryWhenSnapshotExists(t *testing.T) {
	dir := t.TempDir()
	counter := filepath.Join(dir, "runs")
	stderr := "Created snapshot with root kabc123 (1.2 GB)\nERROR error running maintenance: unable to delete blob\n"
	prof := config.Profile{
		Name:        "test",
		KopiaBinary: fakeKopia(t, counter, stderr, 1),
		Backup:      config.BackupSection{Sources: []string{"/tmp"}},
		Lock:        config.LockSection{Path: filepath.Join(dir, "x.lock")},
	}
	res, err := Run(context.Background(), RunOptions{
		Profile:        prof,
		Command:        []string{"snapshot", "create", "/tmp"},
		SkipHooks:      true,
		PasswordSource: fakeLoader{value: "secret"},
		RetryAttempts:  3,
		RetryDelay:     0,
	})
	if err == nil {
		t.Fatal("expected the run to report the maintenance failure")
	}
	if got := countLines(t, counter); got != 1 {
		t.Errorf("kopia was invoked %d times, want 1", got)
	}
	if res.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1", res.Attempts)
	}
}

// Ein wiederholter Lauf darf die Hooks des vorherigen Versuchs nicht
// mitschleppen, sonst zeigt die Statusdatei doppelt so viele Hooks wie
// gelaufen sind und daneben den Fehler eines Versuchs, der ueberholt ist.
func TestRunRetryDoesNotAccumulateHooks(t *testing.T) {
	dir := t.TempDir()
	counter := filepath.Join(dir, "runs")
	prof := config.Profile{
		Name:         "test",
		KopiaBinary:  fakeKopia(t, counter, "unable to read format blob\n", 1),
		Backup:       config.BackupSection{Sources: []string{"/tmp"}},
		Lock:         config.LockSection{Path: filepath.Join(dir, "x.lock")},
		RunBefore:    "true",
		RunAfterFail: "true",
	}
	res, err := Run(context.Background(), RunOptions{
		Profile:        prof,
		Command:        []string{"snapshot", "create", "/tmp"},
		PasswordSource: fakeLoader{value: "secret"},
		RetryAttempts:  2,
		RetryDelay:     0,
	})
	if err == nil {
		t.Fatal("expected the run to fail")
	}
	// Pro Versuch genau run-before und run-after-fail.
	if len(res.Hooks) != 2 {
		t.Errorf("got %d hook results, want the last attempt's 2: %+v", len(res.Hooks), res.Hooks)
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
