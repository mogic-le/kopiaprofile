package secrets

import (
	"errors"
	"fmt"
	"strings"

	"github.com/zalando/go-keyring"
)

// KeyringLoader reads a password from the OS keyring using
// github.com/zalando/go-keyring. The (Service, Account) pair uniquely
// identifies the entry.
type KeyringLoader struct {
	Service string
	Account string
}

// Load returns the password from the keyring. Returns ErrNotFound if the
// entry is missing and ErrUnsupported on platforms where the keyring is
// not available.
func (k KeyringLoader) Load() (string, error) {
	if k.Service == "" || k.Account == "" {
		return "", errors.New("keyring loader requires service and account")
	}
	value, err := keyring.Get(k.Service, k.Account)
	switch {
	case err == nil:
		return strings.TrimRight(value, "\r\n"), nil
	case errors.Is(err, keyring.ErrNotFound):
		return "", ErrNotFound
	case errors.Is(err, keyring.ErrUnsupportedPlatform):
		return "", ErrUnsupported
	default:
		return "", fmt.Errorf("reading from keyring: %w", err)
	}
}
