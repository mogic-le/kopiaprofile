package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mogic-le/kopiaprofile/internal/config"
	"github.com/mogic-le/kopiaprofile/internal/monitor"
	"github.com/mogic-le/kopiaprofile/internal/mounts"
	"github.com/mogic-le/kopiaprofile/internal/profile"
	"github.com/mogic-le/kopiaprofile/internal/secrets"
	"github.com/mogic-le/kopiaprofile/internal/wrapper"
)

// runProfileCmd is invoked when the user types
//
//	kopiaprofile <profile> <action> [args...]
//
// The first positional argument is the profile name; the rest is the
// kopia subcommand to execute.
func runProfileCmd(flags *rootFlags, args []string) error {
	if len(args) < 2 {
		return errorf("usage: kopiaprofile <profile> <action> [args...]")
	}
	profileName := args[0]
	action := args[1]
	rest := args[2:]

	// Filter out kopiaprofile-internal flags from the user-supplied
	// `rest` so they don't get forwarded to kopia. `--dry-run` is
	// handled by the runner, not by kopia - and NOT by --verbose (see
	// below): the two used to be conflated (DryRun was wired to
	// flags.Verbose), so `-v`/`--verbose` silently skipped ever
	// running kopia at all, contradicting its own documented meaning
	// ("print the exact kopia command line ... before each run" - not
	// "instead of a run"). --verbose already gets its printing from
	// rootLogger() switching to Debug level (see the "kopia argv"
	// debug log in profile.Run), so it needs no DryRun wiring at all.
	dryRun := false
	filtered := rest[:0]
	for _, a := range rest {
		if a == "--dry-run" {
			dryRun = true
			continue
		}
		filtered = append(filtered, a)
	}
	rest = filtered

	cfg, err := loadConfig(flags)
	if err != nil {
		return err
	}
	prof, ok := cfg.Get(profileName)
	if !ok {
		return errorf("unknown profile %q", profileName)
	}

	// Expand templates once.
	expanded, err := config.ExpandTemplates(prof)
	if err != nil {
		return errorf("expanding templates: %w", err)
	}

	// Build the kopia argv from (action, rest) plus profile flags.
	kopiaArgs, err := buildKopiaArgs(expanded, action, rest)
	if err != nil {
		return err
	}

	// For the `copy` action we need to connect to the source
	// repository first, with a possibly different password AND
	// its own kopia.config / cache directory so the connect
	// doesn't overwrite the target's kopia.config.
	var preCommands [][]string
	var prePassword string
	var preKopiaConfigDir string
	var preCacheDir string
	if action == "copy" {
		preCommands = [][]string{wrapper.BuildSourceConnectArgs(expanded.Copy.Source)}
		if !expanded.Copy.Source.Password.IsZero() {
			srcProf := expanded
			srcProf.Password = expanded.Copy.Source.Password
			srcLoader := secrets.FromProfile(srcProf)
			if pw, perr := srcLoader.Load(); perr == nil {
				prePassword = pw
			} else {
				return errorf("loading source password: %w", perr)
			}
		}
		// Use a separate kopia.config + cache for the source
		// connection so it doesn't pollute the target. We
		// derive the path from the profile's own config dir
		// (when set) or from a sibling of the target's config
		// dir.
		if home, err := os.UserHomeDir(); err == nil {
			preKopiaConfigDir = filepath.Join(home, ".cache", "kopiaprofile", profileName+"-src", "kopia")
			preCacheDir = filepath.Join(home, ".cache", "kopiaprofile", profileName+"-src", "cache")
		}
	}

	// backup.exclude / backup.exclude-file and retention.keep-* have no
	// kopia snapshot-create equivalent (kopia rejects "--ignore=" and has
	// no per-invocation retention flag - verified against a real kopia
	// 0.23.1); both are policy state, applied as pre-commands against
	// the SAME already-connected repository (preKopiaConfigDir/
	// preCacheDir stay empty, unlike the `copy` case above, so
	// profile.Run does not redirect to a different kopia.config).
	// Running this before every snapshot is safe and keeps the policy in
	// sync if the profile's exclude/retention settings ever change.
	//
	// The clear and the add-ignore/keep-* are two SEPARATE kopia
	// invocations, not one combined command - see
	// wrapper.BuildPolicyClearIgnoreArgs's doc comment for why
	// combining them silently drops every --add-ignore.
	if action == "snapshot" || action == "snap" {
		var policyArgs []string
		ignoreArgs, ierr := wrapper.BuildPolicyIgnoreArgs(expanded)
		if ierr != nil {
			return ierr
		}
		if len(ignoreArgs) > 0 {
			preCommands = append(preCommands, wrapper.BuildPolicyClearIgnoreArgs())
		}
		policyArgs = append(policyArgs, ignoreArgs...)
		if retentionArgs := wrapper.BuildPolicyRetentionArgs(expanded); len(retentionArgs) > 0 {
			if len(policyArgs) == 0 {
				policyArgs = retentionArgs
			} else {
				// retentionArgs already starts with "policy","set","--global";
				// only append its actual flags.
				policyArgs = append(policyArgs, retentionArgs[3:]...)
			}
		}
		if len(policyArgs) > 0 {
			preCommands = append(preCommands, policyArgs)
			pw, perr := secrets.FromProfile(expanded).Load()
			if perr != nil {
				return errorf("loading password for policy: %w", perr)
			}
			prePassword = pw
		}
	}

	// Warn (never fail) about the same filesystem being reachable from
	// more than one path inside this profile's backup sources, which
	// would otherwise scan and hash the same data twice per run. See
	// internal/mounts.
	var mountWarnings []string
	if action == "snapshot" || action == "snap" {
		if groups, merr := mounts.DetectDuplicates("", expanded.Backup.Sources); merr == nil {
			for _, g := range groups {
				w := fmt.Sprintf("same filesystem mounted at multiple backup paths: %s", strings.Join(g.Paths, ", "))
				mountWarnings = append(mountWarnings, w)
				Print("WARNING: %s", w)
			}
		}
	}

	// Run.
	var monitorManager *monitor.Manager
	if isMonitoredAction(action) {
		monitorManager = buildMonitorForProfile(cfg, expanded)
	}
	res, err := profile.Run(context.Background(), profile.RunOptions{
		Profile:           expanded,
		Command:           kopiaArgs,
		Writer:            os.Stdout,
		ErrWriter:         os.Stderr,
		Logger:            rootLogger(flags),
		DryRun:            dryRun,
		Timeout:           24 * time.Hour,
		MonitorManager:    monitorManager,
		PreCommands:       preCommands,
		PrePassword:       prePassword,
		PreKopiaConfigDir: preKopiaConfigDir,
		PreCacheDir:       preCacheDir,
		Warnings:          mountWarnings,
	})
	if err != nil {
		Print("profile %q failed: %v", profileName, err)
		return err
	}
	if res.Kopia != nil {
		Print("kopia exited with code %d in %s", res.Kopia.ExitCode, res.Duration)
	}
	return nil
}

// isMonitoredAction reports whether an action represents a backup
// lifecycle event worth recording in the monitor status file -
// "snapshot"/"snap" (the backup itself) and "prune" (maintenance,
// where kopia's retention policy actually gets enforced). Read-only or
// administrative actions (check-index, display, status, connect, ...)
// must NOT touch the status file: it is shared per-profile, and a
// diagnostic command overwriting it would hide the last real backup's
// outcome from monitoring - observed live: a manual "check-index" run
// made an Icinga check report "no recent backup" right after a
// snapshot had actually succeeded. resticprofile does not have this
// failure mode at all, because its status JSON is structurally only
// ever about backup/retention, never "whatever ran last".
func isMonitoredAction(action string) bool {
	switch action {
	case "snapshot", "snap", "prune":
		return true
	default:
		return false
	}
}

// buildMonitorForProfile creates a monitor.Manager from a profile's
// monitor block. The per-profile config wins over the global one
// (if any), and the status file is auto-anchored in
// ~/.cache/kopiaprofile/<profile>/status.json if not set
// explicitly.
func buildMonitorForProfile(cfg *config.File, p config.Profile) *monitor.Manager {
	defaultPath := ""
	if home, err := os.UserHomeDir(); err == nil {
		defaultPath = filepath.Join(home, ".cache", "kopiaprofile", p.Name, "status.json")
	}
	configs := []monitor.Config{}
	if defaultPath != "" {
		configs = append(configs, monitor.Config{StatusFile: defaultPath})
	}
	if p.Monitor.StatusFile != "" {
		// Expand ~ and relative paths against the config dir.
		expanded := p.Monitor.StatusFile
		if expanded[0] == '~' {
			if home, err := os.UserHomeDir(); err == nil {
				expanded = filepath.Join(home, expanded[1:])
			}
		}
		if !filepath.IsAbs(expanded) {
			expanded = filepath.Join(filepath.Dir(activeFlags.ConfigFile), expanded)
		}
		configs = append(configs, monitor.Config{
			StatusFile:  expanded,
			PushGateway: p.Monitor.PushGateway,
			PushLabels:  p.Monitor.PushLabels,
		})
	}
	if p.Monitor.PushGateway != "" {
		timeout := 10 * time.Second
		if p.Monitor.Timeout != "" {
			if d, err := time.ParseDuration(p.Monitor.Timeout); err == nil {
				timeout = d
			}
		}
		configs = append(configs, monitor.Config{
			PushGateway: p.Monitor.PushGateway,
			PushLabels:  p.Monitor.PushLabels,
			Timeout:     timeout,
		})
	}
	return monitor.New(configs...)
}

// buildKopiaArgs assembles the kopia command line for a given profile
// action. It returns the full argv (kopia-prefixed) and is shared
// between the regular flow and tests.
func buildKopiaArgs(p config.Profile, action string, rest []string) ([]string, error) {
	args := []string{action}
	switch action {
	case "snapshot", "snap":
		// `kopiaprofile home snapshot` / `kopiaprofile home snapshot
		// create /path` -> `kopia snapshot create /path --tags=...
		// --ignore=...`
		//
		// Bare `snapshot` (no args at all) is the resticprofile-style
		// "just back this profile up" shorthand - equivalent to
		// `resticprofile backup` needing no further arguments. It is
		// unambiguous: kopiaprofile's own action set already has a
		// separate `snapshots` (plural) action for listing, so a bare
		// singular `snapshot` can only ever mean "take one". Falls
		// back to the profile's backup.sources when no positional
		// source path is given either way.
		//
		// Examples:
		//   kopiaprofile p snapshot                    -> uses backup.sources
		//   kopiaprofile p snapshot create              -> uses backup.sources
		//   kopiaprofile p snapshot create /tmp/foo     -> uses /tmp/foo
		//   kopiaprofile p snapshot list                 -> list, no fallback
		if len(rest) == 0 || rest[0] == "create" {
			after := rest
			if len(rest) > 0 {
				after = rest[1:]
			}
			if len(after) == 0 {
				after = append(after, p.Backup.Sources...)
			}
			args = append(args, "create")
			args = append(args, after...)
		} else {
			args = append(args, rest...)
		}
		snapshotArgs, err := wrapper.BuildSnapshotArgs(p)
		if err != nil {
			return nil, err
		}
		args = append(args, snapshotArgs...)
	case "snapshots":
		// `kopiaprofile home snapshots` -> `kopia snapshot list --all`
		args = []string{"snapshot", "list", "--all"}
	case "mount":
		// `kopiaprofile home mount <mountpoint>` -> `kopia mount all <mountpoint> --fuse-allow-other`
		source := "all"
		target := ""
		if len(rest) > 0 {
			target = rest[0]
		}
		if len(rest) > 1 {
			source = rest[0]
			target = rest[1]
		}
		args = []string{"mount", source, target}
		args = append(args, wrapper.BuildMountArgs(p)...)
	case "restore":
		// kopia 0.23: `snapshot restore <root-id> <target-path>`
		// The user invokes: `kopiaprofile <p> restore <root-id> <target-path>`
		// We map to: `kopia snapshot restore <root-id> <target-path>`
		// plus any flags from restore.* in the profile.
		args = []string{"snapshot", "restore"}
		args = append(args, rest...)
		args = append(args, wrapper.BuildRestoreArgs(p)...)
	case "verify":
		args = []string{"snapshot", "verify"}
		args = append(args, rest...)
		args = append(args, wrapper.BuildVerifyArgs(p)...)
	case "status":
		args = []string{"repository", "status", "--json"}
	case "prune":
		args = []string{"maintenance", "run", "--full"}
	case "check-index":
		// `kopia index optimize` is a mutating compaction command hidden
		// behind --dangerous-commands=enabled (it can drop content) - not
		// what a read-only "check" action should run. `index inspect
		// --all` reports on every index blob, active and inactive,
		// without changing anything.
		args = []string{"index", "inspect", "--all"}
	case "forget":
		// Kopia does not have a "forget" command. Retention is implicit:
		// the profile's retention.keep-* values are applied via
		// "kopia policy set --global --keep-*=..." before every
		// snapshot (see the pre-command built above), and kopia expires
		// old snapshots against that policy as part of maintenance
		// ("kopiaprofile <p> prune").
		return nil, errorf(`"forget" is implicit in Kopia; configure retention.keep-* in the profile, it is applied automatically on the next "kopiaprofile <p> snapshot"`)
	case "connect":
		// Self-contained: buildProfileFlags deliberately never adds
		// connection flags for "repository connect" (see its comment),
		// so this action must supply --bucket/--access-key/... itself,
		// same as BuildSourceConnectArgs does for the `copy` action's
		// source. Without this, "connect" emitted a bare "repository
		// connect <type>" with no storage flags at all and always
		// failed with "required flag(s) ... not provided".
		connectArgs := wrapper.BuildConnectArgs(p.Repository)
		if connectArgs == nil {
			return nil, errorf("action %q requires repository.type to be set", action)
		}
		args = append(connectArgs, rest...)
	case "init":
		args = []string{"repository", "create"}
		args = append(args, p.Repository.Type)
		args = append(args, rest...)
		// Storage flags (--bucket, --prefix, --access-key, ...) are
		// added by the wrapper's buildProfileFlags(). Adding them
		// here too would cause "flag '...' cannot be repeated"
		// errors.
	case "copy":
		// Multi-repo copy: kopia repository sync-to <target>
		// The source is connected first (via PreCommand).
		if p.Copy.IsZero() {
			return nil, errorf("action %q requires a `copy:` block in the profile", action)
		}
		if p.Copy.Source.Type == "" {
			return nil, errorf("action %q requires copy.source.type", action)
		}
		args = []string{"repository", "sync-to"}
		args = append(args, wrapper.BuildCopyArgs(p, "")...)
	default:
		return nil, errorf("unknown action %q", action)
	}
	return args, nil
}

// loadConfig loads + resolves the configuration file.
func loadConfig(flags *rootFlags) (*config.File, error) {
	cfg, err := config.Load(config.LoadOptions{ConfigPath: flags.ConfigFile})
	if err != nil {
		return nil, errorf("loading config: %w", err)
	}
	if err := cfg.Resolve(); err != nil {
		return nil, errorf("resolving inheritance: %w", err)
	}
	return cfg, nil
}

// rootLogger returns a *slog.Logger that respects --verbose/--quiet.
func rootLogger(flags *rootFlags) *slog.Logger {
	level := slog.LevelInfo
	if flags.Verbose {
		level = slog.LevelDebug
	}
	if flags.Quiet {
		level = slog.LevelError
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

// errorf builds an error with a friendly message; used everywhere.
func errorf(format string, args ...interface{}) error {
	return fmt.Errorf(format, args...)
}

// Print writes a line to stdout. Centralised to make it easy to swap
// for a buffered writer in tests. The Fprintf error is intentionally
// ignored: a broken stdout is a non-actionable runtime error and
// would only obscure the real exit code of the underlying command.
var Print = func(format string, args ...interface{}) {
	_, _ = fmt.Fprintf(os.Stdout, format+"\n", args...) //nolint:errcheck
}

// _ is used to keep "io" imported when no helper uses it directly yet
// (e.g. a future tee writer).
var _ = io.Discard

// _ ensures slog is reachable (used by rootLogger).
var _ = slog.New
