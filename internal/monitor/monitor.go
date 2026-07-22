// Package monitor provides status-file and Prometheus push
// exporters for kopiaprofile runs.
//
// The MVP supports two outputs:
//
//  1. A JSON status file written after every run. The file is
//     human-readable and machine-parseable; it includes the
//     profile name, action, exit code, start/end timestamps,
//     duration, the list of hook results and any error message.
//
//  2. A Prometheus push to a Pushgateway. Each run is exported as
//     a single "kopia_run" gauge (1 = success, 0 = failure) with
//     labels for profile and action, plus a "kopia_run_duration"
//     gauge for the elapsed time. We do not include every
//     kopia-emitted counter because those belong in a separate
//     kopia server metrics export.
//
// The output destinations are configured per-profile (or globally)
// under `monitor:`. See examples/full-demo.yaml for the schema.
package monitor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mogic-le/kopiaprofile/internal/types"
)

// Status is the JSON document written to the status file.
type Status struct {
	Profile  string        `json:"profile"`
	Action   string        `json:"action"`
	ExitCode int           `json:"exit_code"`
	StartAt  time.Time     `json:"start_at"`
	EndAt    time.Time     `json:"end_at"`
	Duration time.Duration `json:"duration_ns"`
	Hooks    []HookStatus  `json:"hooks,omitempty"`
	Error    string        `json:"error,omitempty"`
	Hostname string        `json:"hostname"`
	Kopia    *KopiaStatus  `json:"kopia,omitempty"`
}

// HookStatus records the outcome of a single run-* hook.
type HookStatus struct {
	Phase    string `json:"phase"`
	Command  string `json:"command"`
	ExitCode int    `json:"exit_code"`
	Error    string `json:"error,omitempty"`
}

// KopiaStatus is the (masked) summary of the kopia invocation.
type KopiaStatus struct {
	ExitCode   int    `json:"exit_code"`
	StderrTail string `json:"stderr_tail,omitempty"`
	Argv       string `json:"argv"`
}

// Config is the monitor configuration block.
type Config struct {
	StatusFile  string
	PushGateway string
	PushLabels  map[string]string
	Timeout     time.Duration
}

// Manager runs the configured monitors. The zero value is a
// no-op manager; create one with New().
type Manager struct {
	mu      sync.Mutex
	configs []Config
}

// New creates a Manager from a list of Configs. Duplicate
// StatusFiles are silently collapsed.
func New(configs ...Config) *Manager {
	seen := make(map[string]bool)
	m := &Manager{}
	for _, c := range configs {
		if c.StatusFile != "" && !seen[c.StatusFile] {
			seen[c.StatusFile] = true
			m.configs = append(m.configs, c)
		}
	}
	return m
}

// Add registers an additional config at runtime.
func (m *Manager) Add(c Config) {
	if c.StatusFile == "" && c.PushGateway == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.configs = append(m.configs, c)
}

// Run records a run result against every registered monitor.
// Errors are logged via the supplied logger; they do not propagate.
func (m *Manager) Run(ctx context.Context, pr *types.RunResult, log Logger) {
	if pr == nil {
		return
	}
	st := toStatus(pr)
	for _, c := range m.configs {
		if c.StatusFile != "" {
			if err := writeStatusFile(c.StatusFile, st); err != nil {
				log.Errorf("monitor: writing status file %q: %v", c.StatusFile, err)
			}
		}
		if c.PushGateway != "" {
			if err := pushPrometheus(ctx, c, st, log); err != nil {
				log.Errorf("monitor: push to %q: %v", c.PushGateway, err)
			}
		}
	}
}

func toStatus(pr *types.RunResult) Status {
	st := Status{
		Profile:  pr.Profile,
		Action:   pr.Action,
		ExitCode: pr.ExitCode,
		StartAt:  pr.StartAt,
		EndAt:    pr.EndAt,
		Duration: pr.Duration,
		Hooks:    make([]HookStatus, 0, len(pr.Hooks)),
	}
	st.Hostname = pr.Hostname
	st.Error = pr.Error
	for _, h := range pr.Hooks {
		hs := HookStatus{
			Phase:    h.Phase,
			Command:  h.Command,
			ExitCode: h.ExitCode,
			Error:    h.Error,
		}
		st.Hooks = append(st.Hooks, hs)
	}
	if pr.Kopia != nil {
		st.Kopia = &KopiaStatus{
			ExitCode:   pr.Kopia.ExitCode,
			StderrTail: pr.Kopia.StderrTail,
			Argv:       pr.Kopia.Argv,
		}
	}
	return st
}

// FromResult is a convenience for converting from the profile.Result
// type into a types.RunResult.
func FromResult(profileName, action string, exitCode int, startAt, endAt time.Time, err error, hostname string, hooks []types.RunHookResult, kopia *types.KopiaResult) *types.RunResult {
	pr := &types.RunResult{
		Profile:  profileName,
		Action:   action,
		ExitCode: exitCode,
		StartAt:  startAt,
		EndAt:    endAt,
		Duration: endAt.Sub(startAt),
		Hostname: hostname,
		Hooks:    hooks,
		Kopia:    kopia,
	}
	if err != nil {
		pr.Error = err.Error()
	}
	return pr
}

func writeStatusFile(path string, st Status) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".status-*.json.tmp")
	if err != nil {
		return err
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()
	// os.CreateTemp always creates with mode 0600 regardless of umask, and
	// a rename doesn't change that - the status file otherwise ends up
	// unreadable by a non-root monitoring user (observed live: kopia's
	// backup-status.json at 0600 vs. restic's equivalent at 0644). The
	// file only ever holds run metadata (profile name, action, exit code,
	// timestamps) - never a secret - so a world-readable status file is
	// intentional here, matching resticprofile's own status file.
	if err := tmp.Chmod(0o644); err != nil {
		return err
	}
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(st); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Logger is the small logging interface monitor uses.
type Logger interface {
	Errorf(format string, args ...interface{})
}

func pushPrometheus(ctx context.Context, c Config, st Status, log Logger) error {
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	body := renderPromPayload(st, c.PushLabels)
	u, err := url.Parse(c.PushGateway)
	if err != nil {
		return fmt.Errorf("parsing push URL: %w", err)
	}
	jobName := "kopiaprofile"
	if v := u.Query().Get("job"); v != "" {
		jobName = v
		u.RawQuery = ""
	}
	endpoint := fmt.Sprintf("%s://%s%s/metrics/job/%s",
		u.Scheme, u.Host, u.Path, jobName)
	if strings.HasSuffix(u.Path, "/") {
		endpoint = fmt.Sprintf("%s://%smetrics/job/%s", u.Scheme, u.Host, jobName)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/plain; version=0.0.4")
	cli := &http.Client{Timeout: timeout}
	resp, err := cli.Do(req)
	if err != nil {
		return err
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("push gateway returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// renderPromPayload turns a Status into a Prometheus text-format
// payload. Two metrics per run:
//
//   - kopia_run{...} 1|0
//   - kopia_run_duration_seconds{...} <duration>
//
// Plus a kopia_run_errors_total counter if the run failed.
func renderPromPayload(st Status, baseLabels map[string]string) []byte {
	labels := mergeLabels(baseLabels, map[string]string{
		"profile": st.Profile,
		"action":  st.Action,
		"host":    st.Hostname,
	})
	var b strings.Builder
	success := 0
	if st.ExitCode == 0 && st.Error == "" {
		success = 1
	}
	fmt.Fprintf(&b, "kopia_run{")
	writeLabels(&b, labels)
	fmt.Fprintf(&b, "} %d\n", success)
	fmt.Fprintf(&b, "kopia_run_duration_seconds{")
	writeLabels(&b, labels)
	fmt.Fprintf(&b, "} %f\n", st.Duration.Seconds())
	if st.ExitCode != 0 || st.Error != "" {
		errLabels := mergeLabels(labels, map[string]string{
			"exit_code": fmt.Sprintf("%d", st.ExitCode),
		})
		fmt.Fprintf(&b, "kopia_run_errors_total{")
		writeLabels(&b, errLabels)
		fmt.Fprintf(&b, "} 1\n")
	}
	return []byte(b.String())
}

func writeLabels(b *strings.Builder, labels map[string]string) {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(b, "%s=%q", k, labels[k])
	}
}

func mergeLabels(a, b map[string]string) map[string]string {
	out := make(map[string]string, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
