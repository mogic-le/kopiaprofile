// Package lock implements file-based mutual exclusion for kopiaprofile
// profile runs.
//
// # Why not gofrs/flock
//
// An earlier version of this package used github.com/gofrs/flock,
// which on Windows opens the lock file with the default share-mode
// (FILE_SHARE_READ | FILE_SHARE_WRITE, no FILE_SHARE_DELETE) and keeps
// the handle open for the entire lock lifetime. Releasing the lock
// and then calling os.Remove on the same file in the same process
// races the kernel's handle teardown:
//
//   - On Windows 10/11 with NTFS, the test
//     "TestAcquireAndRelease" reliably failed in CI with
//     "The process cannot access the file because another process
//     has locked a portion of the file" / ERROR_SHARING_VIOLATION,
//     even after a 5 × 20 ms retry loop.
//   - The first attempt (move the metadata to a sibling .meta file)
//     only masked the original WriteFile-vs-LockFileEx race; the
//     Remove-after-Unlock race is a different code path.
//
// We now use atomic file creation (O_CREATE | O_EXCL) as the lock
// primitive:
//
//   - Acquire: os.OpenFile(path, O_CREATE|O_EXCL|O_WRONLY, 0600).
//     The O_EXCL flag maps to CreateFile with CREATE_NEW, which
//     atomically creates the file or fails with ERROR_FILE_EXISTS
//     if another process already did. The returned *os.File is
//     kept open for the lock duration; closing it is what releases
//     the lock.
//   - Release: close the *os.File, then os.Remove the file. The
//     close-then-remove sequence is reliable on every supported
//     platform: on Windows the handle teardown is synchronous and
//     the subsequent DeleteFileW has nothing left to race with.
//   - Stale detection: a file whose recorded PID is no longer
//     running is treated as stale; ForceInactive then deletes the
//     file and retries acquisition.
//
// The lock file is also the metadata file: it contains
//
//	pid=<int>
//	host=<string>
//	at=<rfc3339>
//
// written at acquisition time. That keeps the on-disk footprint to
// one file per profile (instead of the previous lock + lock.meta
// pair) and removes the cross-path coordination surface entirely.
package lock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
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
	fh     *os.File // the open file handle IS the lock
	closed bool
	mu     sync.Mutex
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
		l, err := tryAcquire(opts)
		if err != nil {
			return nil, err
		}
		if l != nil {
			return l, nil
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

// tryAcquire makes a single attempt to take the lock. It returns
// (nil, nil) when the lock is held by someone else; a non-nil Lock
// on success; or (nil, err) on a hard error.
func tryAcquire(opts Options) (*Lock, error) {
	if opts.ForceInactive {
		stale, pid, err := isStale(opts.Path)
		if err != nil {
			return nil, err
		}
		if stale {
			// The previous holder is gone. Remove the stale file and
			// fall through to a fresh acquisition attempt.
			if err := os.Remove(opts.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("removing stale lock: %w", err)
			}
			_ = pid // for future use (could log the killed PID)
		}
	}
	// O_CREATE | O_EXCL | O_WRONLY: atomically create the file or
	// fail with EEXIST / ERROR_FILE_EXISTS if it already exists.
	// This is the lock acquisition primitive.
	fh, err := os.OpenFile(opts.Path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("acquiring lock: %w", err)
	}
	// We hold the lock. Record PID/host/timestamp in the same file.
	body := fmt.Sprintf("pid=%d\nhost=%s\nat=%s\n",
		os.Getpid(), hostname(), time.Now().UTC().Format(time.RFC3339))
	if _, err := fh.WriteString(body); err != nil {
		_ = fh.Close()
		_ = os.Remove(opts.Path)
		return nil, fmt.Errorf("writing lock metadata: %w", err)
	}
	return &Lock{path: opts.Path, fh: fh}, nil
}

// Release unlocks the lock file. It is safe to call on a Lock that
// was not successfully acquired, and safe to call more than once.
func (l *Lock) Release() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	// Close first, then remove. Close releases the lock; the
	// subsequent os.Remove (DeleteFileW on Windows, unlink(2) on
	// Unix) operates on a file we no longer hold a handle to and
	// therefore cannot race with anything we own.
	if err := l.fh.Close(); err != nil {
		_ = os.Remove(l.path)
		return fmt.Errorf("releasing lock: %w", err)
	}
	// Best-effort cleanup. The "lock" is the open file handle; the
	// file itself is informational.
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
