package profile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/mogic-le/kopiaprofile/internal/config"
	"github.com/mogic-le/kopiaprofile/internal/lock"
	"github.com/mogic-le/kopiaprofile/internal/monitor"
	"github.com/mogic-le/kopiaprofile/internal/secrets"
	"github.com/mogic-le/kopiaprofile/internal/types"
	"github.com/mogic-le/kopiaprofile/internal/wrapper"
)

// Phase identifies the lifecycle point of a hook.
type Phase string

const (
	PhaseBefore    Phase = "before"
	PhaseAfter     Phase = "after"
	PhaseAfterFail Phase = "after-fail"
	PhaseFinally   Phase = "finally"
)

// RunOptions configure a single profile run.
type RunOptions struct {
	Profile   config.Profile
	Command   []string // kopia subcommand + args, e.g. ["snapshot", "create"]
	Writer    io.Writer
	ErrWriter io.Writer
	Logger    *slog.Logger
	// SkipHooks disables run-before/run-after/run-after-fail/run-finally.
	SkipHooks bool
	// SkipLock disables the per-profile file lock.
	SkipLock bool
	// Stdin lets the caller pipe data to kopia (e.g. pg_dump).
	Stdin io.Reader
	// Timeout aborts the kopia invocation after the given duration.
	Timeout time.Duration
	// DryRun only prints the kopia command that would be executed.
	DryRun bool
	// PasswordSource overrides profile.Password. If nil, the profile's
	// own password configuration is used.
	PasswordSource secrets.Loader
	// MonitorManager is invoked with the result of the run. May be nil.
	MonitorManager *monitor.Manager

	// PreCommand, if non-empty, is run BEFORE the main kopia command
	// and before any run-before hook. It is used by the `copy`
	// action to connect to the source repository first. The runner
	// blocks on this command and treats non-zero exit as fatal.
	PreCommand []string
	// PrePassword is the KOPIA_PASSWORD to set for the PreCommand.
	// Use a different password than the main one when the source and
	// target repositories have different passwords.
	PrePassword string
	// PreKopiaConfigDir is the --config-file directory used for the
	// pre-command. Defaults to the main profile's KopiaConfigDir
	// if empty. The `copy` action uses this to keep the source's
	// kopia.config isolated from the target's.
	PreKopiaConfigDir string
	// PreCacheDir is the --cache-directory for the pre-command.
	// Defaults to the main profile's CacheDir if empty.
	PreCacheDir string
	// Warnings are non-fatal findings the caller made before invoking
	// Run (e.g. mounts.DetectDuplicates results). They are copied
	// through to the Result and on to the monitor status file/push, but
	// never affect ExitCode or Err.
	Warnings []string
}

// Result is the outcome of a profile run.
type Result struct {
	Profile  string
	Command  string
	ExitCode int
	Duration time.Duration
	StartAt  time.Time
	EndAt    time.Time
	Hooks    []HookResult
	Kopia    *wrapper.Result
	Err      error
	Warnings []string
}

// HookResult records the execution of a single hook.
type HookResult struct {
	Phase    Phase
	Command  string
	ExitCode int
	Err      error
}

// Run executes the profile. It is the single entry point for any
// `kopiaprofile <profile> <command>` invocation.
func Run(ctx context.Context, opts RunOptions) (*Result, error) {
	if opts.Profile.Name == "" {
		return nil, errors.New("profile runner: profile name is empty")
	}
	if len(opts.Command) == 0 {
		return nil, errors.New("profile runner: command is empty")
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	res := &Result{
		Profile:  opts.Profile.Name,
		Command:  strings.Join(opts.Command, " "),
		StartAt:  time.Now(),
		Warnings: opts.Warnings,
	}
	// Set ExitCode to the kopia result's exit code as we discover it.
	defer func() {
		res.EndAt = time.Now()
		res.Duration = res.EndAt.Sub(res.StartAt)
		if opts.MonitorManager != nil {
			opts.MonitorManager.Run(ctx, resultToMonitor(res), slogToLogger(opts.Logger))
		}
	}()

	// Run pre-command (e.g. "kopia repository connect <source>" for
	// the copy action). Failure here is fatal; we don't run run-fail
	// hooks for the pre-command because the main command never ran.
	if len(opts.PreCommand) > 0 {
		preBinary := opts.Profile.KopiaBinary
		if preBinary == "" {
			preBinary = "kopia"
		}
		// Build a synthetic profile that has the source's
		// kopia-config-dir and cache-dir so the pre-command
		// doesn't pollute the target's kopia.config.
		preProfile := opts.Profile
		if opts.PreKopiaConfigDir != "" {
			preProfile.KopiaConfigDir = opts.PreKopiaConfigDir
		}
		if opts.PreCacheDir != "" {
			preProfile.CacheDir = opts.PreCacheDir
		}
		preRunner, perr := wrapper.New(wrapper.Options{
			KopiaBinary: preBinary,
			Profile:     preProfile,
			Command:     opts.PreCommand,
			Password:    opts.PrePassword,
			Stdin:       opts.Stdin,
			Stdout:      opts.Writer,
			Stderr:      opts.ErrWriter,
			Timeout:     opts.Timeout,
		})
		if perr != nil {
			res.Err = perr
			return res, res.Err
		}
		opts.Logger.Info("running pre-command", "command", preRunner.Command())
		preRes, prerr := preRunner.Run(ctx)
		res.Kopia = preRes
		if prerr != nil {
			res.Err = prerr
			res.ExitCode = preRes.ExitCode
			return res, res.Err
		}
	}

	// Load password
	if opts.PasswordSource == nil {
		opts.PasswordSource = secrets.FromProfile(opts.Profile)
	}
	password, err := opts.PasswordSource.Load()
	if err != nil {
		res.Err = fmt.Errorf("loading password: %w", err)
		return res, res.Err
	}

	// Acquire lock
	var l *lock.Lock
	if !opts.SkipLock {
		l, err = lock.Acquire(lock.Options{
			Path:          resolveLockPath(opts.Profile),
			ForceInactive: opts.Profile.Lock.ForceInactive,
		})
		if err != nil {
			res.Err = fmt.Errorf("acquiring lock: %w", err)
			return res, res.Err
		}
		defer func() {
			if relErr := l.Release(); relErr != nil {
				opts.Logger.Warn("releasing lock", "err", relErr)
			}
		}()
	}

	// Run run-before
	hookErr := runHook(ctx, opts, PhaseBefore, opts.Profile.RunBefore, res)
	if hookErr != nil {
		res.Err = hookErr
		// run-finally is best-effort: if the run-before hook failed
		// there is no point in surfacing a second error to the user,
		// the failure of the original hook is the actionable one.
		_ = runHook(ctx, opts, PhaseFinally, opts.Profile.RunFinally, res) //nolint:errcheck
		return res, res.Err
	}

	// Build & run kopia
	binary := opts.Profile.KopiaBinary
	if binary == "" {
		binary = "kopia"
	}
	r, err := wrapper.New(wrapper.Options{
		KopiaBinary: binary,
		Profile:     opts.Profile,
		Command:     opts.Command,
		Password:    password,
		Stdin:       opts.Stdin,
		Stdout:      opts.Writer,
		Stderr:      opts.ErrWriter,
		Timeout:     opts.Timeout,
	})
	if err != nil {
		res.Err = err
		_ = runHook(ctx, opts, PhaseFinally, opts.Profile.RunFinally, res) //nolint:errcheck
		return res, res.Err
	}
	opts.Logger.Debug("kopia argv", "command", r.Command())

	if opts.DryRun {
		opts.Logger.Info("dry-run: would execute", "argv", r.Command())
		res.Kopia = &wrapper.Result{ExitCode: 0}
		// runHook failure here is logged but does not change the
		// already-successful DryRun result. Errors from runHook
		// are surfaced through the monitor status file.
		_ = runHook(ctx, opts, PhaseAfter, opts.Profile.RunAfter, res)     //nolint:errcheck
		_ = runHook(ctx, opts, PhaseFinally, opts.Profile.RunFinally, res) //nolint:errcheck
		return res, nil
	}

	kopiaRes, kerr := r.Run(ctx)
	res.Kopia = kopiaRes
	if kerr != nil {
		res.Err = kerr
		res.ExitCode = kopiaRes.ExitCode
		// Same reasoning as the run-before path: a failure in
		// run-after-fail or run-finally is best-effort and does
		// not change the user-facing kopia error.
		_ = runHook(ctx, opts, PhaseAfterFail, opts.Profile.RunAfterFail, res) //nolint:errcheck
		_ = runHook(ctx, opts, PhaseFinally, opts.Profile.RunFinally, res)     //nolint:errcheck
		return res, res.Err
	}

	// Success path: run-after and run-finally are best-effort. We
	// do not want a misbehaving notification script to flip a
	// successful run into an error result; failures are recorded
	// in the monitor status file via runHook's own logging.
	_ = runHook(ctx, opts, PhaseAfter, opts.Profile.RunAfter, res)     //nolint:errcheck
	_ = runHook(ctx, opts, PhaseFinally, opts.Profile.RunFinally, res) //nolint:errcheck
	return res, nil
}

func runHook(ctx context.Context, opts RunOptions, phase Phase, cmd string, res *Result) error {
	if cmd == "" || opts.SkipHooks {
		return nil
	}
	hr := HookResult{Phase: phase, Command: cmd}
	res.Hooks = append(res.Hooks, hr)

	var hookCmd *exec.Cmd
	if runtime.GOOS == "windows" {
		hookCmd = exec.CommandContext(ctx, "cmd", "/c", cmd) // #nosec G204 -- user-supplied by design
	} else {
		hookCmd = exec.CommandContext(ctx, "/bin/sh", "-c", cmd) // #nosec G204 -- user-supplied by design
	}
	hookCmd.Env = append(os.Environ(), hookEnv(opts.Profile, res)...)
	if opts.Writer != nil {
		hookCmd.Stdout = opts.Writer
	}
	if opts.ErrWriter != nil {
		hookCmd.Stderr = opts.ErrWriter
	}
	if err := hookCmd.Run(); err != nil {
		exit := -1
		if ee, ok := err.(*exec.ExitError); ok {
			exit = ee.ExitCode()
		}
		// Update the last hook result with exit code
		res.Hooks[len(res.Hooks)-1].ExitCode = exit
		res.Hooks[len(res.Hooks)-1].Err = err
		opts.Logger.Error("hook failed", "phase", phase, "command", cmd, "err", err)
		return err
	}
	res.Hooks[len(res.Hooks)-1].ExitCode = 0
	return nil
}

func hookEnv(p config.Profile, res *Result) []string {
	status := "ok"
	if res.Err != nil {
		status = "fail"
	}
	return []string{
		"KOPIAPROFILE_PROFILE_NAME=" + p.Name,
		"KOPIAPROFILE_PROFILE_COMMAND=" + res.Command,
		"KOPIAPROFILE_STATUS=" + status,
		"KOPIAPROFILE_ERROR=" + errString(res.Err),
	}
}

func errString(e error) string {
	if e == nil {
		return ""
	}
	return e.Error()
}

// slogToLogger wraps an *slog.Logger as a monitor.Logger.
type monitorLogger struct{ inner *slog.Logger }

func (m monitorLogger) Errorf(format string, args ...interface{}) {
	m.inner.Error(fmt.Sprintf(format, args...))
}

// slogToLogger converts a *slog.Logger into a monitor.Logger.
func slogToLogger(l *slog.Logger) monitor.Logger {
	return monitorLogger{inner: l}
}

// resultToMonitor converts a profile.Result into a types.RunResult
// for the monitor package. The conversion is deliberately a copy so
// that the two types can evolve independently.
func resultToMonitor(r *Result) *types.RunResult {
	out := &types.RunResult{
		Profile:  r.Profile,
		Action:   r.Command,
		ExitCode: r.ExitCode,
		StartAt:  r.StartAt,
		EndAt:    r.EndAt,
		Duration: r.Duration,
		Hooks:    make([]types.RunHookResult, 0, len(r.Hooks)),
		Warnings: r.Warnings,
	}
	if host, err := os.Hostname(); err == nil {
		out.Hostname = host
	}
	if r.Err != nil {
		out.Error = r.Err.Error()
	}
	for _, h := range r.Hooks {
		hs := types.RunHookResult{
			Phase:    string(h.Phase),
			Command:  h.Command,
			ExitCode: h.ExitCode,
		}
		if h.Err != nil {
			hs.Error = h.Err.Error()
		}
		out.Hooks = append(out.Hooks, hs)
	}
	if r.Kopia != nil {
		out.Kopia = &types.KopiaResult{
			ExitCode: r.Kopia.ExitCode,
			Argv:     r.Command,
		}
		if len(r.Kopia.Stderr) > 4096 {
			out.Kopia.StderrTail = r.Kopia.Stderr[len(r.Kopia.Stderr)-4096:]
		} else {
			out.Kopia.StderrTail = r.Kopia.Stderr
		}
	}
	return out
}

// resolveLockPath returns the path of the lock file for the given
// profile. Profile.Lock.Path takes precedence; otherwise we default
// to ~/.cache/kopiaprofile/<profile>.lock.
func resolveLockPath(p config.Profile) string {
	if p.Lock.Path != "" {
		return expandHome(p.Lock.Path)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "kopiaprofile-"+p.Name+".lock")
	}
	return filepath.Join(home, ".cache", "kopiaprofile", p.Name+".lock")
}

func expandHome(p string) string {
	if !strings.HasPrefix(p, "~") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, strings.TrimPrefix(p, "~"))
}
