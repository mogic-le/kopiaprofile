package cmd

import (
	"context"
	"io"
	"log/slog"
	"time"

	"github.com/mogic-le/kopiaprofile/internal/config"
	"github.com/mogic-le/kopiaprofile/internal/wrapper"
)

// fullMaintenanceProbeTimeout caps the blob listing that reads the
// repository's age. It lists a single blob prefix, so it is a handful of
// requests; a run must never hang here.
const fullMaintenanceProbeTimeout = 2 * time.Minute

// fullMaintenanceGateArgs returns the
// `maintenance set --enable-full=<bool>` pre-command for this profile,
// or nil when the profile is not gated or the decision cannot be made.
//
// Why the gate exists: under a retention lock, full maintenance walks
// the whole repository, finds unreferenced data and then deletes none of
// it, because every candidate blob is still locked. Observed live on a
// multi-terabyte repository: snapshot garbage collection alone grew from
// five to over twelve hours within a week, one run reported "Found
// 2167(18.1 GB) unreferenced pack blobs to delete and deleted 0(0 B)",
// and the run eventually outlived its own timeout and was killed - which
// then blocked the next scheduled run on the profile lock. None of that
// work could have succeeded: the repository was younger than its own
// retention period, so nothing in it was deletable yet.
//
// The gate therefore keeps full maintenance off while that is provably
// the case and turns it on by itself once the oldest blob can expire.
// Quick maintenance is untouched and keeps epochs, indexes and logs in
// shape in the meantime.
//
// A failure to decide never disables anything: an unreadable repository
// age leaves the maintenance parameters exactly as they are, because
// silently switching off reclaiming is worse than a slow run.
func fullMaintenanceGateArgs(ctx context.Context, log *slog.Logger, p config.Profile, password string) []string {
	if !wrapper.FullMaintenanceGated(p) {
		return nil
	}

	mode, err := wrapper.ParseFullMaintenanceMode(p.Repository.ObjectLock.FullMaintenance)
	if err != nil {
		log.Warn("leaving full maintenance untouched", "profile", p.Name, "err", err)
		return nil
	}

	switch mode {
	case wrapper.FullMaintenanceAlways:
		return wrapper.BuildFullMaintenanceArgs(true)
	case wrapper.FullMaintenanceNever:
		return wrapper.BuildFullMaintenanceArgs(false)
	case wrapper.FullMaintenanceAuto:
		// handled below
	}

	started, err := probeRepositoryStart(ctx, p, password)
	if err != nil {
		log.Warn("cannot read repository age, leaving full maintenance untouched",
			"profile", p.Name, "err", err)

		return nil
	}

	reclaimFrom, err := wrapper.ReclaimStartsAt(started, p.Repository.ObjectLock.RetentionPeriod)
	if err != nil {
		log.Warn("cannot compute the retention window, leaving full maintenance untouched",
			"profile", p.Name, "err", err)

		return nil
	}

	if time.Now().Before(reclaimFrom) {
		log.Info("retention still covers every blob, deferring full maintenance",
			"profile", p.Name,
			"repository-start", started.UTC().Format(time.RFC3339),
			"reclaim-possible-from", reclaimFrom.UTC().Format(time.RFC3339))

		return wrapper.BuildFullMaintenanceArgs(false)
	}

	log.Info("retention window is open, enabling full maintenance",
		"profile", p.Name,
		"repository-start", started.UTC().Format(time.RFC3339),
		"reclaim-possible-since", reclaimFrom.UTC().Format(time.RFC3339))

	return wrapper.BuildFullMaintenanceArgs(true)
}

// probeRepositoryStart reads the timestamp of the repository format
// blob, at the cost of one listing of a single-blob prefix. The blob is
// written when the repository is created and rewritten on parameter
// changes, so the timestamp is at or after the repository's start - see
// wrapper.formatBlobPrefix for why reading the repository as younger
// than it is stays on the safe side of the decision.
func probeRepositoryStart(ctx context.Context, p config.Profile, password string) (time.Time, error) {
	runner, err := wrapper.New(wrapper.Options{
		KopiaBinary: p.KopiaBinary,
		Profile:     p,
		Command:     wrapper.BuildFormatBlobListArgs(),
		Password:    password,
		// The output is the payload here, not something to show the
		// operator; Result carries it either way.
		Stdout:  io.Discard,
		Stderr:  io.Discard,
		Timeout: fullMaintenanceProbeTimeout,
	})
	if err != nil {
		return time.Time{}, err
	}

	res, err := runner.Run(ctx)
	if err != nil {
		return time.Time{}, err
	}

	return wrapper.OldestBlobTimestamp([]byte(res.Stdout))
}
