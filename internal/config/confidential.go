package config

import (
	"regexp"
	"strings"
)

// MaskSecret replaces any string in s that matches the "looks like a
// secret" heuristics with a fixed-length placeholder. Use this when
// building log lines that may contain environment values, repository
// fields, password command outputs, etc.
//
// The placeholder is intentionally 8 asterisks to make it visually
// obvious without hiding the line length too much.
const maskPlaceholder = "********"

// Patterns based on Resticprofile's config/confidential.go. We re-use the
// same regex family so that operators moving from resticprofile find the
// behaviour familiar.
// patKey matches KEY=VALUE patterns where KEY contains a sensitive
// suffix. The KEY may be just the suffix (e.g. "PASSWORD=value") or
// longer (e.g. "AWS_SECRET_ACCESS_KEY=value"). A separator of `=`, `:`
// or whitespace is required between KEY and VALUE.
var patKey = regexp.MustCompile(`(?i)([A-Z_][A-Z0-9_]*(?:[_-](?:KEY|TOKEN|SECRET|PASSWORD|AUTH|PASSWD|PWD))+|KEY|TOKEN|SECRET|PASSWORD|PASSWD|PWD)([ \t]*[:=][ \t]*)([^\s"',;}]+)`)

// patURL matches user:password@host inside a URL.
var patURL = regexp.MustCompile(`([a-z][a-z0-9+.-]*://[^\s"'\\]*?:)([^\s"'@\\]+)(@)`)

// patAccessID matches access-key-id: VALUE style entries.
var patAccessID = regexp.MustCompile(`(?i)(access[-_]?key[-_]?id["']?\s*[:=]\s*["']?)([^"'\s,}]+)`)

// MaskSecrets walks the input string and returns a copy with secrets
// replaced by a mask. The function is intentionally simple and
// allocation-friendly; it is safe to call on every log line.
func MaskSecrets(s string) string {
	if s == "" {
		return s
	}
	out := patKey.ReplaceAllString(s, "${1}${2}"+maskPlaceholder)
	out = patURL.ReplaceAllString(out, "${1}"+maskPlaceholder+"${3}")
	out = patAccessID.ReplaceAllString(out, "${1}"+maskPlaceholder)
	return out
}

// MaskMap returns a copy of m where the values of any key that looks
// like a secret have been replaced with the mask placeholder.
func MaskMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		if looksSecret(k) {
			out[k] = maskPlaceholder
		} else {
			out[k] = MaskSecrets(v)
		}
	}
	return out
}

func looksSecret(name string) bool {
	upper := strings.ToUpper(name)
	for _, sub := range []string{"KEY", "TOKEN", "SECRET", "PASSWORD", "PASSWD", "PWD", "AUTH"} {
		if strings.Contains(upper, sub) {
			return true
		}
	}
	return false
}
