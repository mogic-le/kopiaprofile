# Configuration reference

This document describes every key in the `kopiaprofile` configuration
file. The schema is the same for YAML, TOML, HCL and JSON — only the
syntax differs.

The format is auto-detected by file extension. If you want to
override, the first non-comment line of the file can be `# format:
<yaml|toml|hcl|json>` and `kopiaprofile` will honour it.

A configuration file is a single document with the following
top-level keys:

| Key         | Type              | Required | Description                       |
|-------------|-------------------|----------|-----------------------------------|
| `version`   | string            | no       | Schema version; default `"1"`     |
| `global`    | object            | no       | Defaults applied to every profile |
| `profiles`  | map[string]object | yes      | Named profiles                    |
| `groups`    | map[string]object | no       | Bundles of profiles (not yet runnable; informational) |
| `includes`  | []string          | no       | Other files to merge in           |

Inheritance is set per-profile via `inherit: <other-profile-name>`.
Cycles are detected and rejected.

## `global` block

Defaults for every profile. Every field of a profile can be
overridden in the profile itself.

```yaml
global:
  kopia-binary: /usr/local/bin/kopia
  kopia-config-dir: ~/.config/kopia
  initialize: true
  log-level: info
  force-inactive-lock: false
  stale-lock-age: 30m
  lock-retry-after: 5s
  quiet: false
  env:
    KOPIA_LOG_DIR: /var/log/kopia
```

## `profiles.<name>` block

The complete schema of a profile:

```yaml
profiles:
  home:
    # ---------- meta ----------
    description: "Daily backup of /home/alice"   # string
    inherit: base                                 # string | null
    initialize: true                              # bool
    quiet: false                                  # bool
    verbose: false                                # bool

    # ---------- repository ----------
    repository:
      type: s3                                    # string
      bucket: my-bucket                           # string
      endpoint: s3.amazonaws.com                  # string
      region: eu-central-1                        # string
      access-key: AKIA...                         # string
      secret-access-key: "..."                    # string
      session-token: "..."                        # string
      prefix: backups                             # string
      path: /var/lib/kopia                        # string (filesystem)
      disable-tls: false                          # bool
      object-lock:
        mode: compliance                          # compliance | governance | none
        retention-period: 720h                    # string (informational)
        extend-on-maintenance: true               # bool
      extra-flags:                                # map[string]string
        enable-cache: ""

    # ---------- cache & kopia-config ----------
    cache-dir: ~/.cache/kopia
    kopia-config-dir: ~/.config/kopia
    kopia-binary: /usr/local/bin/kopia

    # ---------- password ----------
    password:
      source: keyring                             # keyring | command | env | file
      keyring-service: kopiaprofile
      command: "pass kopia/home"
      env: KOPIA_HOME_PASSWORD
      file: /root/.secrets/kopia-home

    # ---------- environment ----------
    env:                                           # map[string]string
      BACKUP_TAG: "home"
    env-file: /etc/kopiaprofile.env

    # ---------- backup section ----------
    backup:
      sources: [ /home/alice ]                    # []string
      sources-merge: append                       # replace | append | prepend | unique
      source-relative: false                      # bool
      exclude: [ "*.tmp" ]                        # []string
      exclude-merge: replace                      # replace | append | prepend | unique
      exclude-file: /etc/kopiaprofile/exclude     # string
      ignore-identical: false                     # bool
      tags: [ "env:prod" ]                        # []string
      tags-merge: replace                         # replace | append | prepend | unique
      parallel: 8                                 # int
      description: "daily home backup"            # string
      all: false                                  # bool
      stdin-file: /dev/null                       # string
      override-source: hostname                   # string
      upload-limit-mb: 1024                       # int
      fail-fast: false                            # bool
      force-hash: 0                               # float (0-100)
      send-snapshot-report: true                  # bool

    # ---------- retention ----------
    retention:
      keep-latest: 5
      keep-hourly: 24
      keep-daily: 7
      keep-weekly: 4
      keep-monthly: 12
      keep-annual: 3

    # ---------- verify ----------
    verify:
      files-percent: 5.0
      parallel: 4
      max-errors: 10
      all: false

    # ---------- mount ----------
    mount:
      allow-other: true
      allow-non-empty-mount: false
      prefer-webdav: false
      max-cached-entries: 100000
      max-cached-dirs: 1000

    # ---------- restore ----------
    restore:
      mode: tar
      overwrite-files: false
      overwrite-directories: false
      overwrite-symlinks: false
      ignore-errors: false
      skip-existing: false
      shallow: 0
      parallel: 4
      snapshot-time: "2026-01-15T03:00:00Z"

    # ---------- lock ----------
    lock:
      path: /var/lock/kopiaprofile-home.lock
      force-inactive: false

    # ---------- log ----------
    log:
      dir: /var/log/kopiaprofile
      level: info

    # ---------- hooks ----------
    run-before: "/usr/local/bin/pre-backup.sh"        # string
    run-after:  "/usr/local/bin/post-backup.sh"        # string
    run-after-fail: "/usr/local/bin/notify-failure.sh" # string
    run-finally: "/usr/local/bin/always.sh"           # string

    # ---------- free-form flags ----------
    other-flags:                                # map[string][]string
      verbose: []                               # bare flag `--verbose`
      log-level: [ "info" ]                     # `--log-level=info`

    # ---------- schedule ----------
    schedule:
      - name: nightly
        at: "0 3 * * *"
        action: snapshot
      - name: weekly-verify
        at: "0 6 * * 0"
        action: verify

    # ---------- monitoring ----------
    monitor:
      status-file: ~/.cache/kopiaprofile/home/status.json
      push-gateway: http://pushgateway.local:9091
      push-labels:
        job: kopiaprofile
        instance: "{{ .Hostname }}"
      timeout: 15s

    # ---------- multi-repository copy ----------
    copy:
      source:
        type: s3
        bucket: source-bucket
        prefix: legacy
        password:
          source: env
          env: KOPIA_SOURCE_PASSWORD
      allow-overwrite: true
      parallel: 4
      progress-interval: 30s
```

### Inheritance semantics

A child profile that declares `inherit: <parent>` first receives a
deep copy of the parent's resolved profile and then applies its own
values. Scalars in the child override the parent. Lists follow the
`<list>-merge` strategy (see below). Maps are merged recursively.

`inherit` is resolved transitively (`a → b → c`). Cycles raise an
error at config load.

### List-merge semantics

For every list field `foo`, the child may declare a companion field
`foo-merge` that controls how the lists are combined:

| `foo-merge`  | Result                              |
|--------------|-------------------------------------|
| `replace`    | child replaces parent (default)     |
| `append`     | parent ++ child                     |
| `prepend`    | child ++ parent                     |
| `unique`     | union, preserving order, deduped    |

Currently supported for `backup.sources`, `backup.exclude` and
`backup.tags`. The list-merge field is itself inherited: if a
grandchild doesn't set it explicitly, the child's value is used.

## Environment overrides

Every configuration key can be overridden via an environment variable
of the form `KOPIAPROFILE_<PROFILE>_<PATH>`. For example:

```bash
KOPIAPROFILE_HOME_REPOSITORY_BUCKET=other-bucket \
  kopiaprofile home snapshot create
```

overrides `profiles.home.repository.bucket` for that single run.

## File inclusion

`includes` is a list of paths (relative to the parent file's
directory) that are merged into the current configuration. Useful
for keeping secrets in a separate file that isn't checked into
version control:

```yaml
# kopiaprofile.yaml
includes:
  - kopiaprofile.secrets.yaml
profiles:
  home:
    repository:
      type: s3
      bucket: my-bucket
```

```yaml
# kopiaprofile.secrets.yaml
profiles:
  home:
    repository:
      access-key: "AKIA-..."
      secret-access-key: "..."
    password:
      source: env
      env: KOPIA_HOME_PASSWORD
```

Includes are applied in order. If the same key is set in multiple
files, the later include wins.

## Generated skeleton

`kopiaprofile init <file>` produces a heavily-commented starter
config. Use `--format` to choose the output format
(`yaml` / `toml` / `hcl` / `json`, default = inferred from the file
extension). Use `--force` to overwrite an existing file.
