# kopiaprofile

> A configuration wrapper for [Kopia][kopia], inspired by
> [resticprofile][rp]. Single config file, many profiles, inheritance,
> hooks, locks, secret-aware logging — but for Kopia.

[kopia]: https://github.com/kopia/kopia
[rp]: https://github.com/creativeprojects/resticprofile

[![CI](https://github.com/mogic-le/kopiaprofile/actions/workflows/test.yml/badge.svg)](https://github.com/mogic-le/kopiaprofile/actions/workflows/test.yml)
[![Lint](https://github.com/mogic-le/kopiaprofile/actions/workflows/lint.yml/badge.svg)](https://github.com/mogic-le/kopiaprofile/actions/workflows/lint.yml)
[![Release](https://github.com/mogic-le/kopiaprofile/actions/workflows/release.yml/badge.svg)](https://github.com/mogic-le/kopiaprofile/actions/workflows/release.yml)
[![GitHub release (latest SemVer)](https://img.shields.io/github/v/release/mogic-le/kopiaprofile?sort=semver)](https://github.com/mogic-le/kopiaprofile/releases)
[![License: GPL-3.0](https://img.shields.io/badge/License-GPLv3-blue.svg)](LICENSE)

## What is this?

Kopia is a powerful backup tool with native S3 Object-Lock support, a
web-based server UI and a well-engineered storage layer. It does not,
however, ship a "one config file, many profiles" workflow that
operators are used to from [resticprofile][rp].

`kopiaprofile` is a thin CLI shim around the `kopia` binary. It
reads a single configuration file, resolves inheritance, expands
templates, loads secrets, applies hooks, takes a lock, and then
invokes the right `kopia` subcommand for you.

It does not fork Kopia and does not embed any of Kopia's libraries —
every action is `kopia <subcommand> <args>`, just with the args
assembled from your config.

## Table of contents

- [Install](#install)
- [Quick start](#quick-start)
- [Configuration](#configuration)
- [CLI reference](#cli-reference)
- [Examples](#examples)
- [Schedules](#schedules)
- [Monitoring](#monitoring)
- [S3 Object-Lock](#s3-object-lock)
- [Multi-Repository copy](#multi-repository-copy)
- [Development](#development)
- [Release process](#release-process)
- [License](#license)

## Install

### Pre-built binary

Download the latest release for your platform from the
[GitHub releases page](https://github.com/mogic-le/kopiaprofile/releases).
Each release ships a tarball (Linux / macOS / BSD), a zip (Windows)
and `.deb` / `.rpm` / `.apk` packages.

```bash
# example: install Linux amd64
curl -fsSL https://github.com/mogic-le/kopiaprofile/releases/latest/download/kopiaprofile_VERSION_linux_amd64.tar.gz | tar -xz -C /tmp
sudo mv /tmp/kopiaprofile /usr/local/bin/
kopiaprofile version
```

`kopia` itself must be installed separately and be on `$PATH` (or
referenced via the `kopia-binary` global / per-profile setting).

### Linux package managers

Each release also produces `.deb`, `.rpm` and `.apk` artefacts that
you can install directly with your distro's package manager:

```bash
# Debian / Ubuntu
sudo dpkg -i kopiaprofile_VERSION_amd64.deb

# Fedora / RHEL
sudo rpm -i kopiaprofile_VERSION.x86_64.rpm

# Alpine
sudo apk add --allow-untrusted kopiaprofile_VERSION_x86_64.apk
```

### From source

```bash
go install github.com/mogic-le/kopiaprofile@latest
```

## Quick start

```bash
# 1. Create a skeleton config in the current directory
kopiaprofile init kopiaprofile.yaml --force

# 2. Edit it to your needs
$EDITOR kopiaprofile.yaml

# 3. Verify the config parses
kopiaprofile display home
kopiaprofile profiles list

# 4. Store the repository password in the OS keyring
kopiaprofile passwd home

# 5. Create the kopia repository
kopiaprofile home init

# 6. Run a backup (sources from the profile)
kopiaprofile home snapshot create

# 7. List snapshots
kopiaprofile home snapshots

# 8. Mount all snapshots to a directory
kopiaprofile home mount /mnt/kopia
```

## Configuration

The configuration file format is auto-detected by extension. The
following formats are supported:

| Extension  | Format                          |
|------------|---------------------------------|
| `.yaml` / `.yml` | YAML                       |
| `.toml`    | TOML                             |
| `.hcl`     | HashiCorp Configuration Language |
| `.json`    | JSON                             |

A minimal example:

```yaml
version: "1"

profiles:
  home:
    initialize: true
    repository:
      type: s3
      bucket: my-bucket
      region: eu-central-1
    password:
      source: keyring
    backup:
      sources: [ /home/alice ]
    retention:
      keep-latest: 5
      keep-daily: 7
```

The full schema is documented in [`docs/configuration.md`](docs/configuration.md).
Working examples for common setups are in [`examples/`](examples/).

### Inheritance

A profile can inherit from another profile. The child wins on scalar
fields; lists follow the `<list>-merge` strategy (see below).

```yaml
profiles:
  base:
    repository:
      type: s3
      bucket: shared-bucket
    backup:
      tags: [ "base" ]
  home:
    inherit: base
    backup:
      sources: [ /home/alice ]
      tags-merge: append
      tags: [ "home" ]
```

Multi-level inheritance (`a → b → c`) is supported; cycles are
detected and rejected.

### List-merge modes

`backup.sources`, `backup.exclude` and `backup.tags` are lists that
the child may want to combine with the parent's list. The merge
strategy is chosen per-field via `<list>-merge`:

| Value      | Behaviour                                     |
|------------|-----------------------------------------------|
| `replace`  | (default) child list replaces the parent's     |
| `append`   | child list is appended to the parent's         |
| `prepend`  | child list is prepended to the parent's        |
| `unique`   | union, preserving order and removing duplicates |

```yaml
backup:
  sources:      [ /home/alice ]
  sources-merge: append
  exclude:      [ "*.tmp" ]
  exclude-merge: unique
  tags:         [ "env:prod" ]
  tags-merge:   replace   # default, may be omitted
```

### Templates

Any string value may be a Go template. The following variables are
available:

| Variable             | Description                          |
|----------------------|--------------------------------------|
| `.Hostname`          | short hostname of the machine        |
| `.Profile.Name`      | profile name                         |
| `.Profile.Description` | profile description                |
| `.Env.X`             | environment variable `X`             |
| `.User.Username`     | current OS user                      |
| `.User.Uid` / `.Gid` | numeric UID / GID                    |
| `.Now`               | current time (`time.Time`)           |

```yaml
profiles:
  home:
    repository:
      prefix: "{{ .Profile.Name }}/{{ .Hostname }}"
    env:
      BACKUP_TAG: "{{ .Profile.Name }}-{{ .Now.Format \"20060102\" }}"
    run-after: "/usr/local/bin/notify.sh {{ .Profile.Name }}"
```

### Password pipeline

The repository password is loaded by one of four sources, configured
via the `password:` block of each profile:

| Source    | Configuration                                           |
|-----------|---------------------------------------------------------|
| `keyring` | OS keyring (default). Use `kopiaprofile passwd <prof>` to store. |
| `command` | runs a shell command, first line of stdout is the password |
| `env`     | reads `env: KOPIA_PASSWORD`                              |
| `file`    | reads a file                                             |

```yaml
password:
  source: env
  env: KOPIA_HOME_PASSWORD

# or

password:
  source: file
  file: /root/.secrets/kopia-home

# or

password:
  source: command
  command: pass kopia/home
```

### Hooks

Each profile can run shell commands at four lifecycle points:

| Hook               | When                                              |
|--------------------|---------------------------------------------------|
| `run-before`       | before `kopia` is invoked                         |
| `run-after`        | after a successful kopia run                      |
| `run-after-fail`   | after a failed kopia run (non-zero exit)          |
| `run-finally`      | always, after `run-after` or `run-after-fail`     |

The hook receives a few environment variables: `KOPIAPROFILE_NAME`,
`KOPIAPROFILE_ACTION`, `KOPIAPROFILE_EXIT_CODE`,
`KOPIAPROFILE_DURATION_NS`, `KOPIAPROFILE_KOPIA_EXIT_CODE`.

### Locking

A file-based lock prevents concurrent runs of the same profile. The
default lock path is `/var/lock/kopiaprofile-<profile>.lock`. The
path can be overridden via `lock.path`; `lock.force-inactive: true`
ignores any stale lock.

## CLI reference

Global flags: `--config <file>` (`-c`), `--verbose` (`-v`), `--quiet`.

| Command                                    | Description                                |
|--------------------------------------------|--------------------------------------------|
| `kopiaprofile <profile> <action> [args]`   | run `kopia <action>` for the given profile |
| `kopiaprofile init <file> [--format=…]`    | generate a skeleton configuration file     |
| `kopiaprofile profiles list`               | list all profiles (post-inheritance)       |
| `kopiaprofile display [<profile>]`         | show the resolved configuration            |
| `kopiaprofile passwd <profile>`            | load and store the password in the keyring |
| `kopiaprofile generate random`             | generate a random hex secret                |
| `kopiaprofile schedule list`               | list configured schedules                   |
| `kopiaprofile schedule render`             | render schedules to crontab/systemd/launchd |
| `kopiaprofile schedule install`            | install the rendered schedule              |
| `kopiaprofile monitor status`              | show the last run's status                 |
| `kopiaprofile monitor list`                | list known status files                    |
| `kopiaprofile version`                     | print version information                  |
| `kopiaprofile completion <shell>`          | generate shell completions                 |

The `<action>` for `<profile>` is forwarded to `kopia`. Common
actions:

| Action         | Maps to kopia subcommand                       |
|----------------|-------------------------------------------------|
| `snapshot` / `snap` | `kopia snapshot create <rest>`           |
| `snapshots`    | `kopia snapshot list --all`                     |
| `restore`      | `kopia snapshot restore <root-id> <target>`     |
| `mount`        | `kopia mount all <mountpoint>`                  |
| `verify`       | `kopia snapshot verify`                         |
| `status`       | `kopia repository status --json`                |
| `prune`        | `kopia maintenance run --full`                  |
| `init`         | `kopia repository create <type>`                |
| `connect`      | `kopia repository connect <type>`               |
| `copy`         | `kopia repository sync-to <target>` (see below) |
| `check-index`  | `kopia index optimize`                          |

If the action is `snapshot create` and you don't pass any source
paths on the command line, `backup.sources` from the profile is
used.

## Examples

- [`examples/filesystem-local.yaml`](examples/filesystem-local.yaml) —
  minimal local-disk setup
- [`examples/full-demo.yaml`](examples/full-demo.yaml) — multi-profile
  setup with inheritance, groups, hooks, object-lock and templates
- [`examples/s3-object-lock.yaml`](examples/s3-object-lock.yaml) —
  S3 with COMPLIANCE/GOVERNANCE Object-Lock
- [`examples/multi-repo-copy.yaml`](examples/multi-repo-copy.yaml) —
  copying a source repository into a target on every run

## Schedules

Each profile can declare one or more cron-style schedules. They are
rendered to your platform's scheduler by `kopiaprofile schedule`:

```yaml
profiles:
  home:
    schedule:
      - name: nightly
        at: "0 3 * * *"
        action: snapshot
      - name: weekly-verify
        at: "0 6 * * 0"
        action: verify
```

```bash
# preview the crontab fragment on stdout
kopiaprofile schedule render --format=crontab

# install it (auto-detects systemd on Linux, launchd on macOS, crontab otherwise)
kopiaprofile schedule install
```

Supported formats: `crontab` (default), `systemd`, `launchd`.

The cron expression parser supports the common subset: exact values,
`*`, `*/N` and comma-separated lists. Five fields, no seconds.

## Monitoring

Each profile can declare a `monitor:` block that records the result
of every run as a JSON file and (optionally) pushes metrics to a
Prometheus push gateway:

```yaml
profiles:
  home:
    monitor:
      status-file: ~/.cache/kopiaprofile/home/status.json
      push-gateway: http://pushgateway.local:9091
      push-labels:
        job: kopiaprofile
        instance: "{{ .Hostname }}"
      timeout: 15s
```

The status file has the following shape:

```json
{
  "profile": "home",
  "action": "snapshot",
  "started_at": "2026-01-15T03:00:01Z",
  "ended_at":   "2026-01-15T03:00:42Z",
  "duration":   41000000000,
  "exit_code":  0,
  "ok":         true,
  "kopia": {
    "exit_code": 0,
    "stdout": "...",
    "stderr": ""
  },
  "hooks": [
    { "phase": "before", "command": "...", "exit_code": 0, "err": null }
  ]
}
```

The push-gateway payload (Prometheus text format) contains:

- `kopia_run{profile, action, success} 1|0`
- `kopia_run_duration_seconds{profile, action} <float>`
- `kopia_run_errors_total{profile, action, exit_code} <int>`

Inspect the recorded status with:

```bash
kopiaprofile monitor status
kopiaprofile monitor list
```

## S3 Object-Lock

Kopia supports S3 Object-Lock natively (per-blob retention) but exposes
it through maintenance settings, not via a `repository create` flag.
`kopiaprofile` surfaces it as a first-class concept:

```yaml
profiles:
  home:
    repository:
      type: s3
      bucket: my-bucket
      object-lock:
        mode: compliance            # compliance | governance | none
        retention-period: 720h      # informational
        extend-on-maintenance: true # kopia maintenance set --extend-object-locks=true
```

**The S3 bucket must be created with Object-Lock enabled and a
`DefaultRetention` configured.** `kopiaprofile` cannot do this for you
(it requires `s3:PutObjectLockConfiguration` on the bucket, which
Kopia itself doesn't expose). Example:

```bash
aws s3api create-bucket --bucket my-bucket --object-lock-enabled-for-bucket \
  --region eu-central-1 --create-bucket-configuration LocationConstraint=eu-central-1

aws s3api put-object-lock-configuration --bucket my-bucket \
  --object-lock-configuration '{
    "ObjectLockEnabled": "Enabled",
    "Rule": {
      "DefaultRetention": {
        "Mode": "COMPLIANCE",
        "Days": 30
      }
    }
  }'
```

`kopiaprofile <profile> init` then runs
`kopia maintenance set --extend-object-locks=true` so that full
maintenance extends per-blob retention automatically.

## Multi-Repository copy

The `copy:` block on a profile describes how to mirror a *source*
Kopia repository into the profile's own (target) repository. The
action is implemented via `kopia repository sync-to`:

```yaml
profiles:
  home:
    repository:
      type: s3
      bucket: target-bucket
      prefix: home
    copy:
      source:
        type: s3
        bucket: source-bucket
        prefix: legacy
        password:
          source: env
          env: KOPIA_SOURCE_PASSWORD
      allow-overwrite: true   # maps to kopia --update
      parallel: 4
```

Run it with:

```bash
kopiaprofile home copy
```

kopiaprofile will:

1. Connect the source repository using its own kopia.config (under
   `~/.cache/kopiaprofile/<profile>-src/`) and password, so the
   target's kopia.config is left untouched.
2. Run `kopia repository sync-to <target>` with the flags derived
   from the `copy:` block.

Source and target may have different passwords — both are loaded
independently from the secret pipeline.

### Caveat: kopia 0.23

`kopia repository sync-to` is a **BLOB-level mirror** (designed for
cold-storage and DR migration), not a snapshot iterator. It transfers
content BLOBs but **not** the snapshot manifests. After the sync the
target will have:

- All content BLOBs (data is preserved).
- A working `kopia content list` / `kopia blob list`.
- An **empty** `kopia snapshot list`. Restore by snapshot ID is not
  possible.

Full snapshot cross-repo replication requires a future Kopia version
that exposes `kopia snapshot copy` for cross-repository targets.

## Development

```bash
make help         # list targets
make build        # compile into ./kopiaprofile
make test         # short unit tests
make test-ci      # tests with race + coverage
make lint         # golangci-lint
make fmt          # gofmt + goimports
make tidy         # go mod tidy
make integration  # run the rustfs-backed integration test
make snapshot     # goreleaser local snapshot (no publish)
```

Tested with Go 1.25 on macOS, Linux and Windows (best-effort).

### Integration tests

`make integration` runs `testdata/integration-test.sh`, which spins
up a local [rustfs](https://github.com/rustfs/rustfs) container and
exercises the full end-to-end flow against S3 (filesystem backend,
S3 backend, list-merge, multi-repo-copy, schedules, monitor). This
test is **not** part of CI on every PR — it takes minutes, depends
on a Docker daemon that the GitHub-hosted runners don't have on
macOS, and would burn through free Actions minutes for an
end-to-end backup you can run in 20s with `make integration`
locally. The CI workflow (`test.yml`) instead runs an
[end-to-end smoke test](.github/workflows/test.yml) against a
`kopia` filesystem backend — no Docker, no network, ~5s.

To run the full integration test locally you need:

1. Docker (or any OCI runtime that understands `docker compose`).
2. `kopia` on `$PATH` (e.g. `brew install kopia` or
   `go install github.com/kopia/kopia@latest`).
3. A free port 9000 (rustfs binds there).

```bash
docker compose -f testdata/docker-compose.rustfs.yaml up -d
make build
make integration
```

## Release process

Releases are driven by [Conventional Commits][cc] (for the PR title
that goes into the release notes) and a hand-maintained
[`CHANGELOG.md`](CHANGELOG.md) (Keep-a-Changelog style).
[`goreleaser`][gr] builds and publishes the artefacts. The full
release-checklist lives in
[`docs/release-process.md`](docs/release-process.md); the short
version:

```bash
# 1. Move the "Unreleased" section of CHANGELOG.md into a new
#    "## [<version>] - <date>" block, and commit it on main.
$EDITOR CHANGELOG.md
git add CHANGELOG.md && git commit -m "docs: release 0.2.0"

# 2. Tag the release.
git tag -a 0.2.0 -m "feat: schedule renderer + monitoring"
git push origin main 0.2.0
```

The release workflow (`.github/workflows/release.yml`) will then:

1. Run `goreleaser release --clean` to build for every supported
   OS/arch combination.
2. Copy the body of the new `CHANGELOG.md` section into the GitHub
   release notes.
3. Publish tarballs, zips, `.deb`/`.rpm`/`.apk` packages, a
   `SHA256SUMS` file (signed with cosign keyless) and a CycloneDX
   SBOM.

### Commit message format

```
feat: add multi-repo copy via kopia repository sync-to
fix(schedule): handle 5-field cron with implicit day-of-week
docs: add Multi-Repo-Copy example
refactor(wrapper): split connection-flag emission per subcommand
test(profile): add unit test for hooks
ci: bump golangci-lint to v2.12.2
```

Breaking changes are denoted with `!` and a `BREAKING CHANGE:` footer.
The PR title is what lands in the release-notes header.

## License

[GPL-3.0](LICENSE) — see the `LICENSE` file for the full text.
