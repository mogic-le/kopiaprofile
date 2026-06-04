package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/mogic-le/kopiaprofile/internal/monitor"
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
		Use:   "status",
		Short: "Print the most recent run status",
		RunE: func(_ *cobra.Command, _ []string) error {
			path := file
			if path == "" {
				_, _ = loadConfig(flags) // validate config early
				path = defaultStatusPath()
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			fmt.Println(string(data))
			return nil
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "explicit path to a status file (default: ~/.cache/kopiaprofile/monitor/status.json)")
	return cmd
}

func newMonitorListCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all status files",
		RunE: func(_ *cobra.Command, _ []string) error {
			_, _ = loadConfig(flags)
			dir := defaultStatusDir()
			entries, err := os.ReadDir(dir)
			if err != nil {
				if os.IsNotExist(err) {
					Print("no status directory yet: %s", dir)
					return nil
				}
				return err
			}
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				info, _ := e.Info()
				Print("%s  %d bytes  %s",
					info.ModTime().Format(time.RFC3339),
					info.Size(),
					filepath.Join(dir, e.Name()))
			}
			return nil
		},
	}
	return cmd
}

func defaultStatusPath() string {
	return filepath.Join(defaultStatusDir(), "status.json")
}

func defaultStatusDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "kopiaprofile", "monitor")
}

// buildMonitorManager turns a profile's monitor config into a
// monitor.Manager. The global default file (if any) is added first,
// then the per-profile one — so the per-profile file wins on
// duplicate path.
func buildMonitorManager(profiles ...monitor.Config) *monitor.Manager {
	return monitor.New(profiles...)
}
