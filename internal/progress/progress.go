// Package progress reads the live-progress log kopiaprofile tees a
// running kopia invocation's stdout/stderr into (see
// internal/profile/runner.go), for `kopiaprofile <profile> watch` to
// report on a currently-running (or just-finished) backup without
// needing a long-lived server process.
package progress

import (
	"errors"
	"os"
	"regexp"
	"strconv"
	"strings"
)

// chunkSize is how much of the tail of the file we read looking for
// the last N lines, before falling back to reading the whole file.
// Kopia's own progress line is well under 200 bytes; 64KiB comfortably
// covers a few hundred lines without reading a multi-hour run's entire
// log (which, at roughly one line/second, can reach tens of MB).
const chunkSize = 64 * 1024

// TailLines returns up to the last n non-empty lines of the file at
// path, oldest first. A missing file yields (nil, nil) - "no progress
// recorded yet" is a normal state, not an error, e.g. right after a
// profile's very first run before anything has been written.
func TailLines(path string, n int) ([]string, error) {
	if n <= 0 {
		return nil, nil
	}
	f, err := os.Open(path) // #nosec G304 -- path is derived from the profile's own lock path
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close() //nolint:errcheck

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := info.Size()
	readSize := int64(chunkSize)
	if size < readSize {
		readSize = size
	}
	buf := make([]byte, readSize)
	if _, err := f.ReadAt(buf, size-readSize); err != nil {
		return nil, err
	}

	lines := strings.Split(string(buf), "\n")
	// The first entry is likely a partial line (we started reading
	// mid-file) unless we read from the very beginning - drop it in
	// that case to avoid returning a truncated line as if complete.
	if readSize < size && len(lines) > 0 {
		lines = lines[1:]
	}
	// Trim trailing empties (final newline, blank separator lines
	// kopia prints between sections).
	out := lines[:0]
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out, nil
}

// Snapshot is what we managed to parse out of kopia's own progress
// line, e.g.
//
//	[5:31] 78.30%  120 MB/s  8.5 GB / 11 TB  1234567 / 1500000 items  0 errors  ETA 0:45:00
//
// All fields are kept as the original strings kopia printed (already
// human-formatted, no unit conversion needed) - only Percent is parsed
// to a float so callers can compare/sort/threshold on it.
type Snapshot struct {
	Elapsed    string
	Percent    float64
	Speed      string
	Done       string
	Total      string
	DoneItems  string
	TotalItems string
	Errors     string
	ETA        string
}

// progressLineRE matches kopia's own periodic progress line (verified
// live against real kopia 0.23.1-mogic-objectlock output, e.g.
// "[1:57] 100.00%  0B/s  7.004 GiB / 7.004 GiB  57611 / 57611 items  0
// errors  ETA 0:00"). Deliberately tolerant of the exact whitespace/unit
// formatting - kopia's own progress printer is not a stable, versioned
// interface, so this is a best-effort parse: no match just means the
// caller falls back to showing the raw tailed lines instead of a
// parsed summary, never an error.
var progressLineRE = regexp.MustCompile(
	`^\[([\d:]+)\]\s+([\d.]+)%\s+(\S+)\s+([\d.]+\s*\S+)\s*/\s*([\d.]+\s*\S+)\s+(\d+)\s*/\s*(\d+)\s+items\s+(\d+)\s+errors?\s+ETA\s+(\S+)`,
)

// ParseLine attempts to parse a single line as a kopia progress
// update. ok is false (with a zero Snapshot) when line doesn't match -
// callers should treat that as "not a progress line", not an error.
func ParseLine(line string) (snap Snapshot, ok bool) {
	m := progressLineRE.FindStringSubmatch(strings.TrimSpace(line))
	if m == nil {
		return Snapshot{}, false
	}
	pct, err := strconv.ParseFloat(m[2], 64)
	if err != nil {
		return Snapshot{}, false
	}
	return Snapshot{
		Elapsed:    m[1],
		Percent:    pct,
		Speed:      m[3],
		Done:       strings.TrimSpace(m[4]),
		Total:      strings.TrimSpace(m[5]),
		DoneItems:  m[6],
		TotalItems: m[7],
		Errors:     m[8],
		ETA:        m[9],
	}, true
}

// LastSnapshot scans lines (as returned by TailLines, oldest first) from
// the end and returns the most recent one that parses as a kopia
// progress update. ok is false if none of the lines matched.
func LastSnapshot(lines []string) (snap Snapshot, ok bool) {
	for i := len(lines) - 1; i >= 0; i-- {
		if s, matched := ParseLine(lines[i]); matched {
			return s, true
		}
	}
	return Snapshot{}, false
}
