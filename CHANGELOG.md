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
