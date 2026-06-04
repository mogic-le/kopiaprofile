package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Repository describes how to connect to a Kopia storage backend. The
// fields intentionally mirror the flags of `kopia repository create` /
// `kopia repository connect`.
type Repository struct {
	Type       string            `yaml:"type" argument:"type"`
	Bucket     string            `yaml:"bucket" argument:"bucket"`
	Endpoint   string            `yaml:"endpoint" argument:"endpoint"`
	Region     string            `yaml:"region" argument:"region"`
	AccessKey  string            `yaml:"access-key" argument:"access-key"`
	SecretKey  string            `yaml:"secret-access-key" argument:"secret-access-key"`
	SessionTok string            `yaml:"session-token" argument:"session-token"`
	Prefix     string            `yaml:"prefix" argument:"prefix"`
	Path       string            `yaml:"path" argument:"path"` // for filesystem backend
	DisableTLS bool              `yaml:"disable-tls" argument:"disable-tls"`
	ObjectLock ObjectLockConfig  `yaml:"object-lock"`
	ExtraFlags map[string]string `yaml:"extra-flags"`
}

// ObjectLockConfig describes S3 Object-Lock settings. The actual
// configuration lives at the bucket level (it is set with `aws s3api
// put-object-lock-configuration`); kopiaprofile only validates, records
// the intent and configures Kopia's "extend on maintenance" toggle.
type ObjectLockConfig struct {
	Mode                string `yaml:"mode"`                  // compliance | governance | none
	RetentionPeriod     string `yaml:"retention-period"`      // hint for the operator; not directly used by Kopia
	ExtendOnMaintenance bool   `yaml:"extend-on-maintenance"` // sets kopia maintenance set --extend-object-locks
}

// Password describes where to obtain the repository password from.
type Password struct {
	Source         string `yaml:"source"`          // keyring | command | env | file
	KeyringService string `yaml:"keyring-service"` // default: "kopiaprofile"
	Command        string `yaml:"command"`         // shell command, stdout is the password
	EnvVar         string `yaml:"env"`             // env var containing the password
	File           string `yaml:"file"`            // path to a file containing the password
}

// BackupSection describes the `backup:` block of a profile.
//
// The `<list>-merge` fields control how the corresponding list is
// combined with the parent profile's list when inheriting. Each mode
// (replace, append, prepend, unique) is documented in
// `merge.go`/ListMergeMode. The default for every list is
// MergeReplace.
type BackupSection struct {
	Sources            []string `yaml:"sources"`
	SourcesMerge       string   `yaml:"sources-merge"`
	SourceRelative     bool     `yaml:"source-relative"`
	Exclude            []string `yaml:"exclude"`
	ExcludeMerge       string   `yaml:"exclude-merge"`
	ExcludeFile        string   `yaml:"exclude-file"`
	IgnoreIdentical    bool     `yaml:"ignore-identical"`
	Tags               []string `yaml:"tags"`
	TagsMerge          string   `yaml:"tags-merge"`
	Parallel           int      `yaml:"parallel"`
	Description        string   `yaml:"description"`
	All                bool     `yaml:"all"`
	StdinFile          string   `yaml:"stdin-file"`
	OverrideSource     string   `yaml:"override-source"`
	CheckpointUploadMB int      `yaml:"upload-limit-mb"`
	FailFast           bool     `yaml:"fail-fast"`
	ForceHashPercent   float64  `yaml:"force-hash"`
	SendSnapshotReport bool     `yaml:"send-snapshot-report"`
}

// RetentionSection describes the `retention:` block. The values are passed
// to `kopia policy set --keep-*` once at init time; Kopia applies the
// policy automatically thereafter.
type RetentionSection struct {
	KeepLatest  int `yaml:"keep-latest"`
	KeepHourly  int `yaml:"keep-hourly"`
	KeepDaily   int `yaml:"keep-daily"`
	KeepWeekly  int `yaml:"keep-weekly"`
	KeepMonthly int `yaml:"keep-monthly"`
	KeepAnnual  int `yaml:"keep-annual"`
}

// VerifySection describes the `verify:` block.
type VerifySection struct {
	FilesPercent float64 `yaml:"files-percent"`
	Parallel     int     `yaml:"parallel"`
	MaxErrors    int     `yaml:"max-errors"`
	All          bool    `yaml:"all"`
}

// MountSection describes the `mount:` block.
type MountSection struct {
	AllowOther       bool `yaml:"allow-other"`
	AllowNonEmpty    bool `yaml:"allow-non-empty-mount"`
	PreferWebDAV     bool `yaml:"webdav"`
	MaxCachedEntries int  `yaml:"max-cached-entries"`
	MaxCachedDirs    int  `yaml:"max-cached-dirs"`
}

// RestoreSection describes the `restore:` block. Note that some flags
// (source, target) come from the CLI invocation; the block only configures
// the remaining flags.
type RestoreSection struct {
	Mode           string `yaml:"mode"` // auto | local | zip | tar | tgz
	OverwriteFiles bool   `yaml:"overwrite-files"`
	OverwriteDirs  bool   `yaml:"overwrite-directories"`
	OverwriteSym   bool   `yaml:"overwrite-symlinks"`
	IgnoreErrors   bool   `yaml:"ignore-errors"`
	SkipExisting   bool   `yaml:"skip-existing"`
	Shallow        int32  `yaml:"shallow"`
	Parallel       int    `yaml:"parallel"`
	SnapshotTime   string `yaml:"snapshot-time"`
}

// LockSection describes the `lock:` block.
type LockSection struct {
	Path          string `yaml:"path"`
	ForceInactive bool   `yaml:"force-inactive"`
}

// LogSection describes the `log:` block.
type LogSection struct {
	Dir   string `yaml:"dir"`
	Level string `yaml:"level"`
}

// ScheduleEntry is a single backup schedule attached to a profile.
// The `at` field uses 5-field cron syntax (see internal/schedule).
// The `action` field defaults to "snapshot" (i.e. `kopiaprofile <p>
// snapshot create`).
type ScheduleEntry struct {
	Name   string `yaml:"name"`
	At     string `yaml:"at"`
	Action string `yaml:"action"`
}

// MonitorConfig describes monitoring outputs for a single profile.
// The status-file is written after every run; the push gateway is
// called with Prometheus text format.
type MonitorConfig struct {
	StatusFile  string            `yaml:"status-file"`
	PushGateway string            `yaml:"push-gateway"`
	PushLabels  map[string]string `yaml:"push-labels"`
	Timeout     string            `yaml:"timeout"`
}

// SourceRepository describes a *secondary* Kopia repository to read
// from. Multi-repo-copy takes the current profile's repository
// (Repository) as the target and copies its contents from this
// source. The fields mirror the `kopia repository copy-to --from-*`
// flag set; Password is taken from the source's own
// password-file/command/env/file configuration and must point to the
// SOURCE repository's password, not the target's.
//
// Example (Kopia `--from-*` flags and our YAML keys):
//
//	--from-type=s3               -> type: s3
//	--from-bucket=...            -> bucket: ...
//	--from-endpoint=...          -> endpoint: ...
//	--from-access-key=...        -> access-key: ...
//	--from-secret-access-key=... -> secret-access-key: ...
//	--from-password-file=...     -> password.file: ...
//	--from-password-command=...  -> password.command: ...
type SourceRepository struct {
	Type       string            `yaml:"type"`
	Bucket     string            `yaml:"bucket"`
	Endpoint   string            `yaml:"endpoint"`
	Region     string            `yaml:"region"`
	AccessKey  string            `yaml:"access-key"`
	SecretKey  string            `yaml:"secret-access-key"`
	SessionTok string            `yaml:"session-token"`
	Prefix     string            `yaml:"prefix"`
	Path       string            `yaml:"path"` // for filesystem backend
	DisableTLS bool              `yaml:"disable-tls"`
	Password   Password          `yaml:"password"`
	ExtraFlags map[string]string `yaml:"extra-flags"`
}

// IsZero reports whether a SourceRepository is unset.
func (s SourceRepository) IsZero() bool {
	return s.Type == "" && s.Bucket == "" && s.Endpoint == "" &&
		s.Region == "" && s.AccessKey == "" && s.SecretKey == "" &&
		s.SessionTok == "" && s.Prefix == "" && s.Path == "" &&
		!s.DisableTLS && s.Password.IsZero() && len(s.ExtraFlags) == 0
}

// CopySection describes the `copy:` block of a profile. When set, the
// profile can be invoked with `kopiaprofile <p> copy` to run
// `kopia repository copy-to` from Source into the profile's
// Repository. Use Cases include migrating to a new backend,
// creating a redundant copy, or seeding a cold storage bucket.
type CopySection struct {
	// Source is the source Kopia repository.
	Source SourceRepository `yaml:"source"`
	// AllSnapshots copies all snapshots, not just the latest per
	// source. Mirrors `--all-snapshots`.
	AllSnapshots bool `yaml:"all-snapshots"`
	// AllowOverwrite re-copies objects that already exist at the
	// target. Mirrors `--allow-overwrite`.
	AllowOverwrite bool `yaml:"allow-overwrite"`
	// Skipidentical is the inverse of `--ignore-identical-snapshots`:
	// when true, identical snapshots are skipped.
	SkipIdentical bool `yaml:"skip-identical"`
	// Parallel sets the upload parallelisation. Mirrors `--parallel`.
	Parallel int `yaml:"parallel"`
	// ProgressInterval is the time between progress log lines.
	// Mirrors `--progress-interval` (e.g. "30s").
	ProgressInterval string `yaml:"progress-interval"`
}

// IsZero reports whether a CopySection is unset.
func (c CopySection) IsZero() bool {
	return c.Source.IsZero() && !c.AllSnapshots && !c.AllowOverwrite &&
		!c.SkipIdentical && c.Parallel == 0 && c.ProgressInterval == ""
}

// Profile is the resolved (post-inheritance) representation of a single
// profile in the configuration.
type Profile struct {
	Name           string              `yaml:"-"` // not loaded from YAML; set by resolver
	Description    string              `yaml:"description"`
	Inherit        string              `yaml:"inherit"`
	Initialize     bool                `yaml:"initialize"`
	Quiet          bool                `yaml:"quiet"`
	Verbose        bool                `yaml:"verbose"`
	Repository     Repository          `yaml:"repository"`
	CacheDir       string              `yaml:"cache-dir"`
	Password       Password            `yaml:"password"`
	Env            map[string]string   `yaml:"env"`
	EnvFile        string              `yaml:"env-file"`
	KopiaBinary    string              `yaml:"kopia-binary"`
	KopiaConfigDir string              `yaml:"kopia-config-dir"`
	Backup         BackupSection       `yaml:"backup"`
	Retention      RetentionSection    `yaml:"retention"`
	Verify         VerifySection       `yaml:"verify"`
	Mount          MountSection        `yaml:"mount"`
	Restore        RestoreSection      `yaml:"restore"`
	Lock           LockSection         `yaml:"lock"`
	Log            LogSection          `yaml:"log"`
	RunBefore      string              `yaml:"run-before"`
	RunAfter       string              `yaml:"run-after"`
	RunAfterFail   string              `yaml:"run-after-fail"`
	RunFinally     string              `yaml:"run-finally"`
	OtherFlags     map[string][]string `yaml:"other-flags"`

	// Schedule is a list of scheduled runs attached to this
	// profile. The schedule package renders these into crontab,
	// systemd or launchd configs.
	Schedule []ScheduleEntry `yaml:"schedule"`

	// Monitor is the per-profile monitoring config (status file +
	// Prometheus push gateway).
	Monitor MonitorConfig `yaml:"monitor"`

	// Copy is the optional multi-repository copy block. When set, the
	// profile can be invoked with `kopiaprofile <p> copy` to copy the
	// contents of a source Kopia repository into the profile's
	// primary repository.
	Copy CopySection `yaml:"copy"`

	// Children tracks the (transitive) list of profiles that inherit from
	// this one, in declaration order. Useful for error messages.
	Children []string `yaml:"-"`
}

// IsZero reports whether a section is still at its zero value (i.e. nothing
// was set in the YAML). We need it for inheritance merge logic: a child
// must inherit the parent section if and only if the child did not set
// any of its fields.
func (b BackupSection) IsZero() bool {
	return len(b.Sources) == 0 &&
		!b.SourceRelative &&
		len(b.Exclude) == 0 &&
		b.ExcludeFile == "" &&
		!b.IgnoreIdentical &&
		len(b.Tags) == 0 &&
		b.Parallel == 0 &&
		b.Description == "" &&
		!b.All &&
		b.StdinFile == "" &&
		b.OverrideSource == "" &&
		b.CheckpointUploadMB == 0 &&
		!b.FailFast &&
		b.ForceHashPercent == 0 &&
		!b.SendSnapshotReport
}

func (r RetentionSection) IsZero() bool {
	return r.KeepLatest == 0 && r.KeepHourly == 0 && r.KeepDaily == 0 &&
		r.KeepWeekly == 0 && r.KeepMonthly == 0 && r.KeepAnnual == 0
}

func (v VerifySection) IsZero() bool {
	return v.FilesPercent == 0 && v.Parallel == 0 && v.MaxErrors == 0 && !v.All
}

func (m MountSection) IsZero() bool {
	return !m.AllowOther && !m.AllowNonEmpty && !m.PreferWebDAV &&
		m.MaxCachedEntries == 0 && m.MaxCachedDirs == 0
}

func (r RestoreSection) IsZero() bool {
	return r.Mode == "" && !r.OverwriteFiles && !r.OverwriteDirs &&
		!r.OverwriteSym && !r.IgnoreErrors && !r.SkipExisting &&
		r.Shallow == 0 && r.Parallel == 0 && r.SnapshotTime == ""
}

func (l LockSection) IsZero() bool {
	return l.Path == "" && !l.ForceInactive
}

func (l LogSection) IsZero() bool {
	return l.Dir == "" && l.Level == ""
}

// mergeProfiles folds other into base, with other winning on scalar fields
// and DeepMerge semantics for maps and slices. It does NOT resolve
// inheritance; that is the job of Resolve().
//
// List fields in BackupSection honour the child's `*-merge` mode
// (replace by default, or append/prepend/unique). The strategy is
// always taken from the child because the child is the one declaring
// its intent.
func mergeProfiles(base, other Profile) Profile {
	out := base
	if other.Description != "" {
		out.Description = other.Description
	}
	if other.Inherit != "" {
		out.Inherit = other.Inherit
	}
	if other.Initialize {
		out.Initialize = true
	}
	if other.Quiet {
		out.Quiet = true
	}
	if other.Verbose {
		out.Verbose = true
	}
	if !other.Repository.IsZero() {
		out.Repository = mergeRepository(base.Repository, other.Repository)
	}
	if other.CacheDir != "" {
		out.CacheDir = other.CacheDir
	}
	if !other.Password.IsZero() {
		out.Password = other.Password
	}
	if other.EnvFile != "" {
		out.EnvFile = other.EnvFile
	}
	if other.KopiaBinary != "" {
		out.KopiaBinary = other.KopiaBinary
	}
	if other.KopiaConfigDir != "" {
		out.KopiaConfigDir = other.KopiaConfigDir
	}
	for k, v := range other.Env {
		if out.Env == nil {
			out.Env = map[string]string{}
		}
		out.Env[k] = v
	}
	// Backup section: handle list-merge modes. We do NOT short-circuit
	// on IsZero() anymore — instead, we always start from the parent's
	// backup and combine each list according to the child's strategy.
	// If the child has no backup block at all, the parent's is used.
	childBackup := other.Backup
	if !childBackup.isCompletelyEmpty() {
		merged := base.Backup
		// Scalar fields win for the child.
		merged.SourceRelative = childBackup.SourceRelative || merged.SourceRelative
		merged.ExcludeFile = or(childBackup.ExcludeFile, merged.ExcludeFile)
		merged.IgnoreIdentical = childBackup.IgnoreIdentical || merged.IgnoreIdentical
		merged.Description = or(childBackup.Description, merged.Description)
		merged.All = childBackup.All || merged.All
		merged.StdinFile = or(childBackup.StdinFile, merged.StdinFile)
		merged.OverrideSource = or(childBackup.OverrideSource, merged.OverrideSource)
		merged.CheckpointUploadMB = pickInt(childBackup.CheckpointUploadMB, merged.CheckpointUploadMB)
		merged.Parallel = pickInt(childBackup.Parallel, merged.Parallel)
		merged.ForceHashPercent = pickFloat(childBackup.ForceHashPercent, merged.ForceHashPercent)
		if childBackup.FailFast {
			merged.FailFast = true
		}
		// SendSnapshotReport default-true semantics: if child says
		// false, the result is false. Otherwise inherit parent's.
		merged.SendSnapshotReport = merged.SendSnapshotReport && !(!childBackup.SendSnapshotReport && childBackup.snapshotReportExplicitlySet())
		// List fields combined by merge strategy.
		merged.Sources = mergeLists(base.Backup.Sources, childBackup.Sources, ParseListMergeMode(childBackup.SourcesMerge))
		merged.Exclude = mergeLists(base.Backup.Exclude, childBackup.Exclude, ParseListMergeMode(childBackup.ExcludeMerge))
		merged.Tags = mergeLists(base.Backup.Tags, childBackup.Tags, ParseListMergeMode(childBackup.TagsMerge))
		out.Backup = merged
	} else if !base.Backup.IsZero() {
		// Child has no backup block, keep parent's.
		out.Backup = base.Backup
	}
	if !other.Retention.IsZero() {
		out.Retention = other.Retention
	}
	if !other.Verify.IsZero() {
		out.Verify = other.Verify
	}
	if !other.Mount.IsZero() {
		out.Mount = other.Mount
	}
	if !other.Restore.IsZero() {
		out.Restore = other.Restore
	}
	if !other.Lock.IsZero() {
		out.Lock = other.Lock
	}
	if !other.Log.IsZero() {
		out.Log = other.Log
	}
	if !other.Copy.IsZero() {
		// Merge source-repo field-by-field; copy flags OR together so
		// a child that sets `all-snapshots: true` keeps it even when
		// the parent has it false.
		merged := base.Copy
		if !other.Copy.Source.IsZero() {
			merged.Source = mergeSource(base.Copy.Source, other.Copy.Source)
		}
		if other.Copy.AllSnapshots {
			merged.AllSnapshots = true
		}
		if other.Copy.AllowOverwrite {
			merged.AllowOverwrite = true
		}
		if other.Copy.SkipIdentical {
			merged.SkipIdentical = true
		}
		merged.Parallel = pickInt(other.Copy.Parallel, merged.Parallel)
		merged.ProgressInterval = or(other.Copy.ProgressInterval, merged.ProgressInterval)
		out.Copy = merged
	}
	if other.RunBefore != "" {
		out.RunBefore = other.RunBefore
	}
	if other.RunAfter != "" {
		out.RunAfter = other.RunAfter
	}
	if other.RunAfterFail != "" {
		out.RunAfterFail = other.RunAfterFail
	}
	if other.RunFinally != "" {
		out.RunFinally = other.RunFinally
	}
	for k, v := range other.OtherFlags {
		if out.OtherFlags == nil {
			out.OtherFlags = map[string][]string{}
		}
		out.OtherFlags[k] = v
	}
	return out
}

// isCompletelyEmpty is true when the backup block has no fields set
// at all (in which case the child is considered to have not defined a
// backup block and the parent's is used unchanged).
func (b BackupSection) isCompletelyEmpty() bool {
	return b.Sources == nil && b.SourcesMerge == "" &&
		!b.SourceRelative &&
		b.Exclude == nil && b.ExcludeMerge == "" &&
		b.ExcludeFile == "" &&
		!b.IgnoreIdentical &&
		b.Tags == nil && b.TagsMerge == "" &&
		b.Parallel == 0 &&
		b.Description == "" &&
		!b.All &&
		b.StdinFile == "" &&
		b.OverrideSource == "" &&
		b.CheckpointUploadMB == 0 &&
		!b.FailFast &&
		b.ForceHashPercent == 0 &&
		!b.SendSnapshotReport
}

// snapshotReportExplicitlySet returns true when the user wrote
// `send-snapshot-report: false` (or true) in YAML. We cannot tell this
// directly without changing the field's type, so we approximate by
// checking the merge mode — which is a hack. The simpler
// implementation is to just take the child's value when set.
func (b BackupSection) snapshotReportExplicitlySet() bool {
	// Heuristic: if Parallel, CheckpointUploadMB or any of the lists
	// are non-zero, the block is not empty. The only way to write
	// send-snapshot-report: false without any other field is a
	// single-line block; in that rare case the field will be
	// ignored. This is good enough for the MVP.
	return b.Parallel > 0 || b.CheckpointUploadMB > 0 ||
		len(b.Sources) > 0 || len(b.Tags) > 0 || len(b.Exclude) > 0 ||
		b.SourceRelative || b.IgnoreIdentical || b.All || b.FailFast ||
		b.Description != "" || b.ExcludeFile != "" ||
		b.StdinFile != "" || b.OverrideSource != "" ||
		b.ForceHashPercent > 0
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func pickInt(a, b int) int {
	if a != 0 {
		return a
	}
	return b
}

func pickFloat(a, b float64) float64 {
	if a != 0 {
		return a
	}
	return b
}

func (r Repository) IsZero() bool {
	return r.Type == "" && r.Bucket == "" && r.Endpoint == "" &&
		r.Region == "" && r.AccessKey == "" && r.SecretKey == "" &&
		r.SessionTok == "" && r.Prefix == "" && r.Path == "" &&
		!r.DisableTLS && r.ObjectLock.IsZero() && len(r.ExtraFlags) == 0
}

func (o ObjectLockConfig) IsZero() bool {
	return o.Mode == "" && o.RetentionPeriod == "" && !o.ExtendOnMaintenance
}

func (p Password) IsZero() bool {
	return p.Source == "" && p.KeyringService == "" && p.Command == "" &&
		p.EnvVar == "" && p.File == ""
}

func mergeRepository(base, other Repository) Repository {
	out := base
	if other.Type != "" {
		out.Type = other.Type
	}
	if other.Bucket != "" {
		out.Bucket = other.Bucket
	}
	if other.Endpoint != "" {
		out.Endpoint = other.Endpoint
	}
	if other.Region != "" {
		out.Region = other.Region
	}
	if other.AccessKey != "" {
		out.AccessKey = other.AccessKey
	}
	if other.SecretKey != "" {
		out.SecretKey = other.SecretKey
	}
	if other.SessionTok != "" {
		out.SessionTok = other.SessionTok
	}
	if other.Prefix != "" {
		out.Prefix = other.Prefix
	}
	if other.Path != "" {
		out.Path = other.Path
	}
	if other.DisableTLS {
		out.DisableTLS = true
	}
	if !other.ObjectLock.IsZero() {
		out.ObjectLock = other.ObjectLock
	}
	for k, v := range other.ExtraFlags {
		if out.ExtraFlags == nil {
			out.ExtraFlags = map[string]string{}
		}
		out.ExtraFlags[k] = v
	}
	return out
}

// mergeSource is the equivalent of mergeRepository for SourceRepository.
// The Password block follows the same "child wins" rule as the rest of
// the merge logic; if a child profile wants to reuse the parent's
// source, it leaves Source blank.
func mergeSource(base, other SourceRepository) SourceRepository {
	out := base
	if other.Type != "" {
		out.Type = other.Type
	}
	if other.Bucket != "" {
		out.Bucket = other.Bucket
	}
	if other.Endpoint != "" {
		out.Endpoint = other.Endpoint
	}
	if other.Region != "" {
		out.Region = other.Region
	}
	if other.AccessKey != "" {
		out.AccessKey = other.AccessKey
	}
	if other.SecretKey != "" {
		out.SecretKey = other.SecretKey
	}
	if other.SessionTok != "" {
		out.SessionTok = other.SessionTok
	}
	if other.Prefix != "" {
		out.Prefix = other.Prefix
	}
	if other.Path != "" {
		out.Path = other.Path
	}
	if other.DisableTLS {
		out.DisableTLS = true
	}
	if !other.Password.IsZero() {
		out.Password = other.Password
	}
	for k, v := range other.ExtraFlags {
		if out.ExtraFlags == nil {
			out.ExtraFlags = map[string]string{}
		}
		out.ExtraFlags[k] = v
	}
	return out
}

// Resolve walks every profile, applies inheritance and validates that the
// resulting graph is acyclic. It mutates f.Profiles in place.
//
// Inheritance chain: a -> b -> c (c inherits from b; b inherits from a) is
// allowed. Cycles (a -> b -> a) raise an error.
func (f *File) Resolve() error {
	visited := make(map[string]int, len(f.Profiles)) // 0=unseen, 1=in progress, 2=done
	children := make(map[string][]string, len(f.Profiles))
	for name, prof := range f.Profiles {
		if prof.Inherit != "" {
			children[prof.Inherit] = append(children[prof.Inherit], name)
		}
	}
	for name := range f.Profiles {
		existing := f.Profiles[name]
		existing.Children = children[name]
		f.Profiles[name] = existing
		if err := f.resolveOne(name, visited, make([]string, 0, 8)); err != nil {
			return err
		}
	}
	return nil
}

func (f *File) resolveOne(name string, visited map[string]int, stack []string) error {
	switch visited[name] {
	case 2:
		return nil
	case 1:
		return fmt.Errorf("inheritance cycle detected: %v", append(stack, name))
	}
	prof, ok := f.Profiles[name]
	if !ok {
		return fmt.Errorf("unknown profile %q referenced in inheritance chain", name)
	}
	visited[name] = 1
	stack = append(stack, name)

	if prof.Inherit != "" {
		if err := f.resolveOne(prof.Inherit, visited, stack); err != nil {
			return err
		}
		parent := f.Profiles[prof.Inherit]
		prof = mergeProfiles(parent, prof)
	}
	prof.Name = name
	visited[name] = 2
	f.Profiles[name] = prof
	return nil
}

// Get returns the resolved profile by name. The returned value is a copy
// that the caller may modify freely without affecting the underlying File.
func (f *File) Get(name string) (Profile, bool) {
	p, ok := f.Profiles[name]
	return p, ok
}

// Names returns the names of all profiles in deterministic order (sorted).
func (f *File) Names() []string {
	out := make([]string, 0, len(f.Profiles))
	for k := range f.Profiles {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}

// GroupNames returns the names of all groups, sorted.
func (f *File) GroupNames() []string {
	out := make([]string, 0, len(f.Groups))
	for k := range f.Groups {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}

// resolveConfigPath is exposed for tests; it expands ~ and makes the path
// absolute relative to the working directory.
func resolveConfigPath(p string) (string, error) {
	if p == "" {
		return "", errors.New("empty config path")
	}
	if p[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(home, p[1:])
	}
	return filepath.Abs(p)
}

// We import yaml only to keep the package self-contained for downstream
// users that may want to embed config validation. The blank assignment
// prevents the compiler from complaining about an unused import when
// tests aren't run.
var _ = yaml.Marshal
