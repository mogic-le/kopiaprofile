package wrapper

import (
	"fmt"
	"strings"

	"github.com/mogic-le/kopiaprofile/internal/config"
)

// ObjectLockAction describes what to do with the bucket's object-lock
// configuration.
type ObjectLockAction struct {
	// Mode is "compliance", "governance" or "none".
	Mode string
	// RetentionPeriod is a Go duration string (e.g. "720h"). It is
	// informational only; Kopia does not expose a CLI flag to set the
	// bucket-level retention period directly. The operator must
	// configure the bucket out-of-band (e.g. via `aws s3api
	// put-object-lock-configuration`).
	RetentionPeriod string
	// ExtendOnMaintenance, if true, causes kopiaprofile to set
	// `kopia maintenance set --extend-object-locks=true` so that full
	// maintenance runs extend the per-blob retention window.
	ExtendOnMaintenance bool
}

// Validate returns an error if the configuration is invalid.
func (a ObjectLockAction) Validate() error {
	switch strings.ToLower(a.Mode) {
	case "", "compliance", "governance":
		// ok
	case "none":
		// ok - means the operator does not want object lock
	default:
		return fmt.Errorf("object-lock: invalid mode %q (expected compliance|governance|none)", a.Mode)
	}
	return nil
}

// BuildObjectLockMaintenanceArgs returns the
//
//	kopia maintenance set --extend-object-locks=<bool>
//
// argv that makes the repository's maintenance parameters match the
// profile's object-lock.extend-on-maintenance setting, or nil when the
// profile has no object-lock block at all (then kopiaprofile has no
// business touching the repository's maintenance parameters).
//
// The value is always written explicitly, in both directions, so the
// YAML is the single source of truth: flipping the profile from true to
// false actually disables extension on the next run instead of leaving a
// stale "enabled" behind on the repository.
//
// This used to be a standalone ApplyObjectLockMaintenance function that
// nothing ever called, which meant every profile could claim
// extend-on-maintenance: true while the repository had extension
// disabled - confirmed live on a whole fleet, every host reporting
// "Object Lock Extension: disabled". Expressing it as a pre-command
// instead puts it on the same path as the policy pre-commands, where it
// runs against the already-connected repository and cannot silently
// become dead code again.
func BuildObjectLockMaintenanceArgs(p config.Profile) []string {
	if p.Repository.ObjectLock.IsZero() {
		return nil
	}

	if p.Repository.ObjectLock.ExtendOnMaintenance {
		return []string{"maintenance", "set", "--extend-object-locks=true"}
	}

	return []string{"maintenance", "set", "--extend-object-locks=false"}
}
