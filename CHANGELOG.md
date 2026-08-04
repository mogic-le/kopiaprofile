# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

The release workflow copies the body of the new version's section
into the GitHub release notes. See
[`docs/release-process.md`](docs/release-process.md) for the
maintainer's checklist.

## [Unreleased]

### Added

### Changed

### Fixed

## [0.5.12] - 2026-08-04

### Fixed

- `attempts` now actually reaches the status file. 0.5.10 documented the field
  and set it on the result, but the JSON struct had nowhere to put it, so it was
  dropped at serialisation and no monitoring could ever see a repeated run.

## [0.5.11] - 2026-08-03

### Added

- New `policy:` block, which declares kopia's error-handling policy in the
  profile instead of leaving it as hand-set state inside the repository:
  `ignore-file-errors`, `ignore-dir-errors`, `ignore-unknown-types`, globally
  and per path. Each target becomes a `kopia policy set` pre-command before the
  snapshot, the same way `retention:` and the ignore rules already work.
- Per-path targets are the reason this exists. A live object store keeps every
  object in its own directory and deletes objects while the snapshot walks
  them, so every run reports a vanished entry as a fatal error while the
  snapshot itself is complete. Tolerating that for the one subtree is a
  different decision from tolerating it for the whole host, and only the
  narrow one is safe to make.
- The fields are `*bool`, so "not configured" and "configured to false" stay
  distinguishable: `false` is forwarded to kopia, an omitted field emits no
  flag and leaves the repository's value alone. Same reasoning as the `*int`
  retention values in 0.5.6, where a plain type silently dropped the setting.
- `kopiaprofile display` prints the block, with `-` for an unset field.

## [0.5.10] - 2026-07-31

### Added

- New `retry:` block per profile (`attempts`, `delay`), which repeats a
  snapshot run that failed **without having written a snapshot**. It
  targets the one failure class that actually costs a backup: a
  transient backend error before or during the upload. Seen repeatedly
  against S3, which answers a small metadata read with HTTP 200, the
  correct `Content-Length` and an empty body; five hosts were hit in one
  night, the window is minutes to hours, and an attempt an hour later
  would have saved every one of them.
- The retry is deliberately narrow, so it can be enabled everywhere. A
  run is only repeated when the action is a snapshot, the attempt
  failed, and no snapshot reached the repository. Kopia folds a failure
  of its auto-maintenance into the snapshot's own exit code, so a run
  whose backup is in the repository and whose cleanup failed afterwards
  looks like a failure too - repeating that would re-run the same
  failing cleanup for nothing. A run that lost the race for the profile
  lock is not repeated either.
- The retry wraps the pre-commands as well, not just `snapshot create`.
  The policy pre-commands are the first thing that touches the
  repository, so a failing metadata read surfaces there first.
- The status file gains `attempts`. `start_at` stays the first attempt's,
  so `duration` covers the waiting between attempts, and a monitoring
  check that looks at the age of `end_at` sees when the run really
  finished.

## [0.5.9] - 2026-07-31

First release that actually ships the duplicate-mount exclusion. 0.5.7
and 0.5.8 were tagged but never published: their draft releases were
built while the Windows test job was red, first from an endless loop
(fixed in 0.5.8) and then from the regression test below.

### Fixed

- `TestDetectDuplicatesSkipsExcludedMountpoints` failed on Windows. Its
  first half needs a detected duplicate, and `deviceOf` has no device
  number to compare there, so the call returned no group at all - the
  same reason `TestDetectDuplicatesSameFilesystemTwoMountpoints` already
  skipped that platform. Test-only; no change to the binary's behaviour
  on any platform.

## [0.5.8] - 2026-07-31

### Fixed

- The glob branch of the duplicate-mount exclusion could loop forever on
  Windows, hanging the whole `internal/mounts` test binary until the
  10 minute timeout. The ancestor walk terminated on `p != "/"` and used
  `path/filepath`, whose `Dir` is separator-aware: on Windows
  `filepath.Dir` of a lone backslash is that same backslash, so the
  condition never became true. Mountpoints come from `/proc/mounts` and
  are always slash-separated, so the walk now uses `path` instead of
  `path/filepath`, and terminates on `Dir(p) == p` rather than on a
  hardcoded separator. Introduced in 0.5.7 and caught by CI before that
  release was published.

## [0.5.7] - 2026-07-31

### Fixed

- The duplicate-mount detection no longer warns about mountpoints the
  profile excludes. It only ever received `backup.sources`, never the
  ignore patterns, so a filesystem that was deliberately kept out of the
  backup was still reported as "same filesystem mounted at multiple
  backup paths" - a warning whose cause was already fixed and which no
  configuration change could silence. Seen on a k3s node where
  `/var/lib/kubelet` and an auto-mounted duplicate of a data volume were
  both excluded and both still reported, one of them listing fifteen
  kubelet bind-mount paths.
- The matching is deliberately conservative: only anchored patterns are
  considered, against the mountpoint and each of its ancestors, with
  globs via `filepath.Match`. Unanchored gitignore-style patterns are not
  interpreted, because reimplementing kopia's matcher could drift from it
  and the two possible mistakes are not equally bad - failing to suppress
  a warning is noise, wrongly suppressing one hides a filesystem that
  really is read twice.

### Changed

- New `wrapper.EffectiveIgnorePatterns`, so the ignore patterns handed to
  kopia as policy and the ones the mount detection reasons about come
  from one place instead of being assembled twice.

## [0.5.6] - 2026-07-30

### Fixed

- A retention value of `0` in a profile now reaches kopia instead of being
  silently dropped. `retention.keep-*` were plain ints, so `keep-hourly: 0`
  was indistinguishable from not mentioning `keep-hourly` at all: no
  `--keep-hourly` flag was emitted and whatever kopia already had in its
  global policy stayed. On a fresh repository that is kopia's own default,
  so a profile could declare `keep-hourly: 0` and `keep-latest: 0` while
  the repository kept expiring against 48 and 10. Zero is how a retention
  class is switched off, so it has to be forwarded.

### Changed

- `retention.keep-*` are now optional (`*int`) so "not configured" and
  "configured to zero" are distinct, the same distinction kopia makes
  internally with `snapshot/policy.OptionalInt`. Unset fields still emit no
  flag and leave kopia's value alone; existing profiles are unaffected.
- `kopiaprofile display` prints an unset retention value as `-` instead of
  `0`, and now also shows `hourly`. Printing both cases as `0` hid exactly
  the difference above.

## [0.5.5] - 2026-07-29

### Fixed

- `object-lock.extend-on-maintenance` now actually does something. It was
  read into the config struct and then never used: the function meant to
  apply it existed but had no callers, so every profile could declare
  `extend-on-maintenance: true` while the repository had extension
  disabled the whole time. Confirmed live across a fleet - every
  repository reported "Object Lock Extension: disabled" while every
  profile claimed otherwise. It is now applied as a pre-command before
  each snapshot, on the same path as the policy pre-commands, so it runs
  against the already-connected repository and cannot silently become
  dead code again.
- The value is written in both directions
  (`--extend-object-locks=true|false`) rather than only when true, so the
  profile is the single source of truth and flipping it back to `false`
  actually disables extension instead of leaving a stale setting behind.
- The pre-command password is now loaded whenever any pre-command is
  queued, not only when policy arguments were built. Previously a
  pre-command that ended up being the only one would have run without
  credentials.

### Changed

- `wrapper.ApplyObjectLockMaintenance` is replaced by
  `wrapper.BuildObjectLockMaintenanceArgs`, matching the other
  `Build*Args` helpers.

## [0.5.4] - 2026-07-29

### Added

- `run-timeout` per profile: caps how long a single kopia invocation may
  run before it is killed, as a Go duration string. The cap used to be
  hardcoded at 24 hours with no way to change it, which silently
  truncated any run that legitimately needed longer. Observed live on a
  multi-terabyte initial snapshot that had written 1.2 TiB when the cap
  killed it after exactly 24h, leaving only checkpoint snapshots in the
  repository and no way to ever complete in one run. Unset still means
  24h, so nothing changes for existing configurations. A malformed or
  non-positive value is an error rather than a silent fallback to the
  default - quietly reinstating the 24h cap on a profile that asked for
  more would reintroduce exactly the truncation the setting prevents.

## [0.5.3] - 2026-07-29

### Fixed

- A run that cannot acquire the profile lock no longer overwrites the
  monitor status file. It never touched the repository, so it has
  nothing to report about the backup - but writing the status anyway
  destroyed the record of the run that did, and replaced its `end_at`
  with the current time, so an age check would see a fresh timestamp
  for a backup that never happened. Observed live on a host whose
  multi-hour initial snapshot held the lock while every scheduled run
  behind it clobbered the status file with `exit_code: 0` plus
  `error: "acquiring lock: lock: already held"`. Leaving the previous
  status alone is the safe behaviour: the overlapping run still fails
  visibly to whatever scheduled it, and if the lock holder never
  finishes, the untouched `end_at` ages past the warning and critical
  thresholds on its own, which is the alert that should fire.

## [0.5.2] - 2026-07-28

### Fixed

- The `snapshots` action now forwards extra arguments to kopia instead
  of discarding them. It hardcoded its argv, so
  `kopiaprofile <profile> snapshots -- --json` silently dropped the
  flag and printed kopia's human-readable table. `--json` is what
  makes this action usable as a machine-readable backup inventory
  source (snapshot manifest ID, root object ID, source paths,
  size/file counts, `retentionReason`), so the flag has to reach
  kopia. Note the `--` separator: kopiaprofile parses its own flags
  strictly, and that applies to every pass-through action
  (`restore`, `verify`, `mount`, `snapshot create`), not just this one.
- kopiaprofile's own run diagnostics ("kopia exited with code N in D",
  "profile X failed") now go to stderr instead of stdout. They were
  appended to the same stream as kopia's payload output, so the
  epilogue landed after the closing `]` of a `--json` document and
  made it unparseable (observed live: `jq` failing with "Invalid
  numeric literal"). `--quiet` did not suppress them either - it only
  ever affected the log level. stdout now carries exactly what kopia
  produced.

## [0.5.1] - 2026-07-28

### Added

- `kopiaprofile <profile> watch`: reports whether a profile is
  currently running (pid, host, start time, elapsed) and the tail of
  its live progress output, without touching kopia or the repository -
  useful for checking on a long-running backup (large host, slow
  network, initial full upload) from a second session without
  interrupting it. Parses kopia's own periodic progress line
  (`XX.XX% ... ETA ...`) into a one-line summary when present, and
  falls back to the raw tailed output otherwise. Flags a run as
  possibly stuck if its progress log hasn't been updated in over 15
  minutes while its lock is still held.
- `internal/lock`: exported `Info`, `ReadInfo`, and `IsRunning` for
  reading a profile's lock file (PID/host/start-time, and whether that
  PID is still alive) without acquiring it - the basis for `watch`.
- `internal/progress`: new package that tails a profile's progress log
  and parses kopia's progress-line format.
- `internal/profile.Run` now tees the main kopia invocation's stdout/
  stderr into a per-profile progress log (a sibling of the lock file,
  `.progress.log`), truncated at the start of each run. Best-effort:
  a failure to open the log never fails the actual backup.

### Fixed

- `internal/lock` on Windows: the process-liveness check used to send
  `os.Kill` as its "is this PID still alive" probe. Unix's equivalent
  (signal 0) is harmless, but Windows' `os.Process.Signal` only
  implements `os.Interrupt` and `os.Kill` - `Kill` actually terminates
  the process. In practice this meant checking whether a lock's holder
  was still running could kill a legitimate, still-in-progress backup
  on Windows right before treating its lock as stale and taking over.
  Found live in CI (`go test -race` on windows-latest self-terminated
  its own test binary the moment a test exercised this path against a
  real PID) while adding the `Info`/`IsRunning` groundwork for `watch`
  above - nothing before this release ever called that code path
  against a genuinely live process, so the bug had no prior symptom.
  Windows now only relies on `os.FindProcess` succeeding or failing and
  never sends a signal at all.

## [0.4.0] - 2026-07-24

### Added

- `internal/sysmem`: `backup.parallel` is now clamped to what the host's
  currently-available memory can plausibly support, not just used as
  configured. A fixed fleet-wide (or even per-host) `--parallel` value
  can't account for how much memory is free at backup time - that
  depends on whatever else is running on the host, not just its
  installed RAM. Found live: a host with 7.6GB RAM and no swap, already
  busy with Docker/MinIO/Traefik, was kernel-OOM-killed mid-scan at
  `parallel=8` (confirmed via dmesg, not a cgroup limit). The clamp only
  ever narrows the configured value based on `/proc/meminfo`'s
  `MemAvailable` (budgeting ~512MB/worker, using at most 70% of what's
  currently free) - it never raises it, never invents a value, and is a
  no-op on platforms without `/proc/meminfo`.

## [0.3.1] - 2026-07-24

### Fixed

- **Critical regression from 0.2.9/0.3.0**: `kopia policy set --global
  --clear-ignore --add-ignore=...` (issued as a single combined
  pre-command) silently drops every `--add-ignore` when `--clear-ignore`
  is present in the same invocation - verified live against a real
  kopia 0.23.1 repository, reproduced on demand. Since 0.2.9 introduced
  `--clear-ignore` to fix the previous stale-ignore-list bug, every
  snapshot has actually been running with an EMPTY global ignore list -
  no excludes applied at all. This is what caused the `/sys/kernel/
  tracing` hang fixed at the fleet-config level to keep recurring even
  after the exclude was added: the exclude was never actually being set.
  Fixed by running the clear and the add-ignore/retention-keep* as two
  separate kopia invocations (`profile.RunOptions.PreCommands`, plural,
  replacing the single `PreCommand`) instead of one combined command.
  Verified live: after this fix, `kopia policy show --global` correctly
  shows the full ignore list, not an empty one.

## [0.3.0] - 2026-07-23

### Added

- Duplicate-mount detection (`internal/mounts`): before every `snapshot`,
  kopiaprofile now scans for the same filesystem being reachable from
  more than one path inside the profile's `backup.sources` - the common
  case where an auto-mounted volume is also mounted or bind-mounted
  wherever the application actually expects it, without the original
  mount ever being removed. Found live: a volume mounted both at its
  auto-mount path and at the application's data directory, which would
  have scanned and hashed the same data twice every run. This is a
  warning, not a failure - it's printed and recorded in the status
  file's new `warnings` field, not treated as an error.

## [0.2.9] - 2026-07-23

### Fixed

- `FileLoader` (password source `file`) skipped lines starting with `#`
  as comments. A generated password that happens to start with `#` left
  a single-line password file with no non-comment line at all, so the
  lookup failed with "password not found" and the backup failed before
  ever touching the repository. Found live on a freshly onboarded host.
  The fix removes comment-skipping entirely: the first non-empty line is
  now always taken verbatim as the password.

## [0.2.8] - 2026-07-23

### Fixed

- `policy set --global --add-ignore=...` (run before every snapshot to sync
  a profile's `exclude`/`exclude-file` into kopia's repository policy) was
  purely additive: a pattern removed from the exclude file stayed stuck in
  the global ignore list forever, silently continuing to exclude real data
  on every subsequent run. Found live: a host's data volume mounted at
  `/mnt` kept getting excluded from backups even after `/mnt` was removed
  from its exclude file, because the previous run had already added it to
  the policy. Fixed by prefixing the command with `--clear-ignore` so each
  run fully replaces the ignore list instead of merging into it.

## [0.2.7] - 2026-07-22

### Fixed

- Release binaries embedded the source's `X.Y.Z-dev` default version
  string instead of the real tag, all the way back to v0.0.1: GoReleaser
  set `-X main.version=...`, but the variable lives in package `cmd`
  (`cmd/version.go`), not `main` - the linker silently ignores an `-X`
  target that doesn't exist rather than failing the build. Verified by
  extracting the embedded string from a downloaded v0.2.6 release binary.
  Also dropped `-X main.commit/date/builtBy`, equally dead - nothing in
  the codebase declares those variables in any package.

## [0.2.6] - 2026-07-22

### Fixed

- `TestWriteStatusFilePermissions` (added in 0.2.5) asserted a POSIX 0644
  mode unconditionally, failing CI on windows-latest (Windows reports 0666
  instead - it has no POSIX permission bits at all). Test now skips the
  assertion on Windows.

## [0.2.5] - 2026-07-22

### Fixed

- The monitor status file was overwritten by every action, not just
  backups - running a diagnostic command like `check-index` after a
  successful snapshot made monitoring report "no recent backup" even
  though the backup had genuinely succeeded (observed live). Only
  `snapshot`/`snap` and `prune` now write to it; read-only/administrative
  actions (`check-index`, `display`, `status`, `connect`, ...) leave the
  last recorded backup status untouched.

## [0.2.4] - 2026-07-22

### Fixed

- Bumped `golang.org/x/text` v0.25.0 -> v0.39.0 (transitive, via hclparse),
  fixing GO-2026-5970 (infinite loop on invalid input) that was failing
  `govulncheck` in CI.

## [0.2.3] - 2026-07-22

### Fixed

- The status file (`monitor.status-file`) was written with mode 0600
  (`os.CreateTemp`'s default, unaffected by the later rename), unreadable
  by a non-root monitoring user - observed live against resticprofile's
  equivalent file, which is 0644. Now explicitly chmod'd to 0644 before the
  rename; the file only ever holds run metadata, never a secret.

## [0.2.2] - 2026-07-21

### Fixed

- Two unchecked `f.Close()` errcheck lint findings (`internal/wrapper/kopia.go`,
  `internal/wrapper/wrapper_test.go`) that were failing CI on every push since
  0.1.0. No behavior change.

## [0.2.1] - 2026-07-21

### Fixed

- `connect` emitted a bare `repository connect <type>` with no storage
  flags at all and always failed with "required flag(s) '--secret-access-key',
  '--bucket', '--access-key' not provided". `buildProfileFlags` deliberately
  never adds connection flags for the `connect` subcommand (that suppression
  exists for the `copy` action's source pre-connect, which is self-contained
  via `BuildSourceConnectArgs`); the top-level `connect` action shared the
  same subcommand name but never got its own flags built. Fixed with a new
  `BuildConnectArgs`, self-contained the same way.
- `check-index` mapped to `kopia index optimize`, a mutating compaction
  command hidden behind `--dangerous-commands=enabled` (it can drop content)
  in current kopia - not what a read-only "check" action should run, and it
  failed out of the box. Now maps to `kopia index inspect --all`, which
  reports on every index blob without changing anything.
- `retention.keep-*` in a profile was purely decorative: nothing ever issued
  the corresponding `kopia policy set --keep-*` calls, so old snapshots were
  never actually expired regardless of what the profile configured. Now
  applied via `kopia policy set --global --keep-*=...` before every
  snapshot, merged into the same pre-command that already applies
  `backup.exclude`/`exclude-file`.
- `monitor status`/`monitor list` looked in a flat
  `~/.cache/kopiaprofile/monitor/` directory that no run ever wrote to (runs
  write per-profile, at `~/.cache/kopiaprofile/<profile>/status.json` or the
  profile's own `monitor.status-file`). `status` now takes an optional
  `<profile>` argument (defaulting to the sole configured profile) and
  `list` enumerates every configured profile's actual status file.

## [0.2.0] - 2026-07-21

### Added

- `repository create` now forwards the `object-lock:` profile block to kopia
  as `--retention-mode` (uppercased to `COMPLIANCE`/`GOVERNANCE`, which is the
  only form kopia's enum accepts) and `--retention-period`. Previously the
  block was validated and recorded but never reached kopia, so `repository
  create` ran without retention and the repository relied solely on a
  bucket-level default retention. Letting kopia manage per-blob retention is
  the setup kopia is designed for: it locks the data/index/format blobs
  (prefixes `p`/`q`/`x`/`n`/`kopia.repository`/`kopia.blobcfg`) while leaving
  session markers deletable.

### Fixed

- `backup.exclude` / `backup.exclude-file` are now applied via a
  `kopia policy set --global --add-ignore=...` pre-command before each
  snapshot, instead of being passed to `kopia snapshot create` as `--ignore=`.
  `kopia snapshot create` has no `--ignore` flag at all (verified against
  kopia 0.23.1: it fails with `unknown long flag '--ignore'`), so the 0.1.0
  handling never actually worked - excludes silently did nothing and
  everything under the source path was backed up. Ignore rules are policy
  state in kopia; the pre-command is idempotent (kopia de-duplicates the
  ignore list) so re-running it before every snapshot keeps the policy in
  sync with the profile.

## [0.1.0] - 2026-07-20

### Added

- Bare `<profile> snapshot` (no `create`, no path) now falls back to
  `backup.sources`, matching resticprofile's `backup`-needs-no-arguments
  shorthand. Previously the `backup.sources` fallback only triggered when
  `create` was given explicitly, so the shortest possible invocation
  produced a bare `kopia snapshot` (list-like, no `create` subcommand),
  which kopia rejects outright on any of the `snapshot create`-only
  flags (e.g. `--parallel`).
- `backup.exclude-file:` is now actually honored. Kopia has no
  `--exclude-file=` flag of its own; the file's patterns (one glob per
  line, blank lines and `#` comments skipped) are now read and expanded
  into individual `--ignore=` flags, same as `backup.exclude:`.
  Previously the field was parsed from config and merged during profile
  inheritance but never forwarded to kopia at all, so a configured
  exclude file was silently ignored and everything under the source
  path(s) got backed up, unfiltered.

### Fixed

- `-v`/`--verbose` no longer silently skips running kopia. It was
  wired to the same `DryRun` flag as the (separate, per-action)
  `--dry-run`, so passing `--verbose` - documented only as printing the
  command line before each run - actually ran nothing at all. `--dry-run`
  is now the only way to trigger a dry run; `--verbose` only raises the
  log level (which already prints the exact, secret-masked command
  line).

## [0.0.2] - 2026-07-20

### Fixed

- `schedule install --format=systemd` now actually runs `systemctl
  daemon-reload` and `systemctl enable --now` for every installed
  `.timer` unit, matching what its own `--help` text always claimed.
  Previously it only printed those commands as a suggestion, so an
  installed schedule stayed inactive until an operator ran them by
  hand.

## [0.0.1] - 2026-06-05

Initial public release.

### Added

- Single configuration file (YAML / TOML / HCL / JSON) describing one
  or more profiles, with profile inheritance and `<list>-merge`
  strategies (`replace` / `append` / `prepend` / `unique`).
- Profile-level `repository:`, `password:`, `backup:`, `restore:`,
  `retention:`, `cache-dir:`, `lock:`, `monitor:`, `schedule:`,
  `run-before/-after/-after-fail/-finally:` blocks.
- Go-template rendering for any string value
  (`{{ .Profile.Name }}`, `{{ .Env.X }}`, `{{ .Now }}`).
- Secret-aware logging (password / connect-string / source path
  redaction).
- CLI: `init`, `display`, `profiles list`, `passwd`, `generate`,
  `schedule list/render/install`, `monitor status/list`,
  `completion`.
- Cross-platform pre-built binaries for darwin / linux / windows /
  freebsd / openbsd / netbsd × amd64 / arm64 / arm / 386, plus
  `.deb`, `.rpm`, `.apk` packages and a CycloneDX SBOM.
- File-based lock to prevent concurrent profile runs.
- Schedule rendering to crontab, systemd and launchd.
- Monitor that records every run as JSON and (optionally) pushes
  Prometheus metrics to a Pushgateway.
- Multi-repository copy via `kopia repository sync-to`, with
  independent source / target kopia.config directories.
- S3 Object-Lock support (compliance / governance) surfaced as a
  first-class `repository.object-lock:` block.
- GitHub Actions CI: build, test, lint on Linux + macOS + Windows.
  E2E smoke test against a `kopia` filesystem backend.
- GoReleaser v2 release pipeline with cosign keyless signing.

[0.2.0]: https://github.com/mogic-le/kopiaprofile/releases/tag/v0.2.0
[0.1.0]: https://github.com/mogic-le/kopiaprofile/releases/tag/v0.1.0
[0.0.2]: https://github.com/mogic-le/kopiaprofile/releases/tag/v0.0.2
