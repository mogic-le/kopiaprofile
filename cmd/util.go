package cmd

import (
	"fmt"
	"os"
	"path/filepath"
)

// osStat is a thin wrapper around os.Stat that returns a generic error
// so the caller can use `err == nil` to test for existence. Centralised
// here to keep the cobra command files tidy.
func osStat(path string) (os.FileInfo, error) {
	cleaned := filepath.Clean(path)
	return os.Stat(cleaned)
}

// writeString writes content to path, creating parent directories if
// needed. The file is created with mode 0600 (secrets are sometimes
// embedded in configs).
func writeString(path, content string) error {
	cleaned := filepath.Clean(path)
	if err := os.MkdirAll(filepath.Dir(cleaned), 0o750); err != nil {
		return fmt.Errorf("creating parent dir: %w", err)
	}
	return os.WriteFile(cleaned, []byte(content), 0o600)
}
