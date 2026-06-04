package wrapper

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/mogic-le/kopiaprofile/internal/config"
)

// ObjectLockAction describes what to do with the bucket's object-lock
// configuration.
type ObjectLockAction struct {
	// Mode is "compliance", "governance" or "none".
	Mode string
	// RetentionPeriod is a Go duration string (e.g. "720h"). It is
	// informational only; Kopia does not expose a CLI flag to set the
	// bucket-level retention period directly. The operator must
	// configure the bucket out-of-band (e.g. via `aws s3api
	// put-object-lock-configuration`).
	RetentionPeriod string
	// ExtendOnMaintenance, if true, causes kopiaprofile to set
	// `kopia maintenance set --extend-object-locks=true` so that full
	// maintenance runs extend the per-blob retention window.
	ExtendOnMaintenance bool
}

// Validate returns an error if the configuration is invalid.
func (a ObjectLockAction) Validate() error {
	switch strings.ToLower(a.Mode) {
	case "", "compliance", "governance":
		// ok
	case "none":
		// ok - means the operator does not want object lock
	default:
		return fmt.Errorf("object-lock: invalid mode %q (expected compliance|governance|none)", a.Mode)
	}
	return nil
}

// ApplyObjectLockMaintenance runs
//
//	kopia maintenance set --extend-object-locks=true
//
// when ExtendOnMaintenance is true. It is a no-op otherwise.
func ApplyObjectLockMaintenance(ctx context.Context, p config.Profile, password string, stdout, stderr io.Writer) error {
	if !p.Repository.ObjectLock.ExtendOnMaintenance {
		return nil
	}
	r, err := New(Options{
		KopiaBinary: pickBinary(p),
		Profile:     p,
		Command:     []string{"maintenance", "set", "--extend-object-locks=true"},
		Password:    password,
		Stdout:      stdout,
		Stderr:      stderr,
	})
	if err != nil {
		return err
	}
	_, err = r.Run(ctx)
	return err
}

func pickBinary(p config.Profile) string {
	if p.KopiaBinary != "" {
		return p.KopiaBinary
	}
	return "kopia"
}
