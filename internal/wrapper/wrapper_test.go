package wrapper

import (
	"os"
	"strings"
	"testing"

	"github.com/mogic-le/kopiaprofile/internal/config"
)

func TestBuildSnapshotArgsTags(t *testing.T) {
	p := config.Profile{
		Backup: config.BackupSection{
			Tags: []string{"nightly", "host:web1", "env:prod"},
		},
	}
	args, err := BuildSnapshotArgs(p)
	if err != nil {
		t.Fatalf("BuildSnapshotArgs: %v", err)
	}
	joined := strings.Join(args, " ")
	// Bare tags ("nightly") are disambiguated as "tag1:nightly" /
	// "tag2:…" to avoid kopia's "Duplicate tag" error. The exact
	// counter value is implementation detail; we just want to see
	// the value present.
	if !strings.Contains(joined, ":nightly") {
		t.Errorf("missing nightly tag: %v", args)
	}
	if !strings.Contains(joined, "--tags=host:web1") {
		t.Errorf("missing host tag: %v", args)
	}
	if !strings.Contains(joined, "--tags=env:prod") {
		t.Errorf("missing env tag: %v", args)
	}
}

func TestBuildSnapshotArgsParallel(t *testing.T) {
	p := config.Profile{Backup: config.BackupSection{Parallel: 8}}
	args, err := BuildSnapshotArgs(p)
	if err != nil {
		t.Fatalf("BuildSnapshotArgs: %v", err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--parallel=8") {
		t.Errorf("parallel not set: %v", args)
	}
}

func TestBuildSnapshotArgsNoIgnoreFlag(t *testing.T) {
	// "kopia snapshot create" has no "--ignore=" flag - excludes must go
	// through BuildPolicyIgnoreArgs (kopia policy set), not here. This
	// locks in that BuildSnapshotArgs never regresses to emitting one.
	p := config.Profile{Backup: config.BackupSection{Exclude: []string{"*.tmp"}, ExcludeFile: "/nonexistent"}}
	args, err := BuildSnapshotArgs(p)
	if err != nil {
		t.Fatalf("BuildSnapshotArgs: %v", err)
	}
	for _, a := range args {
		if strings.HasPrefix(a, "--ignore") {
			t.Errorf("BuildSnapshotArgs must not emit --ignore flags, got: %v", args)
		}
	}
}

func TestBuildPolicyIgnoreArgsExcludes(t *testing.T) {
	p := config.Profile{Backup: config.BackupSection{Exclude: []string{"*.tmp", "**/node_modules"}}}
	args, err := BuildPolicyIgnoreArgs(p)
	if err != nil {
		t.Fatalf("BuildPolicyIgnoreArgs: %v", err)
	}
	joined := strings.Join(args, " ")
	if !strings.HasPrefix(joined, "policy set --global") {
		t.Errorf("expected \"policy set --global ...\", got: %v", args)
	}
	if strings.Contains(joined, "--clear-ignore") {
		t.Errorf("--clear-ignore must NOT be combined with --add-ignore in the same invocation (kopia drops the adds), got: %v", args)
	}
	if !strings.Contains(joined, "--add-ignore=*.tmp") {
		t.Errorf("exclude 1: %v", args)
	}
	if !strings.Contains(joined, "--add-ignore=**/node_modules") {
		t.Errorf("exclude 2: %v", args)
	}
}

func TestBuildPolicyIgnoreArgsExcludeFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "exclude-*.txt")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	if _, err := f.WriteString("/proc\n# a comment\n\n/sys\n"); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	p := config.Profile{Backup: config.BackupSection{ExcludeFile: f.Name()}}
	args, err := BuildPolicyIgnoreArgs(p)
	if err != nil {
		t.Fatalf("BuildPolicyIgnoreArgs: %v", err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--add-ignore=/proc") {
		t.Errorf("missing /proc: %v", args)
	}
	if !strings.Contains(joined, "--add-ignore=/sys") {
		t.Errorf("missing /sys: %v", args)
	}
	if strings.Contains(joined, "comment") {
		t.Errorf("comment line leaked into args: %v", args)
	}
}

func TestBuildPolicyIgnoreArgsExcludeFileMissing(t *testing.T) {
	p := config.Profile{Backup: config.BackupSection{ExcludeFile: "/nonexistent/does-not-exist.txt"}}
	if _, err := BuildPolicyIgnoreArgs(p); err == nil {
		t.Error("expected an error for a missing exclude-file, got nil")
	}
}

func TestBuildPolicyIgnoreArgsEmpty(t *testing.T) {
	if args, err := BuildPolicyIgnoreArgs(config.Profile{}); err != nil || args != nil {
		t.Errorf("expected (nil, nil) for a profile with no excludes, got (%v, %v)", args, err)
	}
}

func TestBuildPolicyClearIgnoreArgs(t *testing.T) {
	args := BuildPolicyClearIgnoreArgs()
	joined := strings.Join(args, " ")
	if joined != "policy set --global --clear-ignore" {
		t.Errorf("got: %v", args)
	}
}

func TestBuildPolicyRetentionArgs(t *testing.T) {
	p := config.Profile{
		Retention: config.RetentionSection{
			KeepDaily:   7,
			KeepWeekly:  5,
			KeepMonthly: 12,
		},
	}
	args := BuildPolicyRetentionArgs(p)
	want := []string{"policy", "set", "--global", "--keep-daily=7", "--keep-weekly=5", "--keep-monthly=12"}
	if len(args) != len(want) {
		t.Fatalf("BuildPolicyRetentionArgs = %v, want %v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("BuildPolicyRetentionArgs = %v, want %v", args, want)
		}
	}
}

func TestBuildPolicyRetentionArgsEmpty(t *testing.T) {
	if args := BuildPolicyRetentionArgs(config.Profile{}); args != nil {
		t.Errorf("expected nil for a profile with no retention settings, got %v", args)
	}
}

func TestBuildSnapshotArgsIgnoreIdentical(t *testing.T) {
	p := config.Profile{Backup: config.BackupSection{IgnoreIdentical: true}}
	args, err := BuildSnapshotArgs(p)
	if err != nil {
		t.Fatalf("BuildSnapshotArgs: %v", err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--ignore-identical-snapshots=true") {
		t.Errorf("ignore-identical not set: %v", args)
	}
}

func TestBuildSnapshotArgsNoSnapshotReport(t *testing.T) {
	// Default is true; only when explicitly false should we emit the
	// flag. Kopia 0.23 only accepts --no-send-snapshot-report (the
	// `--send-snapshot-report=false` form is rejected and parsed as a
	// positional source, which causes "unsupported source: .../false"
	// errors).
	p := config.Profile{Backup: config.BackupSection{SendSnapshotReport: false}}
	args, err := BuildSnapshotArgs(p)
	if err != nil {
		t.Fatalf("BuildSnapshotArgs: %v", err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--no-send-snapshot-report") {
		t.Errorf("--no-send-snapshot-report not set: %v", args)
	}
}

func TestBuildRestoreArgs(t *testing.T) {
	p := config.Profile{Restore: config.RestoreSection{
		Mode:           "tar",
		OverwriteFiles: true,
		Parallel:       4,
		Shallow:        2,
	}}
	args := BuildRestoreArgs(p)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--mode=tar") {
		t.Errorf("mode: %v", args)
	}
	if !strings.Contains(joined, "--overwrite-files=true") {
		t.Errorf("overwrite: %v", args)
	}
	if !strings.Contains(joined, "--parallel=4") {
		t.Errorf("parallel: %v", args)
	}
	if !strings.Contains(joined, "--shallow=2") {
		t.Errorf("shallow: %v", args)
	}
}

func TestBuildMountArgs(t *testing.T) {
	p := config.Profile{Mount: config.MountSection{
		AllowOther:       true,
		MaxCachedEntries: 100000,
	}}
	args := BuildMountArgs(p)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--fuse-allow-other") {
		t.Errorf("allow-other: %v", args)
	}
	if !strings.Contains(joined, "--max-cached-entries=100000") {
		t.Errorf("cache: %v", args)
	}
}

func TestBuildVerifyArgs(t *testing.T) {
	p := config.Profile{Verify: config.VerifySection{
		FilesPercent: 5.0,
		Parallel:     8,
	}}
	args := BuildVerifyArgs(p)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--verify-files-percent=5") {
		t.Errorf("files-percent: %v", args)
	}
	if !strings.Contains(joined, "--parallel=8") {
		t.Errorf("parallel: %v", args)
	}
}

func TestObjectLockValidate(t *testing.T) {
	modes := []string{"", "compliance", "governance", "none", "COMPLIANCE"}
	for _, mode := range modes {
		err := ObjectLockAction{Mode: mode}.Validate()
		if err != nil {
			t.Errorf("mode %q should validate: %v", mode, err)
		}
	}
	err := ObjectLockAction{Mode: "garbage"}.Validate()
	if err == nil {
		t.Error("garbage mode should fail")
	}
}

func TestCommandMasksSecrets(t *testing.T) {
	r := &Runner{
		Binary: "kopia",
		Args:   []string{"snapshot", "create", "/path", "--secret-access-key=hunter2"},
	}
	cmd := r.Command()
	if !strings.Contains(cmd, "********") {
		t.Errorf("expected secret to be masked: %q", cmd)
	}
	if strings.Contains(cmd, "hunter2") {
		t.Errorf("expected no plain secret: %q", cmd)
	}
}

func TestBuildCopyArgsBasic(t *testing.T) {
	p := config.Profile{
		Repository: config.Repository{
			Type:       "s3",
			Bucket:     "target-bucket",
			Endpoint:   "s3.example.com",
			AccessKey:  "AKIA-target",
			SecretKey:  "secret-target",
			Prefix:     "backups",
			DisableTLS: true,
		},
		Copy: config.CopySection{
			AllowOverwrite:   true,
			Parallel:         8,
			ProgressInterval: "30s",
		},
	}
	args := BuildCopyArgs(p, "")
	joined := strings.Join(args, " ")
	// kopia 0.23 sync-to: --all-snapshots and --skip-identical are
	// not accepted. allow-overwrite maps to --update.
	if !strings.Contains(joined, "--update") {
		t.Errorf("missing --update: %v", args)
	}
	if !strings.Contains(joined, "--parallel=8") {
		t.Errorf("missing --parallel: %v", args)
	}
	if !strings.Contains(joined, "--progress") {
		t.Errorf("missing --progress: %v", args)
	}
	if !strings.Contains(joined, " s3 ") && !strings.Contains(joined, "s3 --") {
		t.Errorf("missing target type: %v", args)
	}
	if !strings.Contains(joined, "--bucket=target-bucket") {
		t.Errorf("missing target bucket: %v", args)
	}
	if !strings.Contains(joined, "--prefix=backups") {
		t.Errorf("missing target prefix: %v", args)
	}
	if !strings.Contains(joined, "--disable-tls") {
		t.Errorf("missing --disable-tls: %v", args)
	}
}

func TestBuildCopyArgsSkipIdentical(t *testing.T) {
	// SkipIdentical is now a no-op: kopia 0.23 sync-to is
	// always incremental (it skips identical blobs by content
	// hash, not via a flag). The test verifies that the
	// resulting argv is still valid (target args present) and
	// that SkipIdentical does NOT emit an --ignore-identical
	// flag (which kopia would reject).
	p := config.Profile{
		Repository: config.Repository{Type: "filesystem", Path: "/tmp/kopia-target"},
		Copy:       config.CopySection{SkipIdentical: true},
	}
	args := BuildCopyArgs(p, "")
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "--ignore-identical-snapshots") {
		t.Errorf("--ignore-identical-snapshots is not a sync-to flag: %v", args)
	}
	if !strings.Contains(joined, "filesystem") {
		t.Errorf("missing target type: %v", args)
	}
	if !strings.Contains(joined, "--path=/tmp/kopia-target") {
		t.Errorf("missing target path: %v", args)
	}
}

func TestBuildSourceConnectArgsS3(t *testing.T) {
	src := config.SourceRepository{
		Type:       "s3",
		Bucket:     "source-bucket",
		Endpoint:   "s3.example.com",
		AccessKey:  "AKIA-source",
		SecretKey:  "secret-source",
		DisableTLS: true,
	}
	args := BuildSourceConnectArgs(src)
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"repository", "connect", "s3",
		"--bucket=source-bucket",
		"--endpoint=s3.example.com",
		"--access-key=AKIA-source",
		"--secret-access-key=secret-source",
		"--disable-tls",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in source connect args: %v", want, args)
		}
	}
}

func TestBuildSourceConnectArgsEmpty(t *testing.T) {
	if got := BuildSourceConnectArgs(config.SourceRepository{}); got != nil {
		t.Errorf("expected nil for empty source, got %v", got)
	}
}

func TestBuildSourceConnectArgsFilesystem(t *testing.T) {
	src := config.SourceRepository{
		Type: "filesystem",
		Path: "/var/lib/kopia-source",
	}
	args := BuildSourceConnectArgs(src)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "filesystem --path=/var/lib/kopia-source") {
		t.Errorf("filesystem source not rendered: %v", args)
	}
}

func TestBuildProfileFlagsObjectLockOnCreate(t *testing.T) {
	p := config.Profile{
		Repository: config.Repository{
			Type:   "s3",
			Bucket: "b",
			ObjectLock: config.ObjectLockConfig{
				Mode:            "compliance",
				RetentionPeriod: "720h",
			},
		},
	}

	// On "repository create" the object-lock config must reach kopia as
	// --retention-mode (uppercased) and --retention-period.
	create := strings.Join(buildProfileFlags(p, []string{"repository", "create"}), " ")
	if !strings.Contains(create, "--retention-mode=COMPLIANCE") {
		t.Errorf("missing --retention-mode=COMPLIANCE on create: %v", create)
	}
	if !strings.Contains(create, "--retention-period=720h") {
		t.Errorf("missing --retention-period=720h on create: %v", create)
	}

	// On "repository connect" retention flags must NOT be emitted (they are
	// create-only; blobcfg already holds retention once the repo exists).
	connect := strings.Join(buildProfileFlags(p, []string{"repository", "connect"}), " ")
	if strings.Contains(connect, "--retention-mode") || strings.Contains(connect, "--retention-period") {
		t.Errorf("retention flags must not appear on connect: %v", connect)
	}
}

func TestBuildProfileFlagsObjectLockNoneOmitted(t *testing.T) {
	p := config.Profile{
		Repository: config.Repository{
			Type:       "s3",
			Bucket:     "b",
			ObjectLock: config.ObjectLockConfig{Mode: "none", RetentionPeriod: "720h"},
		},
	}

	create := strings.Join(buildProfileFlags(p, []string{"repository", "create"}), " ")
	if strings.Contains(create, "--retention-mode") {
		t.Errorf("mode=none must not emit --retention-mode: %v", create)
	}
}
