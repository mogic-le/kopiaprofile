package wrapper

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/mogic-le/kopiaprofile/internal/config"
)

// Runner holds the prepared `kopia` invocation.
type Runner struct {
	Binary string
	Args   []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// Env are extra environment variables to add to the kopia process.
	Env []string
	// Password is injected as KOPIA_PASSWORD for the kopia process.
	Password string
	Timeout  time.Duration
}

// Result captures the outcome of a kopia invocation.
type Result struct {
	ExitCode int
	Stdout   string
	Stderr   string
	StartAt  time.Time
	EndAt    time.Time
	Err      error
}

// Options configures Runner construction.
type Options struct {
	KopiaBinary string
	Profile     config.Profile
	Command     []string // e.g. ["snapshot", "create"]
	Password    string
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
	Timeout     time.Duration
}

// New constructs a Runner for the given profile and command. It
// returns an error if the command cannot be built (e.g. missing
// required fields).
func New(opts Options) (*Runner, error) {
	if opts.KopiaBinary == "" {
		opts.KopiaBinary = "kopia"
	}
	args := []string{}
	args = append(args, opts.Command...)

	// Inject --config-file for repository isolation. If a profile
	// specifies a custom kopia-config-dir we honour it; otherwise we
	// use the default.
	if dir := strings.TrimSpace(opts.Profile.KopiaConfigDir); dir != "" {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("creating kopia config dir: %w", err)
		}
		args = append(args, "--config-file="+dir+"/repository.config")
	}

	// Append profile-derived flags
	// For `repository connect/create/sync-to` the caller has already
	// supplied the connection subcommand + flags positionally, so we
	// skip the connection flags here. Otherwise we'd emit a
	// duplicate `--bucket`, `--access-key`, etc.
	args = append(args, buildProfileFlags(opts.Profile, opts.Command)...)

	return &Runner{
		Binary:   opts.KopiaBinary,
		Args:     args,
		Stdin:    opts.Stdin,
		Stdout:   opts.Stdout,
		Stderr:   opts.Stderr,
		Password: opts.Password,
		Timeout:  opts.Timeout,
	}, nil
}

// Command returns the exact argv that will be passed to kopia, with
// the password masked. Useful for --verbose logging.
func (r *Runner) Command() string {
	parts := []string{r.Binary}
	for _, a := range r.Args {
		if strings.Contains(a, "secret") || strings.Contains(a, "password") {
			parts = append(parts, "********")
		} else {
			parts = append(parts, a)
		}
	}
	return strings.Join(parts, " ")
}

// env returns the environment for the kopia process. Includes the
// password via KOPIA_PASSWORD plus any caller-supplied env entries.
func (r *Runner) env() []string {
	env := os.Environ()
	env = append(env, r.Env...)
	if r.Password != "" {
		env = append(env, "KOPIA_PASSWORD="+r.Password)
	}
	return env
}

// Run executes the kopia binary and waits for it to exit. Output is
// captured into the configured Stdout/Stderr writers (or, if nil,
// into a buffered buffer that ends up in Result).
func (r *Runner) Run(ctx context.Context) (*Result, error) {
	start := time.Now()
	if r.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.Timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, r.Binary, r.Args...) // #nosec G204 -- argv is built from validated profile flags
	cmd.Env = r.env()
	cmd.Stdin = r.Stdin

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = r.stdoutWriter(&stdoutBuf)
	cmd.Stderr = r.stderrWriter(&stderrBuf)

	err := cmd.Run()
	end := time.Now()

	res := &Result{
		ExitCode: exitCodeFromErr(err),
		Stdout:   stdoutBuf.String(),
		Stderr:   stderrBuf.String(),
		StartAt:  start,
		EndAt:    end,
		Err:      err,
	}
	if err != nil {
		return res, fmt.Errorf("kopia exited with code %d: %w", res.ExitCode, err)
	}
	return res, nil
}

func (r *Runner) stdoutWriter(buf *bytes.Buffer) io.Writer {
	if r.Stdout != nil {
		return io.MultiWriter(r.Stdout, buf)
	}
	return buf
}

func (r *Runner) stderrWriter(buf *bytes.Buffer) io.Writer {
	if r.Stderr != nil {
		return io.MultiWriter(r.Stderr, buf)
	}
	return buf
}

func exitCodeFromErr(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return -1
}

// buildProfileFlags translates a resolved profile into a slice of
// kopia CLI flags. Only flags relevant to the active command are
// added; for example, --tags is not added for `repository status`.
//
// The function is intentionally a long switch (rather than a generic
// struct-tag-driven mapper) so that the mapping is explicit and easy
// to audit.
//
// Repository-connection flags (`--type`, `--bucket`, ...) are only
// emitted for the `repository <subcommand>` family of commands
// (e.g. `repository status`, `repository create`, `repository
// sync-to`). The other top-level commands (`snapshot create`,
// `snapshot list`, `restore`, `mount`, `verify`, `maintenance`,
// `index`, ...) read the connection from the kopia config file
// (`kopia.config`) instead and reject the connection flags with
// "unknown flag".
func buildProfileFlags(p config.Profile, command []string) []string {
	var args []string

	emitConnection := false
	positionalType := false
	if len(command) >= 1 && command[0] == "repository" {
		emitConnection = true
		if len(command) >= 2 {
			switch command[1] {
			case "create":
				// `repository create <type> ...` takes
				// TYPE positionally and rejects --type
				// when given. All other connection flags
				// are still needed.
				positionalType = true
			case "connect":
				// `repository connect <type> ...` is
				// fully supplied by
				// BuildSourceConnectArgs.
				emitConnection = false
			case "sync-to":
				// `repository sync-to <type> ...` is
				// fully supplied by BuildCopyArgs.
				emitConnection = false
			case "status", "validate-provider":
				emitConnection = false
			}
		}
	}

	if emitConnection {
		// Repository-connection flags. Note: `repository create`
		// takes the TYPE positionally, so --type must be skipped
		// for that subcommand.
		if p.Repository.Type != "" && !positionalType {
			args = append(args, "--type="+p.Repository.Type)
		}
		if p.Repository.Bucket != "" {
			args = append(args, "--bucket="+p.Repository.Bucket)
		}
		if p.Repository.Endpoint != "" {
			args = append(args, "--endpoint="+p.Repository.Endpoint)
		}
		if p.Repository.Region != "" {
			args = append(args, "--region="+p.Repository.Region)
		}
		if p.Repository.AccessKey != "" {
			args = append(args, "--access-key="+p.Repository.AccessKey)
		}
		if p.Repository.SecretKey != "" {
			args = append(args, "--secret-access-key="+p.Repository.SecretKey)
		}
		if p.Repository.SessionTok != "" {
			args = append(args, "--session-token="+p.Repository.SessionTok)
		}
		if p.Repository.Prefix != "" {
			args = append(args, "--prefix="+p.Repository.Prefix)
		}
		if p.Repository.Path != "" {
			args = append(args, "--path="+p.Repository.Path)
		}
		if p.Repository.DisableTLS {
			args = append(args, "--disable-tls")
		}

		// Object-Lock retention. Only valid on `repository create`
		// (positionalType), where it initializes Kopia's own blobcfg
		// retention so Kopia locks its data/index/format blobs itself
		// (prefixes p/q/x/n/kopia.repository/kopia.blobcfg). This is NOT the
		// same as a bucket default retention: Kopia deliberately leaves
		// session markers unlocked so it can delete them at each flush.
		// Passing these makes the object-lock config actually reach Kopia
		// instead of relying solely on a bucket-level policy.
		if positionalType && !p.Repository.ObjectLock.IsZero() {
			mode := strings.ToLower(p.Repository.ObjectLock.Mode)
			if mode != "" && mode != "none" {
				// kopia's --retention-mode enum only accepts the uppercase
				// forms GOVERNANCE / COMPLIANCE.
				args = append(args, "--retention-mode="+strings.ToUpper(mode))
				if p.Repository.ObjectLock.RetentionPeriod != "" {
					args = append(args, "--retention-period="+p.Repository.ObjectLock.RetentionPeriod)
				}
			}
		}
	}

	// Cache directory: only valid for `repository` subcommands.
	if emitConnection && p.CacheDir != "" {
		args = append(args, "--cache-directory="+p.CacheDir)
	}

	// Backup-only flags. They are only included when the binary
	// is invoked with a "snapshot create" command. We detect that
	// by scanning the runner's Args (set by New) — but that would
	// create a cycle. Instead the caller is expected to add these
	// flags itself (see cmd/run.go). We expose BuildSnapshotArgs()
	// for that purpose.

	return args
}

// HasSubcommandType reports whether the runner's command is a kopia
// subcommand that takes a TYPE as a positional argument (e.g.
// `repository create <type> ...`, `repository connect <type> ...`).
// In that case buildProfileFlags must NOT emit --type=<X> because it
// would clash with the positional. The list of verbs is hard-coded;
// the rest of the call site (cmd/run.go) is responsible for adding
// the positional type at the right place.
//
// `snapshot create` is also in the list because kopia's snapshot
// command also rejects --type as a duplicate; the connection flags
// only matter for the persistent (top-level) kopia options, which
// are read from the kopia config file (or from the connection
// subcommand we ran earlier). Emitting them again here is
// redundant and triggers "unknown flag --type" on the snapshot
// subcommand.
func HasSubcommandType(command []string) bool {
	if len(command) < 2 {
		return false
	}
	switch command[0] {
	case "repository":
		switch command[1] {
		case "create", "connect", "sync-to":
			return true
		}
	case "snapshot", "snap":
		switch command[1] {
		case "create", "verify", "list", "restore", "delete", "fix", "pin":
			return true
		}
	}
	return false
}

// BuildSourceConnectArgs returns the argv for `kopia repository
// connect <type> ...flags` to open the *source* repository of a
// multi-repo-copy profile. The returned argv is suitable as the
// PreCommand of a profile.Run.
//
// The source repository lives in profile.Copy.Source. Its password
// is taken from a separate secrets.Loader (constructed in cmd/run.go
// from source.Password) and passed via the runner's PrePassword.
func BuildSourceConnectArgs(s config.SourceRepository) []string {
	if s.Type == "" {
		return nil
	}
	args := []string{"repository", "connect", s.Type}
	if s.Bucket != "" {
		args = append(args, "--bucket="+s.Bucket)
	}
	if s.Endpoint != "" {
		args = append(args, "--endpoint="+s.Endpoint)
	}
	if s.Region != "" {
		args = append(args, "--region="+s.Region)
	}
	if s.AccessKey != "" {
		args = append(args, "--access-key="+s.AccessKey)
	}
	if s.SecretKey != "" {
		args = append(args, "--secret-access-key="+s.SecretKey)
	}
	if s.SessionTok != "" {
		args = append(args, "--session-token="+s.SessionTok)
	}
	if s.Prefix != "" {
		args = append(args, "--prefix="+s.Prefix)
	}
	if s.Path != "" {
		args = append(args, "--path="+s.Path)
	}
	if s.DisableTLS {
		args = append(args, "--disable-tls")
	}
	for k, v := range s.ExtraFlags {
		if v == "" {
			args = append(args, "--"+k)
		} else {
			args = append(args, "--"+k+"="+v)
		}
	}
	return args
}

// tagCounter is incremented once per bare tag (no colon) seen by
// BuildSnapshotArgs. It is intentionally package-scoped because
// BuildSnapshotArgs has no other state and the counter only needs to
// disambiguate within a single call. The address is threaded through
// kopiaTagSpec.
var bareTagCounter int

// kopiaTagSpec converts a user-supplied tag into a kopia-compatible
// "key:value" string. Bare tags like "nightly" become "tag1:nightly",
// "tag2:nightly", … so they don't collide on the "tag" key.
func kopiaTagSpec(tag string, counter *int) string {
	if strings.Contains(tag, ":") {
		return tag
	}
	*counter++
	return fmt.Sprintf("tag%d", *counter) + ":" + tag
}

// readIgnorePatterns reads a resticprofile-style exclude file (one glob
// pattern per line, blank lines and "#" comments skipped) and returns the
// patterns found. Kopia has no "--exclude-file=" flag of its own (ignore
// rules are either per-directory .kopiaignore files or repository policy -
// see kopia.io/docs/advanced/kopiaignore and BuildPolicyIgnoreArgs).
func readIgnorePatterns(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var patterns []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	return patterns, scanner.Err()
}

// BuildSnapshotArgs returns the kopia flags corresponding to a
// profile's backup section. Pass the source list (positional args) at
// the end.
func BuildSnapshotArgs(p config.Profile) ([]string, error) {
	var args []string
	if p.Backup.IgnoreIdentical {
		args = append(args, "--ignore-identical-snapshots=true")
	}
	if p.Backup.Parallel > 0 {
		args = append(args, fmt.Sprintf("--parallel=%d", p.Backup.Parallel))
	}
	if p.Backup.Description != "" {
		args = append(args, "--description="+p.Backup.Description)
	}
	if p.Backup.FailFast {
		args = append(args, "--fail-fast")
	}
	if p.Backup.ForceHashPercent > 0 {
		args = append(args, fmt.Sprintf("--force-hash=%g", p.Backup.ForceHashPercent))
	}
	if p.Backup.CheckpointUploadMB > 0 {
		args = append(args, fmt.Sprintf("--upload-limit-mb=%d", p.Backup.CheckpointUploadMB))
	}
	if p.Backup.OverrideSource != "" {
		args = append(args, "--override-source="+p.Backup.OverrideSource)
	}
	if p.Backup.StdinFile != "" {
		args = append(args, "--stdin-file="+p.Backup.StdinFile)
	}
	if !p.Backup.SendSnapshotReport {
		// Kopia 0.23 does not accept `--send-snapshot-report=false`;
		// the canonical form is `--no-send-snapshot-report`.
		args = append(args, "--no-send-snapshot-report")
	}
	// Excludes (Backup.Exclude / Backup.ExcludeFile) are NOT emitted
	// here. "kopia snapshot create" has no "--ignore=" flag at all -
	// verified live against a real kopia 0.23.1 ("kopia snapshot create
	// --help" lists no such flag; passing one fails with "unknown long
	// flag '--ignore'"). Ignore rules in kopia are policy-based, set
	// via "kopia policy set --add-ignore=...". See BuildPolicyIgnoreArgs
	// and cmd/run.go, which runs that as a pre-command before create.
	for _, tag := range p.Backup.Tags {
		// Kopia uses "key:value" tags and DE-DUPLICATES by key.
		// A list of two bare tags like ["demo", "src"] would
		// both be converted to "tag:demo" and "tag:src" — kopia
		// rejects that with "Duplicate tag <tag> found".
		//
		// We disambiguate by naming bare tags "tag1", "tag2",
		// … unless the value already contains a colon. Users
		// who want stable keys should write the full
		// "key:value" form in YAML.
		args = append(args, "--tags="+kopiaTagSpec(tag, &bareTagCounter))
	}
	for k, vs := range p.OtherFlags {
		// OtherFlags is map[string][]string. Empty entries become a
		// bare flag (e.g. --verbose); non-empty entries become
		// --key=value.
		for _, v := range vs {
			if v == "" {
				args = append(args, "--"+k)
			} else {
				args = append(args, "--"+k+"="+v)
			}
		}
	}
	return args, nil
}

// BuildPolicyIgnoreArgs returns the "policy set --global --add-ignore=..."
// argv needed to apply a profile's Backup.Exclude / Backup.ExcludeFile
// patterns, or nil if there is nothing to apply. This targets the global
// policy (applies to every source in the repository) rather than a
// per-source-path policy, matching resticprofile's model where exclude/
// exclude-file apply uniformly regardless of how many backup.sources are
// configured. Returns (nil, nil) - not an error - when there is nothing to
// set, so callers can skip running it.
func BuildPolicyIgnoreArgs(p config.Profile) ([]string, error) {
	var patterns []string
	patterns = append(patterns, p.Backup.Exclude...)
	if p.Backup.ExcludeFile != "" {
		fromFile, err := readIgnorePatterns(p.Backup.ExcludeFile)
		if err != nil {
			return nil, fmt.Errorf("reading exclude-file %q: %w", p.Backup.ExcludeFile, err)
		}
		patterns = append(patterns, fromFile...)
	}
	if len(patterns) == 0 {
		return nil, nil
	}
	args := []string{"policy", "set", "--global"}
	for _, pattern := range patterns {
		args = append(args, "--add-ignore="+pattern)
	}
	return args, nil
}

// BuildVerifyArgs returns kopia flags for a profile's verify section.
func BuildVerifyArgs(p config.Profile) []string {
	var args []string
	if p.Verify.FilesPercent > 0 {
		args = append(args, fmt.Sprintf("--verify-files-percent=%g", p.Verify.FilesPercent))
	}
	if p.Verify.Parallel > 0 {
		args = append(args, fmt.Sprintf("--parallel=%d", p.Verify.Parallel))
	}
	if p.Verify.MaxErrors > 0 {
		args = append(args, fmt.Sprintf("--max-errors=%d", p.Verify.MaxErrors))
	}
	return args
}

// BuildCopyArgs returns the kopia argv for `repository sync-to`,
// starting with the top-level flags and ending with the target-type
// subcommand and its connection arguments.
//
// `kopia repository sync-to` does NOT use `--from-*` flags; instead
// it reads the *current* repository (the source, configured via
// profile.Repository) and writes to a destination given as a
// positional subcommand. The source's password is passed via the
// runner's Password (the regular KOPIA_PASSWORD env var); the
// destination's password, if it differs, must be supplied by the
// caller via the runner's env.
//
// Kopia 0.23's `sync-to` is a blob-level mirror: it copies all
// BLOBs present in the source to the destination. There is no
// per-snapshot selection. The relevant flags are:
//
//	--update  : overwrite destination blobs with newer source blobs
//	--delete  : delete destination blobs not present in source
//	--parallel: copy parallelism
//
// We map the profile's `copy:` block to these flags as follows:
//
//	all-snapshots  -> implied (sync-to always copies everything)
//	allow-overwrite -> --update
//	skip-identical  -> (not exposed; sync-to is incremental by
//	                   default; identical blobs are skipped
//	                   automatically because their content hashes
//	                   match)
//	parallel       -> --parallel=N
//	progress-interval: -> --progress (boolean only in 0.23)
func BuildCopyArgs(p config.Profile, sourcePassword string) []string {
	out := []string{}
	if p.Copy.AllowOverwrite {
		out = append(out, "--update")
	}
	if p.Copy.Parallel > 0 {
		out = append(out, fmt.Sprintf("--parallel=%d", p.Copy.Parallel))
	}
	if p.Copy.ProgressInterval != "" {
		// Kopia 0.23 only has a boolean --progress flag. The
		// user-set interval is approximated by enabling
		// progress output.
		out = append(out, "--progress")
	}
	// Append the target repository as a positional subcommand.
	out = append(out, buildTargetRepositoryArgs(p)...)
	return out
}

// buildTargetRepositoryArgs renders the profile's primary Repository
// as a positional kopia target subcommand for `repository sync-to`.
// The "target" type is taken from the profile's `repository.type`
// field (the same field used by snapshot/restore); this is
// intentionally what resticprofile does too: the profile already
// *is* the target.
func buildTargetRepositoryArgs(p config.Profile) []string {
	args := []string{p.Repository.Type}
	if p.Repository.Bucket != "" {
		args = append(args, "--bucket="+p.Repository.Bucket)
	}
	if p.Repository.Endpoint != "" {
		args = append(args, "--endpoint="+p.Repository.Endpoint)
	}
	if p.Repository.Region != "" {
		args = append(args, "--region="+p.Repository.Region)
	}
	if p.Repository.AccessKey != "" {
		args = append(args, "--access-key="+p.Repository.AccessKey)
	}
	if p.Repository.SecretKey != "" {
		args = append(args, "--secret-access-key="+p.Repository.SecretKey)
	}
	if p.Repository.SessionTok != "" {
		args = append(args, "--session-token="+p.Repository.SessionTok)
	}
	if p.Repository.Prefix != "" {
		args = append(args, "--prefix="+p.Repository.Prefix)
	}
	if p.Repository.Path != "" {
		args = append(args, "--path="+p.Repository.Path)
	}
	if p.Repository.DisableTLS {
		args = append(args, "--disable-tls")
	}
	for k, v := range p.Repository.ExtraFlags {
		if v == "" {
			args = append(args, "--"+k)
		} else {
			args = append(args, "--"+k+"="+v)
		}
	}
	return args
}

// BuildMountArgs returns kopia flags for a profile's mount section.
func BuildMountArgs(p config.Profile) []string {
	var args []string
	if p.Mount.AllowOther {
		args = append(args, "--fuse-allow-other")
	}
	if p.Mount.AllowNonEmpty {
		args = append(args, "--fuse-allow-non-empty-mount")
	}
	if p.Mount.PreferWebDAV {
		args = append(args, "--webdav")
	}
	if p.Mount.MaxCachedEntries > 0 {
		args = append(args, fmt.Sprintf("--max-cached-entries=%d", p.Mount.MaxCachedEntries))
	}
	if p.Mount.MaxCachedDirs > 0 {
		args = append(args, fmt.Sprintf("--max-cached-dirs=%d", p.Mount.MaxCachedDirs))
	}
	return args
}

// BuildRestoreArgs returns kopia flags for a profile's restore section.
func BuildRestoreArgs(p config.Profile) []string {
	var args []string
	if p.Restore.Mode != "" {
		args = append(args, "--mode="+p.Restore.Mode)
	}
	if p.Restore.OverwriteFiles {
		args = append(args, "--overwrite-files=true")
	}
	if p.Restore.OverwriteDirs {
		args = append(args, "--overwrite-directories=true")
	}
	if p.Restore.OverwriteSym {
		args = append(args, "--overwrite-symlinks=true")
	}
	if p.Restore.IgnoreErrors {
		args = append(args, "--ignore-errors")
	}
	if p.Restore.SkipExisting {
		args = append(args, "--skip-existing")
	}
	if p.Restore.Shallow > 0 {
		args = append(args, fmt.Sprintf("--shallow=%d", p.Restore.Shallow))
	}
	if p.Restore.Parallel > 0 {
		args = append(args, fmt.Sprintf("--parallel=%d", p.Restore.Parallel))
	}
	if p.Restore.SnapshotTime != "" {
		args = append(args, "--snapshot-time="+p.Restore.SnapshotTime)
	}
	return args
}

// FirstLineOf returns the first non-empty line of a string. Useful for
// stripping a leading "Connected to repository" banner from kopia's
// output before further parsing.
func FirstLineOf(s string) string {
	scanner := bufio.NewScanner(strings.NewReader(s))
	for scanner.Scan() {
		if v := strings.TrimSpace(scanner.Text()); v != "" {
			return v
		}
	}
	return ""
}
