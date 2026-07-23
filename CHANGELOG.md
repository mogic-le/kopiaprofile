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
