package wrapper

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mogic-le/kopiaprofile/internal/config"
)

// FullMaintenanceMode is the profile's object-lock.full-maintenance
// setting.
type FullMaintenanceMode string

const (
	// FullMaintenanceAuto lets kopiaprofile decide per run: full
	// maintenance stays disabled for as long as the repository's own
	// retention makes reclaiming impossible, and is enabled once the
	// oldest blob can expire. This is the default.
	FullMaintenanceAuto FullMaintenanceMode = "auto"
	// FullMaintenanceAlways enables full maintenance unconditionally,
	// which is the stock kopia behaviour.
	FullMaintenanceAlways FullMaintenanceMode = "always"
	// FullMaintenanceNever disables full maintenance unconditionally.
	FullMaintenanceNever FullMaintenanceMode = "never"
)

// ParseFullMaintenanceMode turns a profile's object-lock.full-maintenance
// into a mode, defaulting to auto when unset. A malformed value is an
// error rather than a silent fallback, because the difference between
// the modes decides whether a repository ever reclaims space.
func ParseFullMaintenanceMode(raw string) (FullMaintenanceMode, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return FullMaintenanceAuto, nil
	case string(FullMaintenanceAuto):
		return FullMaintenanceAuto, nil
	case string(FullMaintenanceAlways):
		return FullMaintenanceAlways, nil
	case string(FullMaintenanceNever):
		return FullMaintenanceNever, nil
	default:
		return "", fmt.Errorf("object-lock: invalid full-maintenance %q (expected auto|always|never)", raw)
	}
}

// formatBlobPrefix is the repository format blob. It is written when the
// repository is created and never rewritten, so no blob in the
// repository can be older than this one - which makes it the cheapest
// exact lower bound for "how old is the oldest object here", one request
// instead of a listing over every pack blob.
const formatBlobPrefix = "kopia.repository"

// BuildFormatBlobListArgs returns the
//
//	kopia blob list --prefix=kopia.repository --json
//
// argv used to read the repository's age.
func BuildFormatBlobListArgs() []string {
	return []string{"blob", "list", "--prefix=" + formatBlobPrefix, "--json"}
}

// BuildFullMaintenanceArgs returns the
//
//	kopia maintenance set --enable-full=<bool>
//
// argv. The value is always written explicitly, in both directions, for
// the same reason BuildObjectLockMaintenanceArgs does it: the profile
// stays the single source of truth and a repository never keeps a stale
// setting from an earlier run.
func BuildFullMaintenanceArgs(enable bool) []string {
	if enable {
		return []string{"maintenance", "set", "--enable-full=true"}
	}

	return []string{"maintenance", "set", "--enable-full=false"}
}

// blobMetadata mirrors the fields of kopia's blob.Metadata that the
// `blob list --json` output carries.
type blobMetadata struct {
	ID        string    `json:"id"`
	Length    int64     `json:"length"`
	Timestamp time.Time `json:"timestamp"`
}

// OldestBlobTimestamp extracts the oldest timestamp from a
// `kopia blob list --json` output.
//
// The output is a JSON array, but kopia is free to print notices around
// it, so the array is located by its brackets rather than by assuming
// the whole stream is JSON. An empty array is an error: "no blobs" is
// not a safe basis for a maintenance decision.
func OldestBlobTimestamp(out []byte) (time.Time, error) {
	start := bytes.IndexByte(out, '[')
	end := bytes.LastIndexByte(out, ']')
	if start < 0 || end < start {
		return time.Time{}, fmt.Errorf("no JSON array in blob list output")
	}

	var blobs []blobMetadata
	if err := json.Unmarshal(out[start:end+1], &blobs); err != nil {
		return time.Time{}, fmt.Errorf("parsing blob list output: %w", err)
	}

	oldest := time.Time{}
	for _, b := range blobs {
		if b.Timestamp.IsZero() {
			continue
		}
		if oldest.IsZero() || b.Timestamp.Before(oldest) {
			oldest = b.Timestamp
		}
	}

	if oldest.IsZero() {
		return time.Time{}, fmt.Errorf("blob list output contains no timestamps")
	}

	return oldest, nil
}

// ReclaimStartsAt returns the earliest wall-clock time at which any blob
// in a retention-locked repository can become deletable: the oldest blob
// plus the configured retention period.
//
// It is deliberately a lower bound and not an exact prediction. Blobs
// written later expire later, and a retention period that was raised
// after the fact only ever moves the real date further out, so acting on
// this bound can delay reclaiming but never delete something early.
func ReclaimStartsAt(oldest time.Time, retentionPeriod string) (time.Time, error) {
	d, err := time.ParseDuration(strings.TrimSpace(retentionPeriod))
	if err != nil {
		return time.Time{}, fmt.Errorf("object-lock: invalid retention-period %q: %w", retentionPeriod, err)
	}
	if d <= 0 {
		return time.Time{}, fmt.Errorf("object-lock: retention-period %q is not positive", retentionPeriod)
	}

	return oldest.Add(d), nil
}

// FullMaintenanceGated reports whether a profile is a candidate for the
// gate at all: it needs an object-lock block with an actual retention
// mode and a retention period. Without those, kopiaprofile has no
// business touching the repository's maintenance parameters, exactly as
// with the extend-object-locks setting.
func FullMaintenanceGated(p config.Profile) bool {
	ol := p.Repository.ObjectLock
	if ol.IsZero() {
		return false
	}

	switch strings.ToLower(strings.TrimSpace(ol.Mode)) {
	case "compliance", "governance":
	default:
		return false
	}

	return strings.TrimSpace(ol.RetentionPeriod) != ""
}
