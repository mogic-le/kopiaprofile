package secrets

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// EnvLoader reads a password from an environment variable. The
// variable name is taken from VarName; an empty variable is treated
// as "not found".
type EnvLoader struct {
	VarName string
}

// Load returns the value of the configured environment variable, with
// trailing newlines stripped.
func (e EnvLoader) Load() (string, error) {
	if e.VarName == "" {
		return "", errors.New("env loader: empty variable name")
	}
	v, ok := os.LookupEnv(e.VarName)
	if !ok {
		return "", ErrNotFound
	}
	v = strings.TrimRight(v, "\r\n")
	if v == "" {
		return "", ErrNotFound
	}
	return v, nil
}

// EnvVar returns the resolved variable name (handy for diagnostic
// messages without leaking the value).
func (e EnvLoader) EnvVar() string { return e.VarName }

var _ = fmt.Sprintf // keep fmt linked to satisfy lint when tests grow
