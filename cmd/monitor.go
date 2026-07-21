package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/mogic-le/kopiaprofile/internal/config"
)

// newMonitorCmd implements `kopiaprofile monitor`. The subcommands:
//
//   - status: print the most recent status file (JSON) in a readable form
//   - list:   list all known status files
func newMonitorCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "monitor",
		Short: "Inspect run status and metrics",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newMonitorStatusCmd(flags))
	cmd.AddCommand(newMonitorListCmd(flags))
	return cmd
}

func newMonitorStatusCmd(flags *rootFlags) *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "status [<profile>]",
		Short: "Print the most recent run status",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			path := file
			if path == "" {
				cfg, err := loadConfig(flags)
				if err != nil {
					return err
				}
				profileName := ""
				if len(args) > 0 {
					profileName = args[0]
				} else {
					names := cfg.Names()
					if len(names) != 1 {
						return errorf("multiple profiles configured, specify one: kopiaprofile monitor status <profile> (or -f <file>)")
					}
					profileName = names[0]
				}
				p, ok := cfg.Get(profileName)
				if !ok {
					return errorf("unknown profile %q", profileName)
				}
				path = statusFilePath(flags, p)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "explicit path to a status file (default: the profile's monitor.status-file, or ~/.cache/kopiaprofile/<profile>/status.json)")
	return cmd
}

func newMonitorListCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all known status files, one per profile",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := loadConfig(flags)
			if err != nil {
				return err
			}
			for _, n := range cfg.Names() {
				p, _ := cfg.Get(n)
				path := statusFilePath(flags, p)
				info, err := os.Stat(path)
				if err != nil {
					if os.IsNotExist(err) {
						Print("%-20s -- (no status yet: %s)", n, path)
						continue
					}
					return err
				}
				Print("%-20s %s  %d bytes  %s", n, info.ModTime().Format(time.RFC3339), info.Size(), path)
			}
			return nil
		},
	}
	return cmd
}

// statusFilePath resolves the status file a profile actually writes
// to: its own monitor.status-file (expanded the same way
// buildMonitorForProfile in run.go does), or the
// ~/.cache/kopiaprofile/<profile>/status.json default. `monitor
// status`/`monitor list` used to look in a flat ~/.cache/kopiaprofile/
// monitor/ directory that no run ever wrote to.
func statusFilePath(flags *rootFlags, p config.Profile) string {
	if p.Monitor.StatusFile != "" {
		expanded := p.Monitor.StatusFile
		if expanded[0] == '~' {
			if home, err := os.UserHomeDir(); err == nil {
				expanded = filepath.Join(home, expanded[1:])
			}
		}
		if !filepath.IsAbs(expanded) {
			expanded = filepath.Join(filepath.Dir(flags.ConfigFile), expanded)
		}
		return expanded
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "kopiaprofile", p.Name, "status.json")
}
