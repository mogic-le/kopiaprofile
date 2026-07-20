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
