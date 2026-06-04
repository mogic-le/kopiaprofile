package secrets

import (
	"errors"
	"fmt"
	"strings"

	"github.com/zalando/go-keyring"

	"github.com/mogic-le/kopiaprofile/internal/config"
)

// ErrUnsupported is returned when the chosen source cannot be used on the
// current platform (e.g. keyring on a headless Linux without a Secret
// Service session bus).
var ErrUnsupported = errors.New("secrets: unsupported source")

// ErrNotFound is returned when the underlying lookup returns no value.
var ErrNotFound = errors.New("secrets: password not found")

// Loader is the contract for password sources. Implementations must be
// safe for concurrent use.
type Loader interface {
	Load() (string, error)
}

// FromProfile returns a Loader that honours the password section of the
// given profile. The profile is expected to be the *resolved* one
// (post-inheritance), so missing fields fall back to a sensible default
// (keyring with service "kopiaprofile", account = profile name).
func FromProfile(p config.Profile) Loader {
	source := strings.ToLower(strings.TrimSpace(p.Password.Source))
	if source == "" {
		source = "keyring"
	}
	switch source {
	case "keyring":
		service := p.Password.KeyringService
		if service == "" {
			service = "kopiaprofile"
		}
		return KeyringLoader{
			Service: service,
			Account: p.Name,
		}
	case "command":
		return CommandLoader{Command: p.Password.Command}
	case "env":
		envName := p.Password.EnvVar
		if envName == "" {
			envName = "KOPIA_PASSWORD"
		}
		return EnvLoader{VarName: envName}
	case "file":
		return FileLoader{Path: p.Password.File}
	default:
		return errorLoader{err: fmt.Errorf("unknown password source %q", source)}
	}
}

// SetKeyring is a convenience function used by `kopiaprofile passwd`.
// It writes the password to the OS keyring under (service, account).
// Returns ErrUnsupported if the platform does not support keyrings.
func SetKeyring(service, account, value string) error {
	if err := keyring.Set(service, account, value); err != nil {
		if errors.Is(err, keyring.ErrUnsupportedPlatform) {
			return ErrUnsupported
		}
		return fmt.Errorf("writing to keyring: %w", err)
	}
	return nil
}

// DeleteKeyring removes a keyring entry. Returns ErrNotFound if the entry
// does not exist; ErrUnsupported on headless platforms.
func DeleteKeyring(service, account string) error {
	err := keyring.Delete(service, account)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, keyring.ErrNotFound):
		return ErrNotFound
	case errors.Is(err, keyring.ErrUnsupportedPlatform):
		return ErrUnsupported
	default:
		return fmt.Errorf("deleting from keyring: %w", err)
	}
}

type errorLoader struct{ err error }

func (e errorLoader) Load() (string, error) { return "", e.err }
