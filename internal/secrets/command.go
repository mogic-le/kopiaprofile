package secrets

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// CommandLoader runs an external command and uses its stdout (with the
// trailing newline stripped) as the password.
//
// The command is executed via "/bin/sh -c" on POSIX systems and via
// "cmd /c" on Windows.
type CommandLoader struct {
	Command string
}

// Load runs the command and returns its stdout. The first non-empty
// line of stdout is taken; trailing whitespace is stripped.
func (c CommandLoader) Load() (string, error) {
	if strings.TrimSpace(c.Command) == "" {
		return "", errors.New("command loader: empty command")
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", c.Command) // #nosec G204 -- user-supplied by design
	} else {
		cmd = exec.Command("/bin/sh", "-c", c.Command) // #nosec G204 -- user-supplied by design
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("running password command %q: %w (stderr: %s)",
			c.Command, err, strings.TrimSpace(stderr.String()))
	}
	for _, line := range strings.Split(stdout.String(), "\n") {
		if v := strings.TrimRight(line, "\r"); v != "" {
			return v, nil
		}
	}
	return "", errors.New("command loader: empty stdout")
}
