package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/mogic-le/kopiaprofile/internal/config"
)

// A bare "snapshot" action (no "create", no path) must behave exactly like
// "snapshot create" with no path: fall back to backup.sources. This is the
// resticprofile-parity shorthand ("resticprofile backup" needs no further
// argument) - and the exact case that failed against a real host before
// this fix ("kopia: error: unknown long flag '--parallel'", because the
// fallback used to require an explicit "create").
func TestBuildKopiaArgsSnapshotBareFallsBackToSources(t *testing.T) {
	p := config.Profile{Backup: config.BackupSection{Sources: []string{"/"}}}
	args, err := buildKopiaArgs(p, "snapshot", nil)
	if err != nil {
		t.Fatalf("buildKopiaArgs: %v", err)
	}
	joined := strings.Join(args, " ")
	if !strings.HasPrefix(joined, "snapshot create /") {
		t.Errorf("expected \"snapshot create /...\", got: %v", args)
	}
}

func TestBuildKopiaArgsSnapshotCreateExplicitPathWins(t *testing.T) {
	p := config.Profile{Backup: config.BackupSection{Sources: []string{"/"}}}
	args, err := buildKopiaArgs(p, "snapshot", []string{"create", "/tmp/foo"})
	if err != nil {
		t.Fatalf("buildKopiaArgs: %v", err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "/tmp/foo") {
		t.Errorf("expected explicit path /tmp/foo, got: %v", args)
	}
	if strings.Contains(joined, "create / ") || strings.HasSuffix(joined, "create /") {
		t.Errorf("explicit path should replace backup.sources, got: %v", args)
	}
}

func TestBuildKopiaArgsSnapshotListPassesThrough(t *testing.T) {
	p := config.Profile{Backup: config.BackupSection{Sources: []string{"/"}}}
	args, err := buildKopiaArgs(p, "snapshot", []string{"list"})
	if err != nil {
		t.Fatalf("buildKopiaArgs: %v", err)
	}
	joined := strings.Join(args, " ")
	if !strings.HasPrefix(joined, "snapshot list") {
		t.Errorf("expected \"snapshot list\" to pass through unchanged, got: %v", args)
	}
	if strings.Contains(joined, "/ ") || strings.HasSuffix(joined, " /") {
		t.Errorf("backup.sources should not be injected for an explicit non-create action, got: %v", args)
	}
}

// The "snapshots" action used to hardcode its argv and drop `rest`
// entirely, so `kopiaprofile <p> snapshots --json` ran a plain
// `kopia snapshot list --all` and returned human-readable table output.
// The flag has to reach kopia for this action to be usable as a
// machine-readable backup inventory source.
func TestBuildKopiaArgsSnapshotsPassesThroughFlags(t *testing.T) {
	args, err := buildKopiaArgs(config.Profile{}, "snapshots", []string{"--json"})
	if err != nil {
		t.Fatalf("buildKopiaArgs: %v", err)
	}
	joined := strings.Join(args, " ")
	if joined != "snapshot list --all --json" {
		t.Errorf(`expected "snapshot list --all --json", got: %v`, args)
	}
}

func TestBuildKopiaArgsSnapshotsWithoutFlags(t *testing.T) {
	args, err := buildKopiaArgs(config.Profile{}, "snapshots", nil)
	if err != nil {
		t.Fatalf("buildKopiaArgs: %v", err)
	}
	joined := strings.Join(args, " ")
	if joined != "snapshot list --all" {
		t.Errorf(`expected "snapshot list --all", got: %v`, args)
	}
}

// "check-index" used to map to "kopia index optimize", a mutating
// compaction command gated behind --dangerous-commands=enabled - not
// what a read-only "check" should run, and it fails out of the box.
// It must map to the read-only "index inspect --all" instead.
func TestBuildKopiaArgsCheckIndexIsReadOnly(t *testing.T) {
	args, err := buildKopiaArgs(config.Profile{}, "check-index", nil)
	if err != nil {
		t.Fatalf("buildKopiaArgs: %v", err)
	}
	joined := strings.Join(args, " ")
	if joined != "index inspect --all" {
		t.Errorf(`expected "index inspect --all", got: %v`, args)
	}
}

// The "connect" action used to emit a bare "repository connect <type>"
// with no storage flags at all, because buildProfileFlags deliberately
// never adds them for the "connect" subcommand (that suppression exists
// for the copy action's source pre-connect, which is self-contained via
// BuildSourceConnectArgs instead). It must build its own flags the same
// way, via wrapper.BuildConnectArgs.
func TestBuildKopiaArgsConnectIncludesStorageFlags(t *testing.T) {
	p := config.Profile{
		Repository: config.Repository{
			Type:      "s3",
			Bucket:    "my-bucket",
			AccessKey: "AKID",
			SecretKey: "SECRET",
		},
	}
	args, err := buildKopiaArgs(p, "connect", nil)
	if err != nil {
		t.Fatalf("buildKopiaArgs: %v", err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"repository connect s3", "--bucket=my-bucket", "--access-key=AKID", "--secret-access-key=SECRET"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q in connect args, got: %v", want, args)
		}
	}
}

func TestBuildKopiaArgsConnectRequiresType(t *testing.T) {
	if _, err := buildKopiaArgs(config.Profile{}, "connect", nil); err == nil {
		t.Error("expected error when repository.type is unset")
	}
}

// The per-invocation timeout used to be hardcoded at 24h, which silently
// truncated any legitimately longer run - observed live on a
// multi-terabyte initial snapshot that was killed after writing 1.2 TiB,
// leaving only checkpoints behind. run-timeout makes it configurable;
// an unset value must still yield the 24h default.
func TestResolveRunTimeoutDefault(t *testing.T) {
	got, err := resolveRunTimeout("")
	if err != nil {
		t.Fatalf("resolveRunTimeout: %v", err)
	}
	if got != 24*time.Hour {
		t.Errorf("expected 24h default, got %v", got)
	}
}

func TestResolveRunTimeoutParsesDuration(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want time.Duration
	}{
		{"48h", 48 * time.Hour},
		{"90m", 90 * time.Minute},
		{"72h30m", 72*time.Hour + 30*time.Minute},
	} {
		got, err := resolveRunTimeout(tc.in)
		if err != nil {
			t.Errorf("resolveRunTimeout(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("resolveRunTimeout(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// A malformed or non-positive value must be an error, not a silent
// fallback to the default: quietly capping a profile that asked for
// more at 24h would reintroduce the very truncation this setting exists
// to prevent, and it would do so invisibly.
func TestResolveRunTimeoutRejectsBadValues(t *testing.T) {
	for _, in := range []string{"nonsense", "24", "0", "0s", "-1h"} {
		if _, err := resolveRunTimeout(in); err == nil {
			t.Errorf("resolveRunTimeout(%q) = nil error, want error", in)
		}
	}
}

func TestResolveRetry(t *testing.T) {
	// Unkonfiguriert heisst ein Versuch, nicht null.
	attempts, delay, err := resolveRetry(config.RetrySection{})
	if err != nil {
		t.Fatalf("empty retry: %v", err)
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1", attempts)
	}
	if delay != defaultRetryDelay {
		t.Errorf("delay = %v, want the default %v", delay, defaultRetryDelay)
	}

	attempts, delay, err = resolveRetry(config.RetrySection{Attempts: 2, Delay: "20m"})
	if err != nil {
		t.Fatalf("configured retry: %v", err)
	}
	if attempts != 2 || delay != 20*time.Minute {
		t.Errorf("got attempts %d delay %v, want 2 and 20m", attempts, delay)
	}

	// Eine 0 im Profil ist kein Fehler, sondern heisst "nicht wiederholen".
	if attempts, _, err = resolveRetry(config.RetrySection{Attempts: 0, Delay: "1h"}); err != nil || attempts != 1 {
		t.Errorf("attempts 0: got %d, %v; want 1 and no error", attempts, err)
	}

	for _, bad := range []string{"nonsense", "1", "-5m"} {
		if _, _, err := resolveRetry(config.RetrySection{Attempts: 2, Delay: bad}); err == nil {
			t.Errorf("resolveRetry delay %q = nil error, want error", bad)
		}
	}
}

// Only actions that are actually part of the backup lifecycle may touch
// the monitor status file - a diagnostic command like "check-index"
// overwriting the last real backup's recorded status is exactly the bug
// this guards against (observed live: it made a monitoring check report
// "no recent backup" right after a snapshot had actually succeeded).
func TestIsMonitoredAction(t *testing.T) {
	monitored := []string{"snapshot", "snap", "prune"}
	for _, a := range monitored {
		if !isMonitoredAction(a) {
			t.Errorf("isMonitoredAction(%q) = false, want true", a)
		}
	}
	notMonitored := []string{"check-index", "display", "status", "connect", "init", "snapshots", "mount", "restore", "verify", "copy", "forget", ""}
	for _, a := range notMonitored {
		if isMonitoredAction(a) {
			t.Errorf("isMonitoredAction(%q) = true, want false", a)
		}
	}
}
