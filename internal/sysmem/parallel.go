// Package sysmem caps kopia's snapshot parallelism to what the host can
// actually afford right now. A fleet-wide fixed --parallel value is safe
// on a spare host but not on one already under memory pressure from its
// own workload (Docker, databases, ...): found live on a 7.6GB host with
// no swap that was OOM-killed mid-scan at parallel=8 (kernel OOM, not a
// cgroup limit - confirmed via dmesg, real anon-rss ~3.5GB for the kopia
// process alone). A per-host static override in the profile can't track
// that either, since how much memory is free varies with whatever else
// is running on the host at backup time, not just its installed RAM.
package sysmem

import (
	"bufio"
	"io"
	"os"
	"strconv"
	"strings"
)

// perWorkerBudgetMB is the assumed peak memory footprint of one kopia
// snapshot worker. Derived from the OOM above: parallel=8 reached
// ~3.5GB RSS, i.e. ~440MB/worker; rounded up for a safety margin.
const perWorkerBudgetMB = 512

// usableFraction is the share of currently-available memory kopia is
// allowed to plan for. The rest stays as headroom for the host's other
// processes and for kopia's own usage briefly exceeding the estimate.
const usableFraction = 0.7

// ClampParallel returns the largest parallelism no greater than
// configured that the host's currently-available memory can plausibly
// support. If configured is <= 0 (the "don't pass --parallel at all"
// sentinel used elsewhere in this package) or available memory can't be
// determined (non-Linux, unreadable /proc/meminfo), it returns
// configured unchanged - this only ever narrows an explicit value, never
// invents one or blocks on an unsupported platform.
func ClampParallel(configured int) int {
	availableMB, ok := AvailableMB()
	return clampWithAvailable(configured, availableMB, ok)
}

// clampWithAvailable is ClampParallel's pure computation, split out so
// tests can supply a fixed availableMB instead of reading the real
// /proc/meminfo.
func clampWithAvailable(configured int, availableMB uint64, ok bool) int {
	if configured <= 0 || !ok {
		return configured
	}

	budget := float64(availableMB) * usableFraction
	computedMax := int(budget / perWorkerBudgetMB)
	if computedMax < 1 {
		computedMax = 1
	}

	if computedMax < configured {
		return computedMax
	}

	return configured
}

// AvailableMB returns the current MemAvailable value from /proc/meminfo
// in megabytes, and false if it can't be read (non-Linux, or the file/
// field is missing).
func AvailableMB() (uint64, bool) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, false
	}
	defer func() { _ = f.Close() }()

	return parseMemAvailable(f)
}

func parseMemAvailable(r io.Reader) (uint64, bool) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "MemAvailable:") {
			continue
		}

		fields := strings.Fields(line)
		// Expected: "MemAvailable:", "<kB value>", "kB"
		if len(fields) < 2 {
			return 0, false
		}

		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0, false
		}

		return kb / 1024, true
	}

	return 0, false
}
