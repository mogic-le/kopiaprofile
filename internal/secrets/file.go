package secrets

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// FileLoader reads the first non-empty line of a text file. The file
// is opened with mode 0o600 assumed; if the file is world-readable we
// still return its content (the operator is responsible for setting
// permissions).
type FileLoader struct {
	Path string
}

// Load reads the password from disk. The first non-empty line is taken
// verbatim as the password - including one that happens to start with
// "#". An earlier version skipped "#"-prefixed lines as comments, which
// silently broke on any generated password whose first character was
// "#": a single-line file like that has no non-comment line at all, so
// the old code returned ErrNotFound and the password lookup failed.
func (f FileLoader) Load() (string, error) {
	if f.Path == "" {
		return "", errors.New("file loader: empty path")
	}
	data, err := os.ReadFile(f.Path) // #nosec G304 -- path is user-controlled by design
	if err != nil {
		return "", fmt.Errorf("reading password file %q: %w", f.Path, err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if v := strings.TrimRight(line, "\r"); v != "" {
			return v, nil
		}
	}
	return "", ErrNotFound
}
