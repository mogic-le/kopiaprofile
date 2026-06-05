// Package config provides YAML/TOML/HCL/JSON loading, inheritance
// resolution, template expansion and secret masking for kopiaprofile
// configuration files.
//
// A configuration file describes global defaults, a set of named profiles
// (with optional inheritance) and a set of groups that bundle profiles
// together.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/pelletier/go-toml/v2"
	"github.com/zclconf/go-cty/cty"
	"gopkg.in/yaml.v3"
)

// FormatVersion is the configuration schema version implemented by this
// build of kopiaprofile.
const FormatVersion = "1"

// File represents the parsed (but not yet resolved) configuration file as it
// sits on disk. Inheritance is resolved in Resolve().
type File struct {
	Version  string             `yaml:"version"`
	Includes []string           `yaml:"includes"`
	Global   Global             `yaml:"global"`
	Groups   map[string]Group   `yaml:"groups"`
	Profiles map[string]Profile `yaml:"profiles"`

	path string // absolute path of the loaded file
}

// Global captures the [global] block: defaults applied to every profile that
// does not override them explicitly.
type Global struct {
	DefaultConfig  bool              `yaml:"default-config"`
	KopiaBinary    string            `yaml:"kopia-binary"`
	KopiaConfigDir string            `yaml:"kopia-config-dir"`
	Initialize     bool              `yaml:"initialize"`
	LogFile        string            `yaml:"log-file"`
	LogLevel       string            `yaml:"log-level"`
	Quiet          bool              `yaml:"quiet"`
	ForceInactive  bool              `yaml:"force-inactive-lock"`
	StaleLockAge   string            `yaml:"stale-lock-age"`
	LockRetryAfter string            `yaml:"lock-retry-after"`
	Repository     Repository        `yaml:"repository"`
	CacheDir       string            `yaml:"cache-dir"`
	Password       Password          `yaml:"password"`
	Env            map[string]string `yaml:"env"`
	EnvFile        string            `yaml:"env-file"`
}

// Group is a named collection of profiles that can be triggered with a single
// command.
type Group struct {
	Profiles           []string `yaml:"profiles"`
	Description        string   `yaml:"description"`
	GroupContinueError bool     `yaml:"group-continue-error"`
}

// LoadOptions controls optional behaviour of Load.
type LoadOptions struct {
	// ConfigPath is the path to the configuration file. Required.
	ConfigPath string
}

// Load reads the configuration file at opts.ConfigPath, recursively merges
// any includes and returns the merged file. The returned File still needs
// Resolve() to apply inheritance between profiles.
func Load(opts LoadOptions) (*File, error) {
	if opts.ConfigPath == "" {
		return nil, errors.New("config path is required")
	}
	abs, err := filepath.Abs(opts.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("resolving config path: %w", err)
	}

	f := &File{path: abs}
	if err := f.loadFromDisk(abs); err != nil {
		return nil, err
	}

	for _, inc := range f.Includes {
		if err := f.applyInclude(inc); err != nil {
			return nil, fmt.Errorf("include %q: %w", inc, err)
		}
	}

	if f.Version == "" {
		// default – no explicit version is acceptable for early adopters
		f.Version = FormatVersion
	}

	if f.Version != FormatVersion {
		return nil, fmt.Errorf("unsupported config version %q (expected %q)", f.Version, FormatVersion)
	}

	return f, nil
}

func (f *File) loadFromDisk(path string) error {
	cleaned := filepath.Clean(path)
	data, err := os.ReadFile(cleaned) // #nosec G304 -- configuration path is user-controlled by design
	if err != nil {
		return fmt.Errorf("reading config %q: %w", cleaned, err)
	}
	return unmarshalConfig(data, DetectFormat(cleaned), f)
}

// DetectFormat returns the configuration format of a file based on its
// extension. Supported: yaml, yml, toml, hcl, json.
func DetectFormat(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yml", ".yaml":
		return "yaml"
	case ".toml":
		return "toml"
	case ".hcl":
		return "hcl"
	case ".json":
		return "json"
	default:
		// default to YAML — most common case
		return "yaml"
	}
}

// unmarshalConfig dispatches to the correct parser based on the format
// hint. All parsers are expected to populate a *File in place.
func unmarshalConfig(data []byte, format string, f *File) error {
	// All three decoders (YAML, TOML, JSON) work on the *File struct via
	// struct tags. HCL is converted to JSON first because the HCL
	// native decoder is per-block, and a flat struct-decode would be
	// verbose.
	switch strings.ToLower(format) {
	case "yaml", "yml", "":
		if err := yaml.Unmarshal(data, f); err != nil {
			return fmt.Errorf("parsing YAML: %w", err)
		}
	case "toml":
		if err := toml.Unmarshal(data, f); err != nil {
			return fmt.Errorf("parsing TOML: %w", err)
		}
	case "json":
		if err := json.Unmarshal(data, f); err != nil {
			return fmt.Errorf("parsing JSON: %w", err)
		}
	case "hcl":
		return unmarshalHCL(data, f)
	default:
		return fmt.Errorf("unknown config format %q (use yaml, toml, hcl or json)", format)
	}
	return nil
}

// unmarshalHCL converts an HCL document into a *File by going through
// JSON. We use hclsyntax to parse the file, then convert the resulting
// AST to a generic map[string]interface{} and finally to JSON.
//
// The conversion handles the common cases (strings, numbers, bools,
// lists, nested blocks) but is not as complete as e.g. Terraform's own
// decoder; in particular, it does not support HCL functions or
// expressions. For kopiaprofile's static config format this is enough.
func unmarshalHCL(data []byte, f *File) error {
	p := hclparse.NewParser()
	file, diags := p.ParseHCL(data, "config.hcl")
	if diags.HasErrors() {
		return fmt.Errorf("parsing HCL: %s", diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return errors.New("HCL body is not native syntax")
	}
	raw, err := hclBodyToMap(body)
	if err != nil {
		return fmt.Errorf("converting HCL to map: %w", err)
	}
	// Round-trip via JSON so we can use the struct tags already on File.
	jsonBytes, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("re-marshaling HCL to JSON: %w", err)
	}
	if err := json.Unmarshal(jsonBytes, f); err != nil {
		return fmt.Errorf("unmarshaling HCL-as-JSON: %w", err)
	}
	return nil
}

// hclBodyToMap converts an hclsyntax.Body into a generic Go map. Nested
// blocks become nested maps; repeated block names (e.g. two profiles
// with the same name) are not supported — kopiaprofile has no such
// use-case, and HCL would have rejected it at parse time anyway.
func hclBodyToMap(body *hclsyntax.Body) (map[string]interface{}, error) {
	out := make(map[string]interface{})
	for _, attr := range body.Attributes {
		v, err := hclExprToGo(attr.Expr)
		if err != nil {
			return nil, fmt.Errorf("attribute %q: %w", attr.Name, err)
		}
		out[attr.Name] = v
	}
	for _, block := range body.Blocks {
		blockBody, err := hclBodyToMap(block.Body)
		if err != nil {
			return nil, fmt.Errorf("block %q: %w", block.Type, err)
		}
		// Multiple blocks of the same type are merged into a list of maps
		// (e.g. two `profile "home"` blocks). The YAML/TOML layer
		// expects a single object per key, so this is the best we can
		// do without an explicit per-block-type map.
		existing, ok := out[block.Type]
		if !ok {
			out[block.Type] = blockBody
			continue
		}
		switch e := existing.(type) {
		case map[string]interface{}:
			out[block.Type] = []interface{}{e, blockBody}
		case []interface{}:
			out[block.Type] = append(e, blockBody)
		}
		_ = block.Labels
	}
	return out, nil
}

// hclExprToGo converts a single HCL expression to its natural Go
// representation. TemplateExpr covers ALL string literals in HCL
// (even those without ${} interpolation); the parts are joined
// verbatim. ObjectConsExpr covers HCL object literals.
func hclExprToGo(expr hclsyntax.Expression) (interface{}, error) {
	switch v := expr.(type) {
	case *hclsyntax.LiteralValueExpr:
		return ctyToGo(v.Val), nil
	case *hclsyntax.TemplateExpr:
		// A string literal in HCL is parsed as a TemplateExpr with a
		// single LiteralValueExpr part. If there are more parts, the
		// string contains ${...} interpolation, which we do not
		// support (use Go-template syntax in YAML/JSON instead).
		if len(v.Parts) == 1 {
			return ctyToGo(v.Parts[0].(*hclsyntax.LiteralValueExpr).Val), nil
		}
		return nil, fmt.Errorf("template expressions with ${...} interpolation are not supported in HCL config; use yaml or json with {{ ... }} instead")
	case *hclsyntax.TemplateWrapExpr:
		// ${ ... } wrapper — unwrap and recurse.
		return hclExprToGo(v.Wrapped)
	case *hclsyntax.TupleConsExpr:
		out := make([]interface{}, 0, len(v.Exprs))
		for _, e := range v.Exprs {
			ev, err := hclExprToGo(e)
			if err != nil {
				return nil, err
			}
			out = append(out, ev)
		}
		return out, nil
	case *hclsyntax.ObjectConsExpr:
		out := make(map[string]interface{}, len(v.Items))
		for _, item := range v.Items {
			key, err := hclExprToGo(item.KeyExpr)
			if err != nil {
				return nil, err
			}
			ks, ok := key.(string)
			if !ok {
				return nil, fmt.Errorf("object key must be a string literal")
			}
			val, err := hclExprToGo(item.ValueExpr)
			if err != nil {
				return nil, err
			}
			out[ks] = val
		}
		return out, nil
	case *hclsyntax.ForExpr:
		return nil, fmt.Errorf("HCL 'for' expressions are not supported; use a static list")
	case *hclsyntax.FunctionCallExpr:
		return nil, fmt.Errorf("HCL function calls are not supported")
	case *hclsyntax.ConditionalExpr:
		return nil, fmt.Errorf("HCL conditional expressions are not supported")
	case *hclsyntax.IndexExpr, *hclsyntax.SplatExpr, *hclsyntax.RelativeTraversalExpr:
		return nil, fmt.Errorf("HCL traversal/index expressions are not supported")
	default:
		return nil, fmt.Errorf("unsupported HCL expression of type %T", expr)
	}
}

// ctyToGo converts a cty.Value into its natural Go representation.
// Cty values are the result of HCL literal evaluation, so this is
// the bridge between the HCL AST and the rest of kopiaprofile.
func ctyToGo(v cty.Value) interface{} {
	if !v.IsKnown() {
		return nil
	}
	t := v.Type()
	switch {
	case t == cty.String:
		return v.AsString()
	case t == cty.Bool:
		return v.True()
	case t == cty.Number:
		// Try int first, then float.
		bf := v.AsBigFloat()
		if bf.IsInt() {
			i, _ := bf.Int64()
			return i
		}
		f, _ := bf.Float64()
		return f
	case t.IsListType() || t.IsTupleType() || t.IsSetType():
		// Lists, tuples, sets — convert recursively.
		out := []interface{}{}
		for it := v.ElementIterator(); it.Next(); {
			_, ev := it.Element()
			out = append(out, ctyToGo(ev))
		}
		return out
	case t.IsMapType() || t.IsObjectType():
		out := map[string]interface{}{}
		for it := v.ElementIterator(); it.Next(); {
			k, ev := it.Element()
			out[k.AsString()] = ctyToGo(ev)
		}
		return out
	default:
		return v.AsString()
	}
}

func (f *File) applyInclude(includePath string) error {
	expanded := expandPath(includePath, f.path)

	other := &File{}
	if err := other.loadFromDisk(expanded); err != nil {
		return err
	}

	f.merge(other)
	return nil
}

// merge folds the contents of other into f. "other" wins on scalar conflicts
// (later includes override earlier ones). Lists and maps are deep-merged.
func (f *File) merge(other *File) {
	if other.Global.DefaultConfig {
		f.Global.DefaultConfig = true
	}
	if other.Global.KopiaBinary != "" {
		f.Global.KopiaBinary = other.Global.KopiaBinary
	}
	if other.Global.KopiaConfigDir != "" {
		f.Global.KopiaConfigDir = other.Global.KopiaConfigDir
	}
	if other.Global.LogFile != "" {
		f.Global.LogFile = other.Global.LogFile
	}
	if other.Global.LogLevel != "" {
		f.Global.LogLevel = other.Global.LogLevel
	}
	if other.Global.CacheDir != "" {
		f.Global.CacheDir = other.Global.CacheDir
	}
	if other.Global.EnvFile != "" {
		f.Global.EnvFile = other.Global.EnvFile
	}
	if other.Global.Initialize {
		f.Global.Initialize = true
	}

	for k, v := range other.Global.Env {
		if f.Global.Env == nil {
			f.Global.Env = map[string]string{}
		}
		f.Global.Env[k] = v
	}

	if f.Profiles == nil {
		f.Profiles = map[string]Profile{}
	}
	for name, prof := range other.Profiles {
		if existing, ok := f.Profiles[name]; ok {
			merged := mergeProfiles(existing, prof)
			f.Profiles[name] = merged
		} else {
			f.Profiles[name] = prof
		}
	}

	if f.Groups == nil {
		f.Groups = map[string]Group{}
	}
	for name, grp := range other.Groups {
		f.Groups[name] = grp
	}
}

// Path returns the absolute path of the loaded configuration file.
func (f *File) Path() string { return f.path }

// expandPath resolves ~ and relative paths against the parent config file.
func expandPath(p, parentFile string) string {
	if p == "" {
		return p
	}
	if p[0] == '~' {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, p[1:])
		}
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(filepath.Dir(parentFile), p)
	}
	return p
}

// Marshal returns a representation of the file in the requested
// format. Used by `init` to render a clean skeleton. Supported
// formats: yaml (default), toml, json. HCL output is not implemented
// because HCL is a write-once language — kopiaprofile round-trips
// configs through the other formats.
func (f *File) Marshal(format string) ([]byte, error) {
	switch strings.ToLower(format) {
	case "", "yaml", "yml":
		var buf bytes.Buffer
		enc := yaml.NewEncoder(&buf)
		enc.SetIndent(2)
		if err := enc.Encode(f); err != nil {
			return nil, err
		}
		_ = enc.Close()
		return buf.Bytes(), nil
	case "toml":
		var buf bytes.Buffer
		enc := toml.NewEncoder(&buf)
		enc.SetIndentTables(true)
		if err := enc.Encode(f); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	case "json":
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetIndent("", "  ")
		if err := enc.Encode(f); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	default:
		return nil, fmt.Errorf("unsupported marshal format %q (use yaml, toml, json)", format)
	}
}
