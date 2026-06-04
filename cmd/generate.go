package cmd

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/spf13/cobra"
)

// newGenerateCmd implements `kopiaprofile generate --random-key`.
func newGenerateCmd(flags *rootFlags) *cobra.Command {
	var randomKey bool
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate random secrets / keys for use in kopiaprofile",
		RunE: func(_ *cobra.Command, _ []string) error {
			if !randomKey {
				return errorf("specify --random-key (other generators are planned)")
			}
			buf := make([]byte, 32)
			if _, err := rand.Read(buf); err != nil {
				return errorf("reading random bytes: %w", err)
			}
			Print("%s", hex.EncodeToString(buf))
			return nil
		},
	}
	cmd.Flags().BoolVar(&randomKey, "random-key", false, "print a 32-byte hex-encoded random key")
	return cmd
}
