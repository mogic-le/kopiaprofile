package config

import (
	"bytes"
	"fmt"
	"os"
	"os/user"
	"strings"
	"text/template"
	"time"
)

// TemplateContext is the set of variables available to Go templates in
// kopiaprofile configuration files.
type TemplateContext struct {
	Hostname string
	Profile  TemplateProfile
	User     TemplateUser
	Env      map[string]string
	Now      time.Time
}

// TemplateProfile is a profile-shaped subset of fields exposed to
// templates. It deliberately does not embed the full Profile struct so
// that template authors do not accidentally rely on internal fields.
type TemplateProfile struct {
	Name        string
	Description string
}

// TemplateUser holds the result of os/user.Current().
type TemplateUser struct {
	Username string
	Uid      string
	Gid      string
}

// NewTemplateContext returns a TemplateContext populated with hostname,
// user info, the entire process environment and the current time.
func NewTemplateContext(profile TemplateProfile) TemplateContext {
	host, _ := os.Hostname()
	tu := TemplateUser{Username: "unknown", Uid: "0", Gid: "0"}
	if u, err := user.Current(); err == nil {
		tu.Username = u.Username
		tu.Uid = u.Uid
		tu.Gid = u.Gid
	}
	return TemplateContext{
		Hostname: host,
		Profile:  profile,
		User:     tu,
		Env:      envToMap(),
		Now:      time.Now(),
	}
}

func envToMap() map[string]string {
	out := make(map[string]string, len(os.Environ()))
	for _, kv := range os.Environ() {
		if idx := strings.IndexByte(kv, '='); idx > 0 {
			out[kv[:idx]] = kv[idx+1:]
		}
	}
	return out
}

// templateFuncMap returns the funcMap used to evaluate templates. Includes
// a small set of safe helpers (env-or-default, path-join, upper, lower)
// beyond the standard library defaults.
func templateFuncMap() template.FuncMap {
	return template.FuncMap{
		"default": func(def, val interface{}) interface{} {
			if val == nil {
				return def
			}
			if s, ok := val.(string); ok && s == "" {
				return def
			}
			return val
		},
		"envOr": func(key, def string) string {
			if v, ok := os.LookupEnv(key); ok {
				return v
			}
			return def
		},
		"upper": strings.ToUpper,
		"lower": strings.ToLower,
		"join":  strings.Join,
	}
}

// ExpandTemplates walks a profile (and any nested struct fields that
// support templates) and returns a copy with all templatable strings
// rendered. Currently expands: Description, CacheDir, EnvFile, KopiaBinary,
// KopiaConfigDir, every Repository string field, Backup, Lock, Log, every
// Run* hook, Password.File and Password.Command.
//
// Strings that contain no template directives are returned unchanged, so
// the fast path is allocation-free.
func ExpandTemplates(p Profile) (Profile, error) {
	tctx := NewTemplateContext(TemplateProfile{
		Name:        p.Name,
		Description: p.Description,
	})
	render := func(s string) (string, error) {
		if !strings.Contains(s, "{{") {
			return s, nil
		}
		t, err := template.New("inline").Funcs(templateFuncMap()).Parse(s)
		if err != nil {
			return "", fmt.Errorf("parse template %q: %w", s, err)
		}
		var buf bytes.Buffer
		if err := t.Execute(&buf, tctx); err != nil {
			return "", fmt.Errorf("execute template %q: %w", s, err)
		}
		return buf.String(), nil
	}

	var err error
	if p.Description, err = render(p.Description); err != nil {
		return p, err
	}
	if p.CacheDir, err = render(p.CacheDir); err != nil {
		return p, err
	}
	if p.EnvFile, err = render(p.EnvFile); err != nil {
		return p, err
	}
	if p.KopiaBinary, err = render(p.KopiaBinary); err != nil {
		return p, err
	}
	if p.KopiaConfigDir, err = render(p.KopiaConfigDir); err != nil {
		return p, err
	}
	if p.Lock.Path, err = render(p.Lock.Path); err != nil {
		return p, err
	}
	if p.Log.Dir, err = render(p.Log.Dir); err != nil {
		return p, err
	}
	if p.RunBefore, err = render(p.RunBefore); err != nil {
		return p, err
	}
	if p.RunAfter, err = render(p.RunAfter); err != nil {
		return p, err
	}
	if p.RunAfterFail, err = render(p.RunAfterFail); err != nil {
		return p, err
	}
	if p.RunFinally, err = render(p.RunFinally); err != nil {
		return p, err
	}
	if p.Password.File, err = render(p.Password.File); err != nil {
		return p, err
	}
	if p.Password.Command, err = render(p.Password.Command); err != nil {
		return p, err
	}
	if p.Password.EnvVar, err = render(p.Password.EnvVar); err != nil {
		return p, err
	}

	// Repository
	repo := p.Repository
	repo.Bucket, err = render(repo.Bucket)
	if err != nil {
		return p, err
	}
	repo.Endpoint, err = render(repo.Endpoint)
	if err != nil {
		return p, err
	}
	repo.Region, err = render(repo.Region)
	if err != nil {
		return p, err
	}
	repo.AccessKey, err = render(repo.AccessKey)
	if err != nil {
		return p, err
	}
	repo.SecretKey, err = render(repo.SecretKey)
	if err != nil {
		return p, err
	}
	repo.Prefix, err = render(repo.Prefix)
	if err != nil {
		return p, err
	}
	repo.Path, err = render(repo.Path)
	if err != nil {
		return p, err
	}
	p.Repository = repo

	// Env
	for k, v := range p.Env {
		newV, err := render(v)
		if err != nil {
			return p, err
		}
		p.Env[k] = newV
	}
	return p, nil
}
