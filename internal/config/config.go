// Package config locates, reads and writes otman's user config and resolves
// the effective Vault, Project and actor from flags, environment and config.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/BurntSushi/toml"
	"github.com/talvor/otman/internal/fsutil"
)

// Key is one user config key. The same name is its config file key and its
// global flag; EnvVar is the environment variable that overrides the file.
type Key struct {
	Name   string
	EnvVar string
	Usage  string // global flag help
	IsPath bool   // relative values resolve against the working directory
	field  func(*Settings) *Value
}

var (
	Vault = Key{
		Name: "vault", EnvVar: "OTM_VAULT", IsPath: true,
		Usage: "Vault path (overrides OTM_VAULT and config)",
		field: func(s *Settings) *Value { return &s.Vault },
	}
	Actor = Key{
		Name: "actor", EnvVar: "OTM_ACTOR",
		Usage: "actor name (overrides OTM_ACTOR and config)",
		field: func(s *Settings) *Value { return &s.Actor },
	}
	Project = Key{
		Name: "project", EnvVar: "OTM_PROJECT",
		Usage: "Project key (overrides OTM_PROJECT and config)",
		field: func(s *Settings) *Value { return &s.Project },
	}
)

// Keys are the user config keys, in display order.
var Keys = []Key{Vault, Actor, Project}

// LookupKey finds the config key called name.
func LookupKey(name string) (Key, bool) {
	for _, k := range Keys {
		if k.Name == name {
			return k, true
		}
	}
	return Key{}, false
}

// KeyNames lists the config key names, in display order.
func KeyNames() []string {
	names := make([]string, len(Keys))
	for i, k := range Keys {
		names[i] = k.Name
	}
	return names
}

// Source says where an effective value came from.
type Source string

const (
	FromFlag    Source = "flag"
	FromEnv     Source = "env"
	FromPointer Source = "pointer" // the repo pointer, .otman.toml
	FromConfig  Source = "config"
)

// File is the user config file's contents. Following ADR 0005, keys otman
// does not know are not an error: they are listed in Unknown, so callers
// can warn, and Set writes them back untouched.
type File struct {
	values  map[string]string // known keys
	Unknown []string          // top-level keys otman does not know, sorted
}

// Get returns the file's value for k, or "" when the file does not set it.
func (f File) Get(k Key) string { return f.values[k.Name] }

// ErrNoConfigDir means neither XDG_CONFIG_HOME nor HOME is set.
var ErrNoConfigDir = errors.New("neither XDG_CONFIG_HOME nor HOME is set")

// Path returns the user config path: $XDG_CONFIG_HOME/otman/config.toml,
// falling back to $HOME/.config/otman/config.toml.
func Path(env map[string]string) (string, error) {
	if x := env["XDG_CONFIG_HOME"]; x != "" {
		return filepath.Join(x, "otman", "config.toml"), nil
	}
	if h := env["HOME"]; h != "" {
		return filepath.Join(h, ".config", "otman", "config.toml"), nil
	}
	return "", ErrNoConfigDir
}

// ParseError means the config file exists but is not valid TOML for otman.
type ParseError struct {
	Path string
	Err  error
}

func (e *ParseError) Error() string { return fmt.Sprintf("%s: %v", e.Path, e.Err) }

// Load reads the config file at path. A missing file is an empty config.
func Load(path string) (File, error) {
	f, _, err := load(path)
	return f, err
}

// load also returns the decoded document, so Set can write back what it
// read.
func load(path string) (File, map[string]any, error) {
	f := File{values: map[string]string{}}
	doc := map[string]any{}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return f, doc, nil
	}
	if err != nil {
		return f, nil, err
	}
	if _, err := toml.Decode(string(b), &doc); err != nil {
		return f, nil, &ParseError{path, err}
	}
	for name, v := range doc {
		k, ok := LookupKey(name)
		if !ok {
			f.Unknown = append(f.Unknown, name)
			continue
		}
		s, ok := v.(string)
		if !ok {
			return f, nil, &ParseError{path, fmt.Errorf("%s must be a string", k.Name)}
		}
		f.values[k.Name] = s
	}
	sort.Strings(f.Unknown)
	return f, doc, nil
}

// Set writes k = value to the config file at path, creating it if needed,
// and reports whether the stored value changed, along with the file as it
// was read. Keys otman does not own are kept (TOML comments are not). The
// file is replaced atomically.
func Set(path string, k Key, value string) (File, bool, error) {
	f, doc, err := load(path)
	if err != nil {
		return f, false, err
	}
	if f.Get(k) == value {
		return f, false, nil
	}
	doc[k.Name] = value
	data, err := encode(doc)
	if err != nil {
		return f, false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return f, false, err
	}
	return f, true, fsutil.WriteFile(path, data)
}

// encode writes the known keys first, in display order, then everything
// else. Known keys are plain strings, so they always precede any table.
func encode(doc map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	enc.Indent = ""
	rest := make(map[string]any, len(doc))
	for name, v := range doc {
		rest[name] = v
	}
	for _, k := range Keys {
		if v, ok := rest[k.Name]; ok {
			if err := enc.Encode(map[string]any{k.Name: v}); err != nil {
				return nil, err
			}
			delete(rest, k.Name)
		}
	}
	if len(rest) > 0 {
		if err := enc.Encode(rest); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

// Value is an effective setting and where it came from. A zero Value means
// the setting is not configured anywhere.
type Value struct {
	Value  string
	Source Source
}

// IsSet reports whether the setting has a value.
func (v Value) IsSet() bool { return v.Source != "" }

// Settings are the effective Vault, actor and Project for one run.
type Settings struct {
	Vault   Value
	Actor   Value
	Project Value
}

// Get returns the effective value for k.
func (s *Settings) Get(k Key) Value { return *k.field(s) }

// Inputs are the layers Resolve merges, highest precedence first.
type Inputs struct {
	Flags   map[string]string // explicitly passed flags, by key name
	Env     map[string]string
	Pointer string // the repo pointer's Project key; "" when there is none
	File    File
	Abs     func(string) string // makes a path key's value absolute; nil leaves it
}

// Resolve applies precedence for every key: flag > OTM_* environment >
// user config, with the repo pointer between environment and config for
// the Project. Empty values count as unset.
func Resolve(in Inputs) Settings {
	var s Settings
	for _, k := range Keys {
		v := k.field(&s)
		switch {
		case in.Flags[k.Name] != "":
			*v = Value{in.Flags[k.Name], FromFlag}
		case in.Env[k.EnvVar] != "":
			*v = Value{in.Env[k.EnvVar], FromEnv}
		case k.Name == Project.Name && in.Pointer != "":
			*v = Value{in.Pointer, FromPointer}
		case in.File.Get(k) != "":
			*v = Value{in.File.Get(k), FromConfig}
		}
		if k.IsPath && v.IsSet() && in.Abs != nil {
			v.Value = in.Abs(v.Value)
		}
	}
	return s
}

// ErrNoActor means a command needed an identity and no actor is configured.
var ErrNoActor = errors.New("no actor configured")

// ResolveUser maps a user name given on the command line to the name to
// store. "@me" means the configured actor; there is deliberately no git or
// OS identity fallback, so "@me" with no actor fails with ErrNoActor.
func (s Settings) ResolveUser(name string) (string, error) {
	if name != "@me" {
		return name, nil
	}
	if !s.Actor.IsSet() {
		return "", ErrNoActor
	}
	return s.Actor.Value, nil
}
