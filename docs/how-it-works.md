# How it works

This document explains how `kopiaprofile` is wired together
internally. It is not a usage guide; for that see
[`README.md`](../README.md) and [`configuration.md`](configuration.md).

## High-level flow

```
$ kopiaprofile <profile> <action> [args...]
```

1. The cobra command tree dispatches the first positional argument
   as a profile name.
2. `cmd/run.go::runProfileCmd` loads the configuration file,
   resolves inheritance, expands templates.
3. The action is mapped to a `kopia <subcommand>` invocation by
   `cmd/run.go::buildKopiaArgs`.
4. `internal/profile.Run` then:
   - loads the password through the configured source
     (`internal/secrets`)
   - acquires a file lock (`internal/lock`)
   - runs `run-before` hooks
   - invokes `kopia` via `internal/wrapper`
   - runs `run-after` or `run-after-fail` hooks
   - runs `run-finally`
   - releases the lock
   - records the result via `internal/monitor` (if a `monitor:`
     block is set on the profile)

## Code layout

```
main.go
  └─ cmd/                    cobra commands
       ├─ root.go            entry point + global flags
       ├─ init.go            `kopiaprofile init` skeleton generator
       ├─ profiles.go        `kopiaprofile profiles list`
       ├─ display.go         `kopiaprofile display`
       ├─ passwd.go          `kopiaprofile passwd` (keyring)
       ├─ generate.go        `kopiaprofile generate random`
       ├─ run.go             `kopiaprofile <p> <action>` dispatcher
       ├─ loaders.go         config loaders used by every subcommand
       ├─ schedule.go        `kopiaprofile schedule`
       ├─ monitor.go         `kopiaprofile monitor`
       └─ util.go            small helpers

  └─ internal/
       ├─ config/            YAML/TOML/HCL/JSON loader
       │                      inheritance resolution
       │                      template expansion
       │                      list-merge modes
       │                      secret masking
       ├─ secrets/           password pipeline
       │                      (keyring | command | env | file)
       ├─ lock/              flock-based mutex
       ├─ wrapper/           `kopia` argv assembly
       │                      subcommand builders (snapshot, restore,
       │                      mount, verify, sync-to, …)
       ├─ profile/           run orchestrator
       │                      hook execution
       │                      deferred monitor call
       ├─ schedule/          cron parser + crontab / systemd / launchd
       │                      renderers
       ├─ monitor/           JSON status file + Prometheus push
       └─ types/             shared result types (no behaviour, just data)
```

The `internal/` packages are deliberately self-contained and do not
import each other in cycles. Shared types that would otherwise cause
a cycle live in `internal/types`.

## Adding a new action

To add a new `kopiaprofile <p> myaction`, you only need to:

1. Add a case to `cmd/run.go::buildKopiaArgs`.
2. Add a `BuildMyActionArgs(p config.Profile) []string` helper in
   `internal/wrapper/kopia.go` that produces the `kopia`-side argv.
3. Add a unit test in `internal/wrapper/wrapper_test.go`.

The runner (`internal/profile/runner.go`) takes care of locking,
hooks, password loading and monitor recording; it does not need to
be touched.

## Adding a new monitor output

`internal/monitor.Monitor` is a slice of output configs. To add a
new kind of output (for example, a Loki push, or a local SQLite
index), implement the `Output` interface and register a constructor
in `monitor.New`.
