package lock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gofrs/flock"
)

// pidAliveOS is the platform-specific implementation of pidAlive.
// On Unix, signal 0 is the only reliable test for "process exists".
// On Windows, signal 0 semantics differ; we fall back to FindProcess
// (which always succeeds on Windows) and assume the PID is alive.
var pidAliveOS = func(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if err := proc.Signal(testSignal()); err != nil {
		return false
	}
	return true
}

// ErrLocked is returned by Acquire when the lock is held by another
// running process and ForceInactive is false.
var ErrLocked = errors.New("lock: already held")

// Lock represents an acquired (or attempted) lock. Always call Release()
// on a successful acquisition to avoid leaking the file descriptor.
type Lock struct {
	path   string
	flock  *flock.Flock
	holder bool
}

// Options configures Lock behaviour.
type Options struct {
	// Path is the file path used as the lock. Required.
	Path string
	// ForceInactive allows breaking the lock when the previous holder's
	// PID is no longer running.
	ForceInactive bool
	// RetryAfter is the wait between lock attempts when the file is
	// locked. Zero means do not retry.
	RetryAfter time.Duration
	// Timeout is the maximum total time spent waiting. Zero means try
	// once.
	Timeout time.Duration
}

// Acquire attempts to obtain the lock. It returns ErrLocked if another
// process holds it. If opts.ForceInactive is true and the recorded PID
// is gone, the lock is taken over (after deleting the stale file).
func Acquire(opts Options) (*Lock, error) {
	if opts.Path == "" {
		return nil, errors.New("lock: path is required")
	}
	if err := os.MkdirAll(filepath.Dir(opts.Path), 0o750); err != nil {
		return nil, fmt.Errorf("creating lock directory: %w", err)
	}
	deadline := time.Now().Add(opts.Timeout)
	for {
		ok, err := tryAcquire(opts)
		if err != nil {
			return nil, err
		}
		if ok {
			return &Lock{path: opts.Path, flock: flock.New(opts.Path), holder: true}, nil
		}
		if opts.RetryAfter <= 0 || (!opts.ForceInactive && time.Now().After(deadline)) {
			return nil, ErrLocked
		}
		if time.Now().After(deadline) {
			return nil, ErrLocked
		}
		time.Sleep(opts.RetryAfter)
	}
}

func tryAcquire(opts Options) (bool, error) {
	// First, check whether the lock is stale.
	if opts.ForceInactive {
		stale, pid, err := isStale(opts.Path)
		if err != nil {
			return false, err
		}
		if stale {
			if err := os.Remove(opts.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return false, fmt.Errorf("removing stale lock: %w", err)
			}
			_ = pid // for future use (could log the killed PID)
		}
	}
	l := flock.New(opts.Path)
	got, err := l.TryLock()
	if err != nil {
		return false, fmt.Errorf("acquiring lock: %w", err)
	}
	if !got {
		_ = l.Unlock()
		return false, nil
	}
	// Write our PID/hostname so a future process can detect staleness.
	body := fmt.Sprintf("pid=%d\nhost=%s\nat=%s\n",
		os.Getpid(), hostname(), time.Now().UTC().Format(time.RFC3339))
	if err := os.WriteFile(opts.Path, []byte(body), 0o600); err != nil {
		_ = l.Unlock()
		return false, fmt.Errorf("writing lock metadata: %w", err)
	}
	return true, nil
}

// Release unlocks the lock file. It is safe to call on a Lock that
// was not successfully acquired.
func (l *Lock) Release() error {
	if l == nil || !l.holder {
		return nil
	}
	l.holder = false
	if err := l.flock.Unlock(); err != nil {
		return fmt.Errorf("releasing lock: %w", err)
	}
	// Best-effort cleanup. The lock file is informational; the OS-level
	// flock is the source of truth.
	_ = os.Remove(l.path)
	return nil
}

// Path returns the lock file path.
func (l *Lock) Path() string { return l.path }

// isStale returns true when the lock file's recorded PID is no longer
// running on this host. The function is best-effort: any error other
// than a missing lock file is propagated; missing file is treated as
// "not stale".
func isStale(path string) (stale bool, pid int, err error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is the lock file we own
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true, 0, nil
		}
		return false, 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "pid=") {
			continue
		}
		raw := strings.TrimPrefix(line, "pid=")
		p, perr := strconv.Atoi(strings.TrimSpace(raw))
		if perr != nil {
			return false, 0, nil
		}
		pid = p
		return !pidAlive(p), pid, nil
	}
	// No pid recorded -> assume stale.
	return true, 0, nil
}

// pidAlive is a small wrapper around os.FindProcess / signal 0 so it
// can be stubbed in tests.
var pidAlive = pidAliveOS

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}
