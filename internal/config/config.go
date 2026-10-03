// Package config locates, reads and writes otman's user config and resolves
// the effective Vault, Project and actor from flags, environment and config.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Keys are the user config keys, in display order.
var Keys = []string{"vault", "actor", "project"}

// IsKey reports whether k is a user config key.
func IsKey(k string) bool {
	for _, key := range Keys {
		if key == k {
			return true
		}
	}
	return false
}

// Source says where an effective value came from.
type Source string

const (
	FromFlag   Source = "flag"
	FromEnv    Source = "env"
	FromConfig Source = "config"
)

// File is the user config file's contents.
type File struct {
	Vault   string `toml:"vault,omitempty"`
	Actor   string `toml:"actor,omitempty"`
	Project string `toml:"project,omitempty"`
}

func (f *File) get(k string) string {
	switch k {
	case "vault":
		return f.Vault
	case "actor":
		return f.Actor
	case "project":
		return f.Project
	}
	return ""
}

func (f *File) set(k, v string) {
	switch k {
	case "vault":
		f.Vault = v
	case "actor":
		f.Actor = v
	case "project":
		f.Project = v
	}
}

// ErrNoConfigDir means neither XDG_CONFIG_HOME nor HOME is set.
var ErrNoConfigDir = errors.New("neither XDG_CONFIG_HOME nor HOME is set")

// Path returns the user config path: $XDG_CONFIG_HOME/otman/config.toml,
// falling back to $HOME/.config/otman/config.toml. lookup reads the
// environment.
func Path(lookup func(string) string) (string, error) {
	if x := lookup("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "otman", "config.toml"), nil
	}
	if h := lookup("HOME"); h != "" {
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
	var f File
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return f, err
	}
	md, err := toml.Decode(string(b), &f)
	if err != nil {
		return f, &ParseError{path, err}
	}
	if undec := md.Undecoded(); len(undec) > 0 {
		return f, &ParseError{path, fmt.Errorf("unknown key %q", undec[0].String())}
	}
	return f, nil
}

// Set writes key = value to the config file at path, creating it if needed,
// and reports whether the stored value changed. The file is replaced
// atomically.
func Set(path, key, value string) (bool, error) {
	f, err := Load(path)
	if err != nil {
		return false, err
	}
	if f.get(key) == value {
		return false, nil
	}
	f.set(key, value)
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(f); err != nil {
		return false, err
	}
	return true, writeAtomic(path, buf.Bytes())
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".config-*.toml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Value is an effective setting and where it came from. A zero Value means
// the setting is not configured anywhere.
type Value struct {
	Value  string
	Source Source
}

// Set reports whether the setting has a value.
func (v Value) IsSet() bool { return v.Source != "" }

// Settings are the effective Vault, actor and Project for one run.
type Settings struct {
	Vault   Value
	Actor   Value
	Project Value
}

// Get returns the effective value for a config key.
func (s Settings) Get(k string) Value {
	switch k {
	case "vault":
		return s.Vault
	case "actor":
		return s.Actor
	case "project":
		return s.Project
	}
	return Value{}
}

// Inputs are the layers Resolve merges, highest precedence first.
type Inputs struct {
	Flags map[string]string // explicitly passed flags, by config key
	Env   func(string) string
	File  File
}

// envVars maps config keys to their environment variables.
var envVars = map[string]string{
	"vault":   "OTM_VAULT",
	"actor":   "OTM_ACTOR",
	"project": "OTM_PROJECT",
}

// EnvVar returns the environment variable that sets config key k.
func EnvVar(k string) string { return envVars[k] }

// Resolve applies precedence for every key: flag > OTM_* environment >
// user config. Empty values count as unset.
func Resolve(in Inputs) Settings {
	pick := func(k string) Value {
		if v, ok := in.Flags[k]; ok && v != "" {
			return Value{v, FromFlag}
		}
		if v := in.Env(envVars[k]); v != "" {
			return Value{v, FromEnv}
		}
		if v := in.File.get(k); v != "" {
			return Value{v, FromConfig}
		}
		return Value{}
	}
	return Settings{Vault: pick("vault"), Actor: pick("actor"), Project: pick("project")}
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
