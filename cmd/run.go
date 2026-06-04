package cmd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/mogic-le/kopiaprofile/internal/config"
	"github.com/mogic-le/kopiaprofile/internal/monitor"
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
	// supported alongside `--verbose`; both are handled by the
	// runner, not by kopia.
	filtered := rest[:0]
	for _, a := range rest {
		if a == "--dry-run" {
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
	var preCommand []string
	var prePassword string
	var preKopiaConfigDir string
	var preCacheDir string
	if action == "copy" {
		preCommand = wrapper.BuildSourceConnectArgs(expanded.Copy.Source)
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

	// Run.
	res, err := profile.Run(context.Background(), profile.RunOptions{
		Profile:           expanded,
		Command:           kopiaArgs,
		Writer:            os.Stdout,
		ErrWriter:         os.Stderr,
		Logger:            rootLogger(flags),
		DryRun:            flags.Verbose,
		Timeout:           24 * time.Hour,
		MonitorManager:    buildMonitorForProfile(cfg, expanded),
		PreCommand:        preCommand,
		PrePassword:       prePassword,
		PreKopiaConfigDir: preKopiaConfigDir,
		PreCacheDir:       preCacheDir,
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
		// `kopiaprofile home snapshot create /path` ->
		// `kopia snapshot create /path --tags=... --ignore=...`
		//
		// If the user invoked the action with the explicit
		// `create` subcommand but did not pass any positional
		// source paths, fall back to the profile's
		// backup.sources. This makes
		// `kopiaprofile home snapshot create` work without
		// repeating the source paths.
		//
		// Examples:
		//   kopiaprofile p snapshot create            -> uses backup.sources
		//   kopiaprofile p snapshot create /tmp/foo   -> uses /tmp/foo
		//   kopiaprofile p snapshot list               -> list, no fallback
		if len(rest) >= 1 && rest[0] == "create" && len(p.Backup.Sources) > 0 {
			after := rest[1:]
			if len(after) == 0 {
				after = append(after, p.Backup.Sources...)
			}
			args = append(args, "create")
			args = append(args, after...)
		} else {
			args = append(args, rest...)
		}
		args = append(args, wrapper.BuildSnapshotArgs(p)...)
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
		args = []string{"index", "optimize"}
	case "forget":
		// Kopia does not have a "forget" command. Retention is applied
		// automatically by `kopia policy set` (handled at init time).
		return nil, errorf(`"forget" is implicit in Kopia; configure retention.once in the profile and run "kopiaprofile <p> init" to apply`)
	case "connect":
		args = []string{"repository", "connect"}
		args = append(args, p.Repository.Type)
		args = append(args, rest...)
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
// for a buffered writer in tests.
var Print = func(format string, args ...interface{}) {
	fmt.Fprintf(os.Stdout, format+"\n", args...)
}

// _ is used to keep "io" imported when no helper uses it directly yet
// (e.g. a future tee writer).
var _ = io.Discard

// _ ensures slog is reachable (used by rootLogger).
var _ = slog.New
