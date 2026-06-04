package schedule

import (
	"strings"
	"testing"
	"time"
)

func TestParseCronSimple(t *testing.T) {
	cs, err := ParseCron("0 3 * * *")
	if err != nil {
		t.Fatal(err)
	}
	if len(cs.Minute) != 1 || cs.Minute[0] != 0 {
		t.Errorf("minute: %v", cs.Minute)
	}
	if len(cs.Hour) != 1 || cs.Hour[0] != 3 {
		t.Errorf("hour: %v", cs.Hour)
	}
	if cs.DayOfMonth != nil {
		t.Errorf("day-of-month should be nil (=*): %v", cs.DayOfMonth)
	}
	if cs.Month != nil {
		t.Errorf("month should be nil (=*): %v", cs.Month)
	}
	if cs.DayOfWeek != nil {
		t.Errorf("day-of-week should be nil (=*): %v", cs.DayOfWeek)
	}
}

func TestParseCronStep(t *testing.T) {
	cs, err := ParseCron("*/15 * * * *")
	if err != nil {
		t.Fatal(err)
	}
	if len(cs.Minute) != 4 {
		t.Errorf("every 15: got %v", cs.Minute)
	}
}

func TestParseCronRange(t *testing.T) {
	cs, err := ParseCron("0 9-17 * * 1-5")
	if err != nil {
		t.Fatal(err)
	}
	if len(cs.Hour) != 9 {
		t.Errorf("9-17: got %v", cs.Hour)
	}
	if len(cs.DayOfWeek) != 5 {
		t.Errorf("1-5: got %v", cs.DayOfWeek)
	}
}

func TestParseCronCommaList(t *testing.T) {
	cs, err := ParseCron("0,15,30,45 * * * *")
	if err != nil {
		t.Fatal(err)
	}
	if len(cs.Minute) != 4 {
		t.Errorf("comma list: got %v", cs.Minute)
	}
}

func TestParseCronInvalid(t *testing.T) {
	cases := []string{
		"",
		"0 3",
		"60 3 * * *",  // minute out of range
		"0 24 * * *",  // hour out of range
		"a b c d e",   // non-numeric
		"*/0 * * * *", // step 0
	}
	for _, c := range cases {
		if _, err := ParseCron(c); err == nil {
			t.Errorf("expected error for %q, got nil", c)
		}
	}
}

func TestRenderCrontab(t *testing.T) {
	c := Config{
		KopiaprofileBinary: "/usr/local/bin/kopiaprofile",
		ConfigFile:         "/etc/kopiaprofile.yaml",
		Schedules: []Schedule{
			{Name: "nightly", At: "0 3 * * *", Action: "snapshot", Profile: "home"},
			{Name: "verify", At: "0 6 * * 0", Action: "verify", Profile: "home"},
		},
	}
	out, err := c.RenderCrontab()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "0   3   *   *   *") {
		t.Errorf("crontab missing nightly entry: %q", out)
	}
	if !strings.Contains(out, "0   6   *   *   0") {
		t.Errorf("crontab missing verify entry: %q", out)
	}
	if !strings.Contains(out, "/usr/local/bin/kopiaprofile") {
		t.Errorf("crontab missing binary path: %q", out)
	}
}

func TestRenderSystemd(t *testing.T) {
	c := Config{
		KopiaprofileBinary: "/usr/local/bin/kopiaprofile",
		ConfigFile:         "/etc/kopiaprofile.yaml",
		Schedules: []Schedule{
			{Name: "nightly", At: "0 3 * * *", Action: "snapshot", Profile: "home"},
		},
	}
	files, err := c.RenderSystemd(SystemdOptions{User: "backup", Group: "backup"})
	if err != nil {
		t.Fatal(err)
	}
	svc, ok := files["kopiaprofile-home-nightly.service"]
	if !ok {
		t.Fatalf("missing service file, got: %v", files)
	}
	if !strings.Contains(svc, "User=backup") {
		t.Errorf("service: %q", svc)
	}
	if !strings.Contains(svc, "OnCalendar=*-*-* 03:00:00") {
		// Timer file is separate
	}
	timer, ok := files["kopiaprofile-home-nightly.timer"]
	if !ok {
		t.Fatalf("missing timer file")
	}
	if !strings.Contains(timer, "OnCalendar=*-*-* 03:00:00") {
		t.Errorf("timer: %q", timer)
	}
}

func TestRenderLaunchd(t *testing.T) {
	c := Config{
		KopiaprofileBinary: "/usr/local/bin/kopiaprofile",
		ConfigFile:         "/etc/kopiaprofile.yaml",
		Schedules: []Schedule{
			{Name: "nightly", At: "0 3 * * *", Action: "snapshot", Profile: "home"},
		},
	}
	files, err := c.RenderLaunchd(LaunchdOptions{Workdir: "/tmp"})
	if err != nil {
		t.Fatal(err)
	}
	plist, ok := files["com.kopiaprofile.home.nightly.plist"]
	if !ok {
		t.Fatalf("missing plist, got: %v", files)
	}
	if !strings.Contains(plist, "<string>com.kopiaprofile.home.nightly</string>") {
		t.Errorf("plist: %q", plist)
	}
	if !strings.Contains(plist, "<integer>3</integer>") {
		t.Errorf("plist hour: %q", plist)
	}
}

func TestNextTime(t *testing.T) {
	cs, err := ParseCron("0 3 * * *")
	if err != nil {
		t.Fatal(err)
	}
	from := timeMustParse("2025-01-01T00:00:00Z")
	next := cs.NextTime(from)
	if next.Hour() != 3 || next.Minute() != 0 {
		t.Errorf("next time: %v", next)
	}
	if next.Day() != 1 {
		t.Errorf("next time should be same day, got %v", next)
	}
}

func timeMustParse(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}
