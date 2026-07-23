package secrets

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFileLoader(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "pw")
	if err := os.WriteFile(p, []byte("hunter2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := FileLoader{Path: p}.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got != "hunter2" {
		t.Errorf("got %q", got)
	}
}

func TestFileLoaderMissing(t *testing.T) {
	_, err := FileLoader{Path: "/nonexistent/file"}.Load()
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestFileLoaderPasswordStartingWithHash(t *testing.T) {
	// A machine-generated password may start with "#" - it must be read
	// verbatim, not treated as a comment line to skip (that previously
	// left a single-line file like this with no line at all, i.e.
	// ErrNotFound, for every host whose generated password happened to
	// start with "#").
	dir := t.TempDir()
	p := filepath.Join(dir, "pw")
	if err := os.WriteFile(p, []byte("#L!k$5IWoI?as10Z\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := FileLoader{Path: p}.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got != "#L!k$5IWoI?as10Z" {
		t.Errorf("got %q", got)
	}
}

func TestFileLoaderSkipsBlankLines(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "pw")
	if err := os.WriteFile(p, []byte("\n\nhunter2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := FileLoader{Path: p}.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got != "hunter2" {
		t.Errorf("got %q", got)
	}
}

func TestEnvLoader(t *testing.T) {
	t.Setenv("KOPIA_PROFILE_TEST_PW", "secret")
	got, err := EnvLoader{VarName: "KOPIA_PROFILE_TEST_PW"}.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got != "secret" {
		t.Errorf("got %q", got)
	}
}

func TestEnvLoaderMissing(t *testing.T) {
	_, err := EnvLoader{VarName: "KOPIA_PROFILE_TEST_NEVER_SET"}.Load()
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestCommandLoader(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip on windows")
	}
	got, err := CommandLoader{Command: "printf secret"}.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "secret") {
		t.Errorf("got %q", got)
	}
}

func TestCommandLoaderFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip on windows")
	}
	_, err := CommandLoader{Command: "exit 5"}.Load()
	if err == nil {
		t.Fatal("expected error from failing command")
	}
}
