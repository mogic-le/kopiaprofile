package cmd

import (
	"fmt"
	"time"

	"github.com/mogic-le/kopiaprofile/internal/config"
	"github.com/mogic-le/kopiaprofile/internal/lock"
	"github.com/mogic-le/kopiaprofile/internal/profile"
	"github.com/mogic-le/kopiaprofile/internal/progress"
)

// defaultStaleAfter is how long a running profile can go without a new
// progress-log line before `watch` flags it as possibly stuck. Kopia's
// own progress printer updates roughly every few seconds during a
// snapshot; 15 minutes of total silence is well past anything a slow
// disk or network hiccup would explain on its own.
const defaultStaleAfter = 15 * time.Minute

// defaultTailLines is how many raw progress-log lines watch shows when
// none of them parse as a kopia progress update (e.g. still in the
// initial directory scan, or a hook is running).
const defaultTailLines = 10

// runWatchAction implements `kopiaprofile <profile> watch`. Unlike
// every other action, this never invokes kopia at all - it only reads
// the profile's own lock file and progress log (see internal/lock and
// internal/profile.Run's tee in runner.go), so it is safe to run
// concurrently with (or long after) a real backup without contending
// for the lock itself.
func runWatchAction(p config.Profile) error {
	lockPath := profile.LockPath(p)
	running, info, err := lock.IsRunning(lockPath)
	if err != nil {
		return errorf("reading lock file %q: %w", lockPath, err)
	}

	progressPath := profile.ProgressLogPath(p)
	lines, terr := progress.TailLines(progressPath, defaultTailLines)
	if terr != nil {
		return errorf("reading progress log %q: %w", progressPath, terr)
	}

	if running {
		since := ""
		if !info.At.IsZero() {
			since = fmt.Sprintf(", started %s (running %s)",
				info.At.Local().Format("2006-01-02 15:04:05"),
				time.Since(info.At).Round(time.Second))
		}
		Print("RUNNING - pid %d on %s%s", info.PID, info.Host, since)
	} else {
		Print("NOT RUNNING - no active lock at %s", lockPath)
	}

	if len(lines) == 0 {
		if running {
			Print("No progress output yet - still starting up, or nothing has been printed.")
		} else {
			Print("No progress log found - has this profile ever run a command that prints progress?")
		}
		return nil
	}

	// mtime of the progress log doubles as "time of last output" - good
	// enough for a staleness check without needing per-line timestamps
	// kopia itself doesn't print.
	age := progressLogAge(progressPath)
	if running && age > 0 && age >= defaultStaleAfter {
		Print("WARNING: no new output in %s - this run may be stuck", age.Round(time.Second))
	}

	if snap, ok := progress.LastSnapshot(lines); ok {
		Print("Progress: %.2f%%  %s  %s / %s  %s / %s items  %s errors  ETA %s",
			snap.Percent, snap.Speed, snap.Done, snap.Total,
			snap.DoneItems, snap.TotalItems, snap.Errors, snap.ETA)
	}

	ageLabel := "unknown"
	if age > 0 {
		ageLabel = age.Round(time.Second).String() + " ago"
	}
	Print("Last output (%s):", ageLabel)
	for _, l := range lines {
		Print("  %s", l)
	}
	return nil
}

func progressLogAge(path string) time.Duration {
	st, err := osStat(path)
	if err != nil {
		return 0
	}
	return time.Since(st.ModTime())
}
