package progress

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTailLinesMissingFile(t *testing.T) {
	dir := t.TempDir()
	lines, err := TailLines(filepath.Join(dir, "nope.log"), 5)
	if err != nil {
		t.Fatalf("expected no error for a missing file, got %v", err)
	}
	if lines != nil {
		t.Errorf("expected nil lines for a missing file, got %v", lines)
	}
}

func TestTailLinesReturnsLastN(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	content := "line1\nline2\nline3\nline4\nline5\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	lines, err := TailLines(path, 2)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"line4", "line5"}
	if len(lines) != len(want) || lines[0] != want[0] || lines[1] != want[1] {
		t.Errorf("got %v, want %v", lines, want)
	}
}

func TestTailLinesFewerLinesThanRequested(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	if err := os.WriteFile(path, []byte("only-one-line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lines, err := TailLines(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || lines[0] != "only-one-line" {
		t.Errorf("got %v, want [only-one-line]", lines)
	}
}

func TestTailLinesLargerThanChunk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	// Write well more than chunkSize (64KiB) worth of lines so TailLines
	// exercises its "read only the tail, discard the first (partial)
	// line" path instead of the whole-file path.
	var b strings.Builder
	for i := 0; i < 20000; i++ {
		b.WriteString("filler line to pad the log past one chunk of reading\n")
	}
	b.WriteString("second-to-last\n")
	b.WriteString("last\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	lines, err := TailLines(path, 2)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"second-to-last", "last"}
	if len(lines) != 2 || lines[0] != want[0] || lines[1] != want[1] {
		t.Errorf("got %v, want %v", lines, want)
	}
}

func TestParseLineMatchesRealKopiaOutput(t *testing.T) {
	line := "[1:57] 100.00%  0B/s  7.004 GiB / 7.004 GiB  57611 / 57611 items  0 errors  ETA 0:00"
	snap, ok := ParseLine(line)
	if !ok {
		t.Fatalf("expected line to parse: %q", line)
	}
	if snap.Percent != 100.0 {
		t.Errorf("Percent: got %v want 100.0", snap.Percent)
	}
	if snap.Done != "7.004 GiB" || snap.Total != "7.004 GiB" {
		t.Errorf("Done/Total: got %q/%q", snap.Done, snap.Total)
	}
	if snap.DoneItems != "57611" || snap.TotalItems != "57611" {
		t.Errorf("DoneItems/TotalItems: got %q/%q", snap.DoneItems, snap.TotalItems)
	}
	if snap.Errors != "0" {
		t.Errorf("Errors: got %q want 0", snap.Errors)
	}
	if snap.ETA != "0:00" {
		t.Errorf("ETA: got %q want 0:00", snap.ETA)
	}
}

func TestParseLinePartialProgress(t *testing.T) {
	// Speed has no space before the unit (verified live: "61.10MiB/s"),
	// unlike Done/Total size, which does ("7.004 GiB").
	line := "[5:31] 42.10%  120MB/s  8.5 GB / 11 TB  1234567 / 1500000 items  0 errors  ETA 3:12:00"
	snap, ok := ParseLine(line)
	if !ok {
		t.Fatalf("expected line to parse: %q", line)
	}
	if snap.Percent != 42.10 {
		t.Errorf("Percent: got %v want 42.10", snap.Percent)
	}
	if snap.ETA != "3:12:00" {
		t.Errorf("ETA: got %q want 3:12:00", snap.ETA)
	}
}

func TestParseLineNonProgressLines(t *testing.T) {
	nonProgress := []string{
		"",
		"Snapshotting root@example-host:/srv/app/data ...",
		"scan [/srv/www]",
		"[0:02] 6692 directories, 50919 files, 7.004 GiB",
		"scanned 6692 directories, 50919 files in 0:02",
		"duration: 1:57, 61.10MiB/s",
		"snapshot d85f571e saved",
		"Running full maintenance...",
	}
	for _, l := range nonProgress {
		if _, ok := ParseLine(l); ok {
			t.Errorf("expected %q to NOT parse as a progress line", l)
		}
	}
}

func TestLastSnapshotFindsMostRecentMatch(t *testing.T) {
	lines := []string{
		"Snapshotting root@host:/ ...",
		"[0:10] 10.00%  5MB/s  1 GB / 10 GB  100 / 1000 items  0 errors  ETA 1:00:00",
		"some unrelated log line",
		"[0:20] 20.00%  5MB/s  2 GB / 10 GB  200 / 1000 items  0 errors  ETA 0:50:00",
	}
	snap, ok := LastSnapshot(lines)
	if !ok {
		t.Fatal("expected a match")
	}
	if snap.Percent != 20.0 {
		t.Errorf("expected the LAST matching line (20%%), got %v", snap.Percent)
	}
}

func TestLastSnapshotNoMatch(t *testing.T) {
	_, ok := LastSnapshot([]string{"nothing here", "still nothing"})
	if ok {
		t.Error("expected no match")
	}
}
