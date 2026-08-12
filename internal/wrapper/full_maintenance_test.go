package wrapper

import (
	"strings"
	"testing"
	"time"

	"github.com/mogic-le/kopiaprofile/internal/config"
)

func fullMaintenanceProfile(mode, period, fullMaintenance string) config.Profile {
	return config.Profile{
		Repository: config.Repository{
			Type: "s3",
			ObjectLock: config.ObjectLockConfig{
				Mode:            mode,
				RetentionPeriod: period,
				FullMaintenance: fullMaintenance,
			},
		},
	}
}

// An unset value has to mean auto, otherwise every existing profile would
// silently change behaviour on upgrade.
func TestParseFullMaintenanceModeDefaultsToAuto(t *testing.T) {
	got, err := ParseFullMaintenanceMode("")
	if err != nil {
		t.Fatalf("ParseFullMaintenanceMode: %v", err)
	}
	if got != FullMaintenanceAuto {
		t.Errorf("got %q, want %q", got, FullMaintenanceAuto)
	}
}

func TestParseFullMaintenanceModeValues(t *testing.T) {
	cases := map[string]FullMaintenanceMode{
		"auto":    FullMaintenanceAuto,
		"always":  FullMaintenanceAlways,
		"never":   FullMaintenanceNever,
		" Always": FullMaintenanceAlways,
		"NEVER":   FullMaintenanceNever,
	}
	for in, want := range cases {
		got, err := ParseFullMaintenanceMode(in)
		if err != nil {
			t.Errorf("ParseFullMaintenanceMode(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseFullMaintenanceMode(%q) = %q, want %q", in, got, want)
		}
	}
}

// A typo must not quietly fall back to a mode the operator did not ask
// for: the difference decides whether a repository ever reclaims space.
func TestParseFullMaintenanceModeRejectsUnknown(t *testing.T) {
	if _, err := ParseFullMaintenanceMode("sometimes"); err == nil {
		t.Errorf("expected an error for an unknown mode")
	}
}

func TestBuildFullMaintenanceArgsBothDirections(t *testing.T) {
	if joined := strings.Join(BuildFullMaintenanceArgs(true), " "); joined != "maintenance set --enable-full=true" {
		t.Errorf("enable: got %q", joined)
	}
	if joined := strings.Join(BuildFullMaintenanceArgs(false), " "); joined != "maintenance set --enable-full=false" {
		t.Errorf("disable: got %q", joined)
	}
}

func TestBuildFormatBlobListArgs(t *testing.T) {
	want := "blob list --prefix=kopia.repository --json"
	if joined := strings.Join(BuildFormatBlobListArgs(), " "); joined != want {
		t.Errorf("got %q, want %q", joined, want)
	}
}

func TestOldestBlobTimestampPicksTheEarliest(t *testing.T) {
	out := []byte(`[
 {"id":"kopia.repository","length":661,"timestamp":"2026-01-02T03:04:05Z"},
 {"id":"kopia.repository.f","length":661,"timestamp":"2026-05-06T07:08:09Z"}
]`)

	got, err := OldestBlobTimestamp(out)
	if err != nil {
		t.Fatalf("OldestBlobTimestamp: %v", err)
	}

	want := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// kopia is free to print notices around its JSON, so the array is located
// by its brackets instead of assuming the whole stream parses.
func TestOldestBlobTimestampIgnoresSurroundingNoise(t *testing.T) {
	out := []byte("NOTICE: something to say\n[\n {\"id\":\"kopia.repository\",\"length\":661,\"timestamp\":\"2026-01-02T03:04:05Z\"}\n]\ntrailing chatter\n")

	got, err := OldestBlobTimestamp(out)
	if err != nil {
		t.Fatalf("OldestBlobTimestamp: %v", err)
	}
	if want := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC); !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// "no blobs" is not a safe basis for a maintenance decision - the caller
// has to leave the repository alone rather than guess.
func TestOldestBlobTimestampEmptyListIsAnError(t *testing.T) {
	if _, err := OldestBlobTimestamp([]byte("[]")); err == nil {
		t.Errorf("expected an error for an empty list")
	}
}

func TestOldestBlobTimestampGarbageIsAnError(t *testing.T) {
	for _, in := range []string{"", "not json at all", "[{"} {
		if _, err := OldestBlobTimestamp([]byte(in)); err == nil {
			t.Errorf("expected an error for %q", in)
		}
	}
}

func TestReclaimStartsAt(t *testing.T) {
	oldest := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	got, err := ReclaimStartsAt(oldest, "720h")
	if err != nil {
		t.Fatalf("ReclaimStartsAt: %v", err)
	}
	if want := oldest.Add(720 * time.Hour); !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestReclaimStartsAtRejectsBadPeriods(t *testing.T) {
	oldest := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	for _, in := range []string{"", "30 days", "0h", "-720h"} {
		if _, err := ReclaimStartsAt(oldest, in); err == nil {
			t.Errorf("expected an error for retention-period %q", in)
		}
	}
}

// Without an object-lock block, or with a mode that means "no lock",
// kopiaprofile has no business touching the repository's maintenance
// parameters - same rule as for the extend-object-locks setting.
func TestFullMaintenanceGated(t *testing.T) {
	cases := []struct {
		name string
		prof config.Profile
		want bool
	}{
		{"no object-lock block", config.Profile{}, false},
		{"mode none", fullMaintenanceProfile("none", "720h", ""), false},
		{"no retention period", fullMaintenanceProfile("compliance", "", ""), false},
		{"compliance", fullMaintenanceProfile("compliance", "720h", ""), true},
		{"governance", fullMaintenanceProfile("governance", "720h", ""), true},
	}

	for _, tc := range cases {
		if got := FullMaintenanceGated(tc.prof); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
