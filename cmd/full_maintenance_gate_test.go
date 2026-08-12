package cmd

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/mogic-le/kopiaprofile/internal/config"
)

func gateTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func gateTestProfile(mode, period, fullMaintenance string) config.Profile {
	return config.Profile{
		Name: "example-host",
		// A binary that does not exist: any test that reaches the probe
		// would fail loudly instead of silently shelling out.
		KopiaBinary: "/nonexistent/kopia",
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

// A profile without an object-lock block must not have its maintenance
// parameters touched at all.
func TestFullMaintenanceGateArgsUngatedProfile(t *testing.T) {
	got := fullMaintenanceGateArgs(context.Background(), gateTestLogger(), config.Profile{}, "pw")
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

// always/never are answered from the profile alone; neither may pay for
// a repository round-trip.
func TestFullMaintenanceGateArgsExplicitModesSkipTheProbe(t *testing.T) {
	cases := map[string]string{
		"always": "maintenance set --enable-full=true",
		"never":  "maintenance set --enable-full=false",
	}

	for mode, want := range cases {
		got := fullMaintenanceGateArgs(context.Background(), gateTestLogger(),
			gateTestProfile("compliance", "720h", mode), "pw")
		if joined := strings.Join(got, " "); joined != want {
			t.Errorf("%s: got %q, want %q", mode, joined, want)
		}
	}
}

// An undecidable gate leaves the repository as it is. Disabling
// reclaiming on a failed probe would be the one outcome nobody could see
// from the outside.
func TestFullMaintenanceGateArgsUndecidableLeavesRepositoryAlone(t *testing.T) {
	// Invalid mode value, and a probe that cannot run either way.
	if got := fullMaintenanceGateArgs(context.Background(), gateTestLogger(),
		gateTestProfile("compliance", "720h", "sometimes"), "pw"); got != nil {
		t.Errorf("invalid full-maintenance: expected nil, got %v", got)
	}

	if got := fullMaintenanceGateArgs(context.Background(), gateTestLogger(),
		gateTestProfile("compliance", "720h", "auto"), "pw"); got != nil {
		t.Errorf("failed probe: expected nil, got %v", got)
	}
}
