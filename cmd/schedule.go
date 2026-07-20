package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mogic-le/kopiaprofile/internal/config"
	"github.com/mogic-le/kopiaprofile/internal/schedule"
)

// newScheduleCmd implements `kopiaprofile schedule`. The
// subcommands are:
//
//   - render:   print the rendered crontab/systemd/launchd to stdout
//   - install:  write the rendered files to disk
//   - list:     list the configured schedules
func newScheduleCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schedule",
		Short: "Manage backup schedules (crontab, systemd, launchd)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newScheduleListCmd(flags))
	cmd.AddCommand(newScheduleRenderCmd(flags))
	cmd.AddCommand(newScheduleInstallCmd(flags))
	return cmd
}

func newScheduleListCmd(flags *rootFlags) *cobra.Command {
	var profileFilter string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all configured backup schedules",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := loadConfig(flags)
			if err != nil {
				return err
			}
			if err := collectSchedules(cfg, profileFilter); err != nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&profileFilter, "profile", "p", "", "only show schedules for the given profile")
	return cmd
}

func newScheduleRenderCmd(flags *rootFlags) *cobra.Command {
	var format string
	var output string
	var profileFilter string
	cmd := &cobra.Command{
		Use:   "render",
		Short: "Render the schedule to stdout (or a file with --output)",
		Long: `render prints the rendered schedule in one of three formats:

  --format=crontab  a crontab fragment (default)
  --format=systemd  a directory of .service and .timer units
  --format=launchd  a directory of launchd plists

The output destination can be overridden with --output (a file for
crontab, a directory for systemd/launchd).`,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := loadConfig(flags)
			if err != nil {
				return err
			}
			sc, err := buildScheduleConfig(cfg, profileFilter)
			if err != nil {
				return err
			}
			return renderSchedule(sc, format, output)
		},
	}
	cmd.Flags().StringVar(&format, "format", "crontab", "output format: crontab|systemd|launchd")
	cmd.Flags().StringVarP(&output, "output", "o", "", "output path (file for crontab, directory for systemd/launchd); default: stdout")
	cmd.Flags().StringVarP(&profileFilter, "profile", "p", "", "only render schedules for the given profile")
	return cmd
}

func newScheduleInstallCmd(flags *rootFlags) *cobra.Command {
	var format string
	var dest string
	var profileFilter string
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install the rendered schedule to its target location",
		Long: `install writes the schedule to a system location. For
crontab, it appends to the user's crontab (use --dest to override the
target user). For systemd, it writes to /etc/systemd/system and runs
systemctl daemon-reload + systemctl enable --now. For launchd, it
copies the plist to ~/Library/LaunchAgents.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := loadConfig(flags)
			if err != nil {
				return err
			}
			sc, err := buildScheduleConfig(cfg, profileFilter)
			if err != nil {
				return err
			}
			return installSchedule(sc, format, dest, flags)
		},
	}
	cmd.Flags().StringVar(&format, "format", detectScheduler(), "output format: crontab|systemd|launchd (auto-detected)")
	cmd.Flags().StringVarP(&dest, "dest", "d", "", "destination: file (crontab) or directory (systemd/launchd)")
	cmd.Flags().StringVarP(&profileFilter, "profile", "p", "", "only install schedules for the given profile")
	return cmd
}

// detectScheduler picks a reasonable default for the local OS.
func detectScheduler() string {
	switch {
	case isLinux():
		return "systemd"
	case isMac():
		return "launchd"
	default:
		return "crontab"
	}
}

func collectSchedules(cfg *config.File, profileFilter string) error {
	for _, pname := range cfg.Names() {
		if profileFilter != "" && pname != profileFilter {
			continue
		}
		p, _ := cfg.Get(pname)
		for _, s := range p.Schedule {
			Print("%-15s %-12s %-30s at %s", pname, s.Name, s.Action, s.At)
		}
	}
	return nil
}

// buildScheduleConfig converts the resolved profile schedule entries
// into a schedule.Config that the renderers can consume. Each
// profile's schedule entries become individual schedule.Schedule
// records.
func buildScheduleConfig(cfg *config.File, profileFilter string) (schedule.Config, error) {
	sc := schedule.Config{
		KopiaBinary:        "kopia",
		KopiaprofileBinary: selfBinary(),
		ConfigFile:         activeFlags.ConfigFile,
		Workdir:            "/",
	}
	for _, pname := range cfg.Names() {
		if profileFilter != "" && pname != profileFilter {
			continue
		}
		p, _ := cfg.Get(pname)
		for _, s := range p.Schedule {
			action := s.Action
			if action == "" {
				action = "backup"
			}
			sc.Schedules = append(sc.Schedules, schedule.Schedule{
				Name:    s.Name,
				At:      s.At,
				Action:  action,
				Profile: pname,
			})
		}
	}
	return sc, nil
}

func renderSchedule(sc schedule.Config, format, output string) error {
	var body string
	files := map[string]string{}
	var err error
	switch strings.ToLower(format) {
	case "crontab":
		body, err = sc.RenderCrontab()
	case "systemd":
		files, err = sc.RenderSystemd(schedule.SystemdOptions{})
	case "launchd":
		files, err = sc.RenderLaunchd(schedule.LaunchdOptions{})
	default:
		return errorf("unknown format %q (use crontab, systemd, launchd)", format)
	}
	if err != nil {
		return err
	}
	if output != "" {
		if format == "crontab" {
			return os.WriteFile(output, []byte(body), 0o644)
		}
		_, err := schedule.WriteFiles(output, files)
		return err
	}
	if body != "" {
		Print("%s", body)
	}
	for name, content := range files {
		Print("=== %s ===", name)
		Print("%s", content)
	}
	return nil
}

func installSchedule(sc schedule.Config, format, dest string, flags *rootFlags) error {
	if format == "" {
		format = detectScheduler()
	}
	if dest == "" {
		switch format {
		case "crontab":
			dest = filepath.Join(os.TempDir(), "kopiaprofile.crontab")
		case "systemd":
			dest = "/etc/systemd/system"
		case "launchd":
			home, _ := os.UserHomeDir()
			dest = filepath.Join(home, "Library", "LaunchAgents")
		}
	}
	switch format {
	case "crontab":
		body, err := sc.RenderCrontab()
		if err != nil {
			return err
		}
		// Append to the user's crontab.
		if err := os.WriteFile(dest, []byte(body), 0o644); err != nil {
			return err
		}
		Print("wrote crontab fragment to %s", dest)
		Print("install with: crontab %s", dest)
	case "systemd":
		files, err := sc.RenderSystemd(schedule.SystemdOptions{})
		if err != nil {
			return err
		}
		written, err := schedule.WriteFiles(dest, files)
		if err != nil {
			return err
		}
		for _, w := range written {
			Print("wrote %s", w)
		}
		activateSystemdUnits(written)
	case "launchd":
		files, err := sc.RenderLaunchd(schedule.LaunchdOptions{})
		if err != nil {
			return err
		}
		written, err := schedule.WriteFiles(dest, files)
		if err != nil {
			return err
		}
		for _, w := range written {
			Print("wrote %s", w)
		}
		Print("load with: launchctl load %s/*.plist", dest)
	default:
		return errorf("unknown format %q", format)
	}
	return nil
}

// activateSystemdUnits runs `systemctl daemon-reload` followed by
// `systemctl enable --now` for every .timer unit among the freshly
// written files. This is what the `install` command's own --help text
// has always claimed it does; previously it only printed the commands
// as a suggestion without ever executing them, which left an installed
// schedule inactive until an operator ran the printed commands by hand.
// A failure here does not roll back the files that were already
// written - it is reported as a warning so the caller can still see
// what was installed and finish activation manually if systemctl is
// unavailable (e.g. containers, non-systemd hosts despite --format=systemd).
func activateSystemdUnits(written []string) {
	if err := runSystemctl("daemon-reload"); err != nil {
		Print("warning: systemctl daemon-reload failed: %v", err)
		return
	}
	Print("ran: systemctl daemon-reload")

	for _, w := range written {
		if !strings.HasSuffix(w, ".timer") {
			continue
		}
		name := filepath.Base(w)
		if err := runSystemctl("enable", "--now", name); err != nil {
			Print("warning: systemctl enable --now %s failed: %v", name, err)
			continue
		}
		Print("ran: systemctl enable --now %s", name)
	}
}

func runSystemctl(args ...string) error {
	cmd := exec.Command("systemctl", args...) // #nosec G204 -- args are "daemon-reload" or "enable"/"--now"/a unit basename we just wrote ourselves
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func isLinux() bool { return os.PathSeparator == '/' && !fileExists("/System/Library") }
func isMac() bool   { return fileExists("/System/Library") }
func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// selfBinary returns the absolute path of the currently running
// kopiaprofile binary, falling back to "kopiaprofile" if it cannot
// be determined.
func selfBinary() string {
	exe, err := os.Executable()
	if err != nil {
		return "kopiaprofile"
	}
	if abs, err := filepath.Abs(exe); err == nil {
		return abs
	}
	return exe
}

// activeFlags is set at the entry of every command. It is read by
// helpers that need the global flag values.
var activeFlags *rootFlags
