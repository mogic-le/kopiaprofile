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

[0.1.0]: https://github.com/mogic-le/kopiaprofile/releases/tag/v0.1.0
[0.0.2]: https://github.com/mogic-le/kopiaprofile/releases/tag/v0.0.2
