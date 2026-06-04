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

// Load reads the password from disk.
func (f FileLoader) Load() (string, error) {
	if f.Path == "" {
		return "", errors.New("file loader: empty path")
	}
	data, err := os.ReadFile(f.Path) // #nosec G304 -- path is user-controlled by design
	if err != nil {
		return "", fmt.Errorf("reading password file %q: %w", f.Path, err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if v := strings.TrimRight(line, "\r"); v != "" && !strings.HasPrefix(v, "#") {
			return v, nil
		}
	}
	return "", ErrNotFound
}
