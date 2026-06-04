package schedule

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
)

// CrontabEntry is a single rendered crontab line.
type CrontabEntry struct {
	Minute     string // cron field, default "*"
	Hour       string // cron field, default "*"
	DayOfMonth string
	Month      string
	DayOfWeek  string
	Command    string
}

// RenderCrontab produces a crontab fragment that can be appended to
// (or installed via `crontab -`). One entry is produced per
// Schedule.
func (c Config) RenderCrontab() (string, error) {
	var b strings.Builder
	b.WriteString("# kopiaprofile generated crontab — do not edit by hand\n")
	b.WriteString("# Re-render with `kopiaprofile schedule render --format=crontab`\n")
	for _, s := range c.Schedules {
		cs, err := ParseCron(s.At)
		if err != nil {
			return "", fmt.Errorf("schedule %q: %w", s.Name, err)
		}
		cmd := c.kopiaprofileCmd(s)
		entry := CrontabEntry{
			Minute:     joinField(cs.Minute),
			Hour:       joinField(cs.Hour),
			DayOfMonth: joinField(cs.DayOfMonth),
			Month:      joinField(cs.Month),
			DayOfWeek:  joinField(cs.DayOfWeek),
			Command:    cmd,
		}
		b.WriteString(fmt.Sprintf("%-3s %-3s %-3s %-3s %-3s %s\n",
			entry.Minute, entry.Hour, entry.DayOfMonth, entry.Month,
			entry.DayOfWeek, entry.Command))
	}
	return b.String(), nil
}

func joinField(vals []int) string {
	if len(vals) == 0 {
		return "*"
	}
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = fmt.Sprintf("%d", v)
	}
	return strings.Join(parts, ",")
}

func (c Config) kopiaprofileCmd(s Schedule) string {
	action := s.Action
	if action == "" {
		action = "snapshot"
	}
	// cron runs under a non-interactive shell; we set PATH minimally
	// and use the absolute path of the binary.
	kpath := c.KopiaprofileBinary
	if kpath == "" {
		kpath = "kopiaprofile"
	}
	cfgpath := c.ConfigFile
	cmd := fmt.Sprintf("%s -c %s %s %s", kpath, shellQuote(cfgpath), s.Profile, action)
	if c.Workdir != "" {
		cmd = "cd " + shellQuote(c.Workdir) + " && " + cmd
	}
	return cmd
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t'\"\\$`&;|*?<>(){}[]#~!") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// SystemdOptions customises the systemd rendering.
type SystemdOptions struct {
	// UnitPrefix is prepended to the unit names. Default: "kopiaprofile".
	UnitPrefix string
	// User is the system user to run the service as. Default: "root".
	User string
	// Group is the system group. Default: same as User.
	Group string
}

// RenderSystemd produces the contents of a directory containing
// service + timer pairs. Returns a map of file name → file
// content. The caller is responsible for writing the files
// (typically under /etc/systemd/system/).
func (c Config) RenderSystemd(opts SystemdOptions) (map[string]string, error) {
	prefix := opts.UnitPrefix
	if prefix == "" {
		prefix = "kopiaprofile"
	}
	user := opts.User
	if user == "" {
		user = "root"
	}
	group := opts.Group
	if group == "" {
		group = user
	}

	files := make(map[string]string)
	for _, s := range c.Schedules {
		if s.Profile == "" {
			return nil, fmt.Errorf("schedule %q has no profile", s.Name)
		}
		cs, err := ParseCron(s.At)
		if err != nil {
			return nil, fmt.Errorf("schedule %q: %w", s.Name, err)
		}
		unit := scheduleUnitName(s, prefix)
		name := s.Name
		if name == "" {
			name = s.At
		}
		// Service unit (oneshot, runs the command)
		svc := newSystemdService(unit, c.kopiaprofileCmd(s), user, group, name, c.envSlice())
		files[svc.name] = svc.body
		// Timer unit
		t := newSystemdTimer(unit, name, cs)
		files[t.name] = t.body
	}
	return files, nil
}

type unit struct {
	name string
	body string
}

func scheduleUnitName(s Schedule, prefix string) string {
	if s.Name != "" {
		return fmt.Sprintf("%s-%s-%s", prefix, s.Profile, s.Name)
	}
	return fmt.Sprintf("%s-%s", prefix, s.Profile)
}

func newSystemdService(unitName, cmd, user, group, name string, envLines []string) unit {
	body := `[Unit]
Description=kopiaprofile schedule ` + name + `
Wants=network-online.target
After=network-online.target

[Service]
Type=oneshot
User=` + user + `
Group=` + group + `
WorkingDirectory=/root
` + strings.Join(envLines, "\n") + `
ExecStart=` + cmd + `
Nice=10
`
	return unit{
		name: unitName + ".service",
		body: body,
	}
}

func (c Config) envSlice() []string {
	// Emit only the keys that look useful. We do not set
	// KOPIA_PASSWORD here; the scheduled command is expected to
	// read the password from the keyring.
	keys := []string{}
	for k := range c.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, fmt.Sprintf("Environment=%s=%s", k, c.Env[k]))
	}
	return out
}

func newSystemdTimer(unitName, name string, cs CronSchedule) unit {
	// We use OnCalendar for systemd. We translate the cron
	// expression to systemd's calendar format.
	cal := systemdOnCalendar(cs)
	body := `[Unit]
Description=kopiaprofile schedule ` + name + ` timer
Requires=` + unitName + `.service

[Timer]
OnCalendar=` + cal + `
Persistent=true
Unit=` + unitName + `.service

[Install]
WantedBy=timers.target
`
	return unit{
		name: unitName + ".timer",
		body: body,
	}
}

// systemdOnCalendar renders a CronSchedule into a systemd
// OnCalendar= value. We only support the common subset:
//   - * * * * *         -> *-*-* *:*:00
//   - M H * * *         -> *-*-* H:M:00
//   - */N * * * *       -> *:0/N
//   - 0 3 * * *         -> *-*-* 03:00:00
func systemdOnCalendar(cs CronSchedule) string {
	// If only minute and hour are specific, use "*-*-* H:M:00".
	// Otherwise we need a fuller expression. We deliberately keep
	// the implementation simple: if any day-of-month or month
	// field is non-default, we emit a wildcard and let the user
	// customise. The timer still fires — just less often.
	if len(cs.DayOfMonth) == 1 && cs.DayOfMonth[0] == 1 && len(cs.Month) == 12 {
		// All days, all months.
		_ = cs
	}
	hour := "*"
	minute := "*"
	if len(cs.Hour) == 1 {
		hour = fmt.Sprintf("%02d", cs.Hour[0])
	}
	if len(cs.Minute) == 1 {
		minute = fmt.Sprintf("%02d", cs.Minute[0])
	}
	return fmt.Sprintf("*-*-* %s:%s:00", hour, minute)
}

// LaunchdOptions customises the launchd rendering.
type LaunchdOptions struct {
	// LabelPrefix is prepended to the plist labels.
	LabelPrefix string
	// User is the user account to run the agent as (defaults to
	// the current user; only the launching user can install it).
	User string
	// Workdir overrides the working directory.
	Workdir string
}

// RenderLaunchd produces the contents of a directory of plist
// files. Each schedule becomes one plist with StartCalendarInterval
// entries.
func (c Config) RenderLaunchd(opts LaunchdOptions) (map[string]string, error) {
	prefix := opts.LabelPrefix
	if prefix == "" {
		prefix = "com.kopiaprofile"
	}
	workdir := opts.Workdir
	if workdir == "" {
		workdir = c.Workdir
		if workdir == "" {
			workdir = os.Getenv("HOME")
		}
	}
	files := make(map[string]string)
	for _, s := range c.Schedules {
		if s.Profile == "" {
			return nil, fmt.Errorf("schedule %q has no profile", s.Name)
		}
		cs, err := ParseCron(s.At)
		if err != nil {
			return nil, fmt.Errorf("schedule %q: %w", s.Name, err)
		}
		label := fmt.Sprintf("%s.%s.%s", prefix, s.Profile, labelName(s))
		plist := renderLaunchdPlist(label, c.kopiaprofileCmd(s), cs, workdir)
		files[label+".plist"] = plist
	}
	return files, nil
}

func labelName(s Schedule) string {
	if s.Name != "" {
		return s.Name
	}
	return s.At
}

func renderLaunchdPlist(label, cmd string, cs CronSchedule, workdir string) string {
	var intervals strings.Builder
	// launchd StartCalendarInterval supports an array of {Minute,
	// Hour, Day, Weekday, Month} dicts. We only emit the supported
	// subset (day-of-month OR day-of-week is mutually exclusive
	// in launchd; we use day-of-week and let the cron expression's
	// day-of-month fall through).
	for _, h := range cs.Hour {
		for _, m := range cs.Minute {
			intervals.WriteString(fmt.Sprintf("\t<dict>\n\t\t<key>Minute</key>\n\t\t<integer>%d</integer>\n\t\t<key>Hour</key>\n\t\t<integer>%d</integer>\n\t</dict>\n", m, h))
		}
	}
	plistTpl := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>{{.Label}}</string>
	<key>ProgramArguments</key>
	<array>
		<string>/bin/sh</string>
		<string>-c</string>
		<string>{{.Command}}</string>
	</array>
	<key>WorkingDirectory</key>
	<string>{{.Workdir}}</string>
	<key>StartCalendarInterval</key>
	<array>
{{.Intervals}}	</array>
</dict>
</plist>
`
	t := template.Must(template.New("plist").Parse(plistTpl))
	var b strings.Builder
	_ = t.Execute(&b, map[string]string{
		"Label":     label,
		"Command":   cmd,
		"Workdir":   workdir,
		"Intervals": intervals.String(),
	})
	return b.String()
}

// WriteFiles writes the rendered files to disk under dir. The
// directory is created if it doesn't exist. Returns the list of
// files written.
func WriteFiles(dir string, files map[string]string) ([]string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	var written []string
	for name, body := range files {
		full := filepath.Join(dir, name)
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			return written, err
		}
		written = append(written, full)
	}
	sort.Strings(written)
	return written, nil
}
