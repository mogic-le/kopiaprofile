package schedule

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Schedule is a single scheduled run. Multiple schedules per profile
// are allowed (a profile can have a backup at 3am and a verify at
// 6am, for example).
type Schedule struct {
	// Name is optional and used for documentation/error messages.
	Name string
	// At is a cron-style expression with five fields
	// (minute hour day-of-month month day-of-week). It is parsed by
	// ParseCron into a list of concrete trigger times.
	At string
	// Action is the kopia subcommand to run. Default: "backup".
	Action string
	// Profile is the profile name to invoke.
	Profile string
	// User is the system user to run the schedule as (systemd only).
	// Empty means: keep the default (root if running as root,
	// otherwise the current user).
	User string
}

// Config holds the schedules to render.
type Config struct {
	// KopiaBinary is the full path to the `kopia` binary. Required
	// for systemd and launchd (they need an absolute path because
	// they don't inherit the operator's $PATH).
	KopiaBinary string
	// KopiaprofileBinary is the full path to the `kopiaprofile`
	// binary. Same rationale as KopiaBinary.
	KopiaprofileBinary string
	// ConfigFile is the path to the kopiaprofile configuration.
	ConfigFile string
	// Schedules is the list of schedules to render.
	Schedules []Schedule
	// Workdir is the working directory for the scheduled command.
	// Defaults to / (systemd) or the user's home (cron/launchd).
	Workdir string
	// Env holds extra environment variables for the command.
	Env map[string]string
}

// CronSchedule is the parsed form of a 5-field cron expression.
type CronSchedule struct {
	Minute     []int // 0-59
	Hour       []int // 0-23
	DayOfMonth []int // 1-31
	Month      []int // 1-12
	DayOfWeek  []int // 0-6 (0 = Sunday)
}

// ParseCron parses a standard 5-field cron expression. We support
// the common subset: exact values, "*" (any), "*/N" (every N),
// ranges "A-B", and comma-separated lists. Day-of-week and
// day-of-month are OR-ed (cron semantics). Returns a CronSchedule
// that can be inspected, or an error if the expression is invalid.
func ParseCron(expr string) (CronSchedule, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return CronSchedule{}, fmt.Errorf("cron expression must have 5 fields, got %d: %q", len(fields), expr)
	}
	cs := CronSchedule{}
	var err error
	if cs.Minute, err = parseField(fields[0], 0, 59); err != nil {
		return cs, fmt.Errorf("minute: %w", err)
	}
	if cs.Hour, err = parseField(fields[1], 0, 23); err != nil {
		return cs, fmt.Errorf("hour: %w", err)
	}
	if cs.DayOfMonth, err = parseField(fields[2], 1, 31); err != nil {
		return cs, fmt.Errorf("day-of-month: %w", err)
	}
	if cs.Month, err = parseField(fields[3], 1, 12); err != nil {
		return cs, fmt.Errorf("month: %w", err)
	}
	if cs.DayOfWeek, err = parseField(fields[4], 0, 6); err != nil {
		return cs, fmt.Errorf("day-of-week: %w", err)
	}
	return cs, nil
}

func parseField(s string, min, max int) ([]int, error) {
	if s == "" {
		return nil, fmt.Errorf("empty field")
	}
	var out []int
	for _, part := range strings.Split(s, ",") {
		vals, err := parsePart(part, min, max)
		if err != nil {
			return nil, err
		}
		// If the user wrote `*` (or `min-max` with the same range
		// as min..max), return nil so the renderer emits a single
		// "*" instead of an explicit list of every possible value.
		if len(vals) == max-min+1 {
			return nil, nil
		}
		out = append(out, vals...)
	}
	sort.Ints(out)
	return dedup(out), nil
}

func parsePart(s string, min, max int) ([]int, error) {
	step := 1
	if i := strings.Index(s, "/"); i >= 0 {
		var err error
		step, err = parseInt(strings.TrimPrefix(s[i:], "/"))
		if err != nil {
			return nil, fmt.Errorf("step: %w", err)
		}
		if step <= 0 {
			return nil, fmt.Errorf("step must be > 0")
		}
		s = s[:i]
	}
	if s == "*" {
		s = ""
	}
	var lo, hi int
	if s == "" {
		lo, hi = min, max
	} else if i := strings.Index(s, "-"); i >= 0 {
		var err error
		lo, err = parseInt(s[:i])
		if err != nil {
			return nil, fmt.Errorf("range lo: %w", err)
		}
		hi, err = parseInt(s[i+1:])
		if err != nil {
			return nil, fmt.Errorf("range hi: %w", err)
		}
	} else {
		v, err := parseInt(s)
		if err != nil {
			return nil, err
		}
		if step != 1 {
			return nil, fmt.Errorf("step without range")
		}
		if v < min || v > max {
			return nil, fmt.Errorf("value %d out of [%d,%d]", v, min, max)
		}
		return []int{v}, nil
	}
	if lo < min || hi > max || lo > hi {
		return nil, fmt.Errorf("range [%d,%d] invalid for [%d,%d]", lo, hi, min, max)
	}
	var out []int
	for v := lo; v <= hi; v += step {
		out = append(out, v)
	}
	return out, nil
}

func parseInt(s string) (int, error) {
	var v int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("not a number: %q", s)
		}
		v = v*10 + int(c-'0')
	}
	return v, nil
}

func dedup(in []int) []int {
	if len(in) <= 1 {
		return in
	}
	out := in[:1]
	for _, v := range in[1:] {
		if v != out[len(out)-1] {
			out = append(out, v)
		}
	}
	return out
}

// NextTime returns the next time the cron expression triggers at or
// after `from`. Returns time.Time{} (zero) if no match is found in
// the next 4 years. This is a deliberately small scope — we only
// need enough to verify the cron expression and to write it into
// launchd/systemd configs.
//
// Field semantics follow standard cron: a nil field matches any
// value (i.e. `*`). DayOfMonth and DayOfWeek are OR-ed: the trigger
// fires if EITHER matches, per the Vixie cron spec.
func (c CronSchedule) NextTime(from time.Time) time.Time {
	for d := 0; d < 366*4; d++ {
		t := from.AddDate(0, 0, d)
		if !c.monthMatches(int(t.Month())) {
			continue
		}
		// cron day-of-week: 0 = Sunday. In Go: Sunday = 0.
		if !c.dayOfWeekMatches(int(t.Weekday())) {
			continue
		}
		// day-of-month: OR with day-of-week (cron semantics).
		if !c.dayOfMonthMatches(t.Day()) {
			continue
		}
		for _, h := range c.allHours() {
			for _, m := range c.allMinutes() {
				candidate := time.Date(t.Year(), t.Month(), t.Day(), h, m, 0, 0, t.Location())
				if candidate.After(from) || candidate.Equal(from) {
					return candidate
				}
			}
		}
	}
	return time.Time{}
}

// fieldMatches returns true if the value matches the field. An
// empty/nil field matches everything.
func (c CronSchedule) fieldMatches(field []int, v int) bool {
	if len(field) == 0 {
		return true
	}
	return contains(field, v)
}

func (c CronSchedule) monthMatches(v int) bool      { return c.fieldMatches(c.Month, v) }
func (c CronSchedule) dayOfWeekMatches(v int) bool  { return c.fieldMatches(c.DayOfWeek, v) }
func (c CronSchedule) dayOfMonthMatches(v int) bool { return c.fieldMatches(c.DayOfMonth, v) }
func (c CronSchedule) allHours() []int {
	if len(c.Hour) == 0 {
		return []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23}
	}
	return c.Hour
}
func (c CronSchedule) allMinutes() []int {
	if len(c.Minute) == 0 {
		return []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40, 41, 42, 43, 44, 45, 46, 47, 48, 49, 50, 51, 52, 53, 54, 55, 56, 57, 58, 59}
	}
	return c.Minute
}

func contains(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
