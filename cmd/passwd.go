package cmd

import (
	"github.com/spf13/cobra"
)

// newPasswdCmd implements `kopiaprofile passwd <profile>`.
func newPasswdCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "passwd <profile>",
		Short: "Load the repository password for a profile and store it in the OS keyring",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			name := args[0]
			cfg, err := loadConfig(flags)
			if err != nil {
				return err
			}
			prof, ok := cfg.Get(name)
			if !ok {
				return errorf("unknown profile %q", name)
			}
			loader := loadPasswordSource(prof)
			pw, err := loader.Load()
			if err != nil {
				return errorf("loading password for profile %q: %w", name, err)
			}
			service := prof.Password.KeyringService
			if service == "" {
				service = "kopiaprofile"
			}
			if err := storeKeyring(service, name, pw); err != nil {
				return errorf("storing password in keyring: %w", err)
			}
			Print("stored password in keyring (service=%q, account=%q)", service, name)
			return nil
		},
	}
	return cmd
}
