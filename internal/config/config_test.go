package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadSimple(t *testing.T) {
	dir := t.TempDir()
	cfg := writeFile(t, dir, "kopiaprofile.yaml", `
version: "1"
profiles:
  home:
    description: "Home backup"
    repository:
      type: s3
      bucket: my-bucket
    backup:
      sources: [/home/user]
`)
	f, err := Load(LoadOptions{ConfigPath: cfg})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got, want := len(f.Profiles), 1; got != want {
		t.Fatalf("profiles: got %d, want %d", got, want)
	}
	if f.Profiles["home"].Repository.Bucket != "my-bucket" {
		t.Errorf("bucket: got %q", f.Profiles["home"].Repository.Bucket)
	}
}

func TestInheritance(t *testing.T) {
	dir := t.TempDir()
	cfg := writeFile(t, dir, "kopiaprofile.yaml", `
version: "1"
profiles:
  base:
    repository:
      type: s3
      bucket: parent-bucket
    backup:
      sources: [/etc]
      tags: [base-tag]
  child:
    inherit: base
    backup:
      sources: [/home/user]
`)
	f, err := Load(LoadOptions{ConfigPath: cfg})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := f.Resolve(); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	c, _ := f.Get("child")
	if c.Repository.Bucket != "parent-bucket" {
		t.Errorf("inherited bucket: got %q", c.Repository.Bucket)
	}
	if len(c.Backup.Sources) != 1 || c.Backup.Sources[0] != "/home/user" {
		t.Errorf("child backup.sources: got %v", c.Backup.Sources)
	}
}

func TestInheritanceCycle(t *testing.T) {
	dir := t.TempDir()
	cfg := writeFile(t, dir, "kopiaprofile.yaml", `
version: "1"
profiles:
  a:
    inherit: b
  b:
    inherit: a
`)
	f, err := Load(LoadOptions{ConfigPath: cfg})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := f.Resolve(); err == nil {
		t.Fatalf("expected cycle error, got nil")
	}
}

func TestInheritanceMultiLevel(t *testing.T) {
	dir := t.TempDir()
	cfg := writeFile(t, dir, "kopiaprofile.yaml", `
version: "1"
profiles:
  a:
    repository:
      type: s3
      bucket: root
  b:
    inherit: a
    repository:
      region: us-east-1
  c:
    inherit: b
    repository:
      prefix: c-prefix
`)
	f, err := Load(LoadOptions{ConfigPath: cfg})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := f.Resolve(); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	c, _ := f.Get("c")
	if c.Repository.Bucket != "root" {
		t.Errorf("bucket: got %q want root", c.Repository.Bucket)
	}
	if c.Repository.Region != "us-east-1" {
		t.Errorf("region: got %q", c.Repository.Region)
	}
	if c.Repository.Prefix != "c-prefix" {
		t.Errorf("prefix: got %q", c.Repository.Prefix)
	}
}

func TestIncludes(t *testing.T) {
	dir := t.TempDir()
	_ = writeFile(t, dir, "common.yaml", `
global:
  log-level: debug
profiles:
  common-profile:
    description: "From common"
`)
	main := writeFile(t, dir, "kopiaprofile.yaml", `
version: "1"
includes:
  - common.yaml
profiles:
  main-profile:
    description: "From main"
`)
	f, err := Load(LoadOptions{ConfigPath: main})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if f.Global.LogLevel != "debug" {
		t.Errorf("log-level from include: got %q", f.Global.LogLevel)
	}
	if _, ok := f.Profiles["common-profile"]; !ok {
		t.Errorf("common-profile missing")
	}
	if _, ok := f.Profiles["main-profile"]; !ok {
		t.Errorf("main-profile missing")
	}
}

func TestExpandTemplates(t *testing.T) {
	p := Profile{
		Name:        "home",
		Description: "Home",
		Repository:  Repository{Prefix: "p-{{ .Profile.Name }}-h-{{ .Hostname }}"},
		Lock:        LockSection{Path: "/var/lock/{{ .Profile.Name }}.lock"},
	}
	out, err := ExpandTemplates(p)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if out.Repository.Prefix[:2] != "p-" {
		t.Errorf("prefix not rendered: %q", out.Repository.Prefix)
	}
	// The template was "p-{{ .Profile.Name }}-h-{{ .Hostname }}";
	// after rendering both substitutions must be present. We don't
	// pin the exact suffix because .Hostname is host-dependent and
	// may have any length, but the profile name should appear as a
	// contiguous substring right after the literal "p-" prefix.
	if want := "p-" + p.Name + "-h-"; !strings.Contains(out.Repository.Prefix, want) {
		t.Errorf("prefix %q does not contain %q (substitutions broken)", out.Repository.Prefix, want)
	}
	if filepath.Base(out.Lock.Path) != p.Name+".lock" {
		t.Errorf("lock path: got %q", out.Lock.Path)
	}
}

func TestListMergeAppend(t *testing.T) {
	dir := t.TempDir()
	cfg := writeFile(t, dir, "kopiaprofile.yaml", `
version: "1"
profiles:
  base:
    backup:
      sources: [/etc]
      tags: [base-tag]
  child:
    inherit: base
    backup:
      sources: [/var]
      sources-merge: append
      tags: [child-tag]
      tags-merge: append
`)
	f, err := Load(LoadOptions{ConfigPath: cfg})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := f.Resolve(); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	c, _ := f.Get("child")
	want := []string{"/etc", "/var"}
	if !equal(c.Backup.Sources, want) {
		t.Errorf("sources append: got %v, want %v", c.Backup.Sources, want)
	}
	if !equal(c.Backup.Tags, []string{"base-tag", "child-tag"}) {
		t.Errorf("tags append: got %v, want [base-tag child-tag]", c.Backup.Tags)
	}
}

func TestListMergeReplace(t *testing.T) {
	dir := t.TempDir()
	cfg := writeFile(t, dir, "kopiaprofile.yaml", `
version: "1"
profiles:
  base:
    backup:
      tags: [base-tag]
  child:
    inherit: base
    backup:
      tags: [child-tag]
`)
	f, _ := Load(LoadOptions{ConfigPath: cfg})
	_ = f.Resolve()
	c, _ := f.Get("child")
	// Default is replace — child wins.
	if !equal(c.Backup.Tags, []string{"child-tag"}) {
		t.Errorf("tags replace: got %v", c.Backup.Tags)
	}
}

func TestListMergePrepend(t *testing.T) {
	dir := t.TempDir()
	cfg := writeFile(t, dir, "kopiaprofile.yaml", `
version: "1"
profiles:
  base:
    backup:
      sources: [/etc]
  child:
    inherit: base
    backup:
      sources: [/var]
      sources-merge: prepend
`)
	f, _ := Load(LoadOptions{ConfigPath: cfg})
	_ = f.Resolve()
	c, _ := f.Get("child")
	if !equal(c.Backup.Sources, []string{"/var", "/etc"}) {
		t.Errorf("sources prepend: got %v", c.Backup.Sources)
	}
}

func TestListMergeUnique(t *testing.T) {
	dir := t.TempDir()
	cfg := writeFile(t, dir, "kopiaprofile.yaml", `
version: "1"
profiles:
  base:
    backup:
      tags: [common, base-only]
  child:
    inherit: base
    backup:
      tags: [common, child-only]
      tags-merge: unique
`)
	f, _ := Load(LoadOptions{ConfigPath: cfg})
	_ = f.Resolve()
	c, _ := f.Get("child")
	if !equal(c.Backup.Tags, []string{"common", "base-only", "child-only"}) {
		t.Errorf("tags unique: got %v", c.Backup.Tags)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestMaskSecrets(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"AWS_SECRET_ACCESS_KEY=abc123def", "AWS_SECRET_ACCESS_KEY=********"},
		{"PASSWORD=hunter2", "PASSWORD=********"},
		{"https://user:secret@host", "https://user:********@host"},
		{"access-key-id: AKIAEXAMPLE", "access-key-id: ********"},
		{"innocent=value", "innocent=value"},
	}
	for _, c := range cases {
		if got := MaskSecrets(c.in); got != c.want {
			t.Errorf("MaskSecrets(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
