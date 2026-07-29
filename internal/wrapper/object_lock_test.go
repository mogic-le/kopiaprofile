package wrapper

import (
	"strings"
	"testing"

	"github.com/mogic-le/kopiaprofile/internal/config"
)

func objectLockProfile(mode, period string, extend bool) config.Profile {
	return config.Profile{
		Repository: config.Repository{
			Type: "s3",
			ObjectLock: config.ObjectLockConfig{
				Mode:                mode,
				RetentionPeriod:     period,
				ExtendOnMaintenance: extend,
			},
		},
	}
}

// The predecessor of this builder was a standalone function that nothing ever
// called, so `extend-on-maintenance: true` in a profile had no effect at all -
// confirmed live on a whole fleet, every repository reporting "Object Lock
// Extension: disabled" while every profile claimed it was on. The builder
// exists so the setting is expressed on the same path as the other
// pre-commands and cannot quietly become dead code again.
func TestBuildObjectLockMaintenanceArgsEnabled(t *testing.T) {
	got := BuildObjectLockMaintenanceArgs(objectLockProfile("compliance", "720h", true))

	want := "maintenance set --extend-object-locks=true"
	if joined := strings.Join(got, " "); joined != want {
		t.Errorf("got %q, want %q", joined, want)
	}
}

// The value is written in both directions on purpose: flipping the profile
// back to false has to actually disable extension on the next run instead of
// leaving a stale "enabled" behind on the repository. That matters because
// extension is not safe to leave on in every kopia version - it can renew the
// retention of pack blobs that garbage collection wants to reclaim.
func TestBuildObjectLockMaintenanceArgsDisabledIsStillWritten(t *testing.T) {
	got := BuildObjectLockMaintenanceArgs(objectLockProfile("compliance", "720h", false))

	want := "maintenance set --extend-object-locks=false"
	if joined := strings.Join(got, " "); joined != want {
		t.Errorf("got %q, want %q", joined, want)
	}
}

// Without an object-lock block kopiaprofile has no business touching the
// repository's maintenance parameters at all.
func TestBuildObjectLockMaintenanceArgsNoObjectLockBlock(t *testing.T) {
	if got := BuildObjectLockMaintenanceArgs(config.Profile{}); got != nil {
		t.Errorf("expected nil for a profile without an object-lock block, got %v", got)
	}
}
