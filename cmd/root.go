package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// rootFlags holds flags that apply to every subcommand.
type rootFlags struct {
	ConfigFile string
	Verbose    bool
	Quiet      bool
}

// Root is the root cobra command. It is exported for tests.
var Root = newRootCmd()

// Execute is the entry point used by main.go.
func Execute() {
	if err := Root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	flags := &rootFlags{}
	// Make flags globally accessible to helper functions that are
	// not cobra callbacks (e.g. schedule renderer).
	activeFlags = flags
	cmd := &cobra.Command{
		Use:   "kopiaprofile",
		Short: "Configuration wrapper for the Kopia backup tool",
		Long: `kopiaprofile reads a central YAML file describing one or more
backup profiles and orchestrates the underlying kopia(1) binary on
their behalf.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		// If the first positional argument does not match a registered
		// subcommand, treat it as a profile name and dispatch to
		// runProfileCmd.
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return runProfileCmd(flags, args)
		},
	}

	pf := cmd.PersistentFlags()
	pf.StringVarP(&flags.ConfigFile, "config", "c", "kopiaprofile.yaml",
		"path to the kopiaprofile configuration file")
	pf.BoolVarP(&flags.Verbose, "verbose", "v", false,
		"print the exact kopia command line (with secrets masked) before each run")
	pf.BoolVar(&flags.Quiet, "quiet", false,
		"suppress non-error output")

	cmd.AddCommand(newInitCmd(flags))
	cmd.AddCommand(newProfilesCmd(flags))
	cmd.AddCommand(newPasswdCmd(flags))
	cmd.AddCommand(newGenerateCmd(flags))
	cmd.AddCommand(newVersionCmd())
	cmd.AddCommand(newDisplayCmd(flags))
	cmd.AddCommand(newScheduleCmd(flags))
	cmd.AddCommand(newMonitorCmd(flags))
	return cmd
}
