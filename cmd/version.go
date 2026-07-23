package cmd

import (
	"github.com/spf13/cobra"
)

// version is overridable at build time via -ldflags.
var version = "0.2.8-dev"

// newVersionCmd returns the `version` subcommand.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print kopiaprofile version",
		RunE: func(_ *cobra.Command, _ []string) error {
			Print("kopiaprofile %s", version)
			return nil
		},
	}
}
