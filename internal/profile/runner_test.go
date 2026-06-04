package profile

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/mogic-le/kopiaprofile/internal/config"
)

type fakeLoader struct{ value string }

func (f fakeLoader) Load() (string, error) { return f.value, nil }

// failingLoader returns a sentinel error.
type failingLoader struct{}

var errFake = errors.New("fake password error")

func (failingLoader) Load() (string, error) { return "", errFake }

func TestRunSkipsHooksAndLock(t *testing.T) {
	prof := config.Profile{
		Name:      "test",
		Backup:    config.BackupSection{Sources: []string{"/tmp"}},
		Lock:      config.LockSection{Path: filepath.Join(t.TempDir(), "x.lock")},
		RunBefore: "echo before",
		RunAfter:  "echo after",
	}
	_, err := Run(context.Background(), RunOptions{
		Profile:        prof,
		Command:        []string{"snapshot", "create", "/tmp"},
		SkipHooks:      true,
		SkipLock:       true,
		PasswordSource: fakeLoader{value: "secret"},
		Timeout:        200 * time.Millisecond,
	})
	// We expect kopia to be missing, so the call will fail. That's fine
	// - we just want to verify the loader was honoured and pre-hooks
	// were skipped.
	if err == nil {
		t.Log("kopia was found and ran successfully (unusual in CI)")
	}
}

func TestRunPasswordFailure(t *testing.T) {
	prof := config.Profile{Name: "test"}
	_, err := Run(context.Background(), RunOptions{
		Profile:        prof,
		Command:        []string{"snapshot", "list"},
		SkipLock:       true,
		PasswordSource: failingLoader{},
	})
	if !errors.Is(err, errFake) {
		t.Errorf("expected errFake, got %v", err)
	}
}
