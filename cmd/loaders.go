package cmd

import (
	"fmt"

	"github.com/mogic-le/kopiaprofile/internal/config"
	"github.com/mogic-le/kopiaprofile/internal/secrets"
)

// loadPasswordSource returns a secrets.Loader for the given profile.
// We extract it into a thin wrapper so tests can stub it.
var loadPasswordSource = func(p config.Profile) secrets.Loader {
	return secrets.FromProfile(p)
}

// storeKeyring writes a password to the OS keyring. Wraps the
// secrets.SetKeyring helper for symmetry with loadPasswordSource.
var storeKeyring = func(service, account, password string) error {
	return secrets.SetKeyring(service, account, password)
}

// _ ensures fmt is imported even if Print is stubbed.
var _ = fmt.Sprintf
