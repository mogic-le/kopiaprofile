package sysmem

import (
	"strings"
	"testing"
)

const sampleMeminfo = `MemTotal:        7936000 kB
MemFree:         1234000 kB
MemAvailable:    4096000 kB
Buffers:          200000 kB
Cached:          1500000 kB
`

func TestParseMemAvailable(t *testing.T) {
	mb, ok := parseMemAvailable(strings.NewReader(sampleMeminfo))
	if !ok {
		t.Fatal("expected ok=true")
	}
	if mb != 4000 {
		t.Errorf("got %d MB, want 4000", mb)
	}
}

func TestParseMemAvailableMissingField(t *testing.T) {
	_, ok := parseMemAvailable(strings.NewReader("MemTotal: 7936000 kB\n"))
	if ok {
		t.Error("expected ok=false when MemAvailable is missing")
	}
}

func TestParseMemAvailableEmpty(t *testing.T) {
	_, ok := parseMemAvailable(strings.NewReader(""))
	if ok {
		t.Error("expected ok=false for empty input")
	}
}

func TestClampParallelNoOpForNonPositive(t *testing.T) {
	if got := ClampParallel(0); got != 0 {
		t.Errorf("got %d, want 0 (sentinel passthrough)", got)
	}
	if got := ClampParallel(-1); got != -1 {
		t.Errorf("got %d, want -1 (sentinel passthrough)", got)
	}
}

func TestClampParallelBelowConfigured(t *testing.T) {
	// 700MB available * 0.7 usable = 490MB budget / 512MB per worker
	// rounds down to 0, floored to 1 - a tight host should still get at
	// least one worker, never zero.
	available := uint64(700)
	got := clampWithAvailable(8, available, true)
	if got != 1 {
		t.Errorf("got %d, want 1", got)
	}
}

func TestClampParallelAmpleMemoryReturnsConfigured(t *testing.T) {
	// 64GB available comfortably supports the configured value.
	got := clampWithAvailable(8, 65536, true)
	if got != 8 {
		t.Errorf("got %d, want 8 (configured value, memory is not the constraint)", got)
	}
}

func TestClampParallelUnavailableReturnsConfigured(t *testing.T) {
	got := clampWithAvailable(8, 0, false)
	if got != 8 {
		t.Errorf("got %d, want 8 (fail open when memory can't be determined)", got)
	}
}
