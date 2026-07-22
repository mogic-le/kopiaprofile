package cmd

import (
	"strings"
	"testing"

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

// Only actions that are actually part of the backup lifecycle may touch
// the monitor status file - a diagnostic command like "check-index"
// overwriting the last real backup's recorded status is exactly the bug
// this guards against (observed live: it made an Icinga check report
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
