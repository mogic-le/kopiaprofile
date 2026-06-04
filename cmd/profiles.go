package cmd

import (
	"github.com/spf13/cobra"
)

// newProfilesCmd implements `kopiaprofile profiles list`.
func newProfilesCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profiles",
		Short: "Manage and inspect profiles",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	list := &cobra.Command{
		Use:   "list",
		Short: "List all configured profiles",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := loadConfig(flags)
			if err != nil {
				return err
			}
			for _, n := range cfg.Names() {
				p, _ := cfg.Get(n)
				desc := p.Description
				if desc == "" {
					desc = "-"
				}
				Print("%-20s %s", n, desc)
			}
			return nil
		},
	}
	groups := &cobra.Command{
		Use:   "groups",
		Short: "List all configured profile groups",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := loadConfig(flags)
			if err != nil {
				return err
			}
			for _, g := range cfg.GroupNames() {
				Print("%s", g)
			}
			return nil
		},
	}
	cmd.AddCommand(list, groups)
	return cmd
}
