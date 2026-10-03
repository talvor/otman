package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/talvor/otman/internal/config"
)

func (a *app) newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show or change user config",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "set vault|actor|project VALUE",
		Short: "Write a value to the user config file",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			return a.configSet(args[0], args[1])
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show each effective setting and where it came from",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return a.configShow()
		},
	})
	return cmd
}

type configSetResult struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Path    string `json:"path"`
	Changed bool   `json:"changed"`
}

func (r configSetResult) RenderHuman(w io.Writer) error {
	if !r.Changed {
		_, err := fmt.Fprintf(w, "%s is already %s in %s\n", r.Key, r.Value, r.Path)
		return err
	}
	_, err := fmt.Fprintf(w, "Set %s to %s in %s\n", r.Key, r.Value, r.Path)
	return err
}

func (a *app) configSet(name, value string) error {
	k, ok := config.LookupKey(name)
	if !ok {
		names := config.KeyNames()
		return invalid("invalid_config_key",
			"unknown config key "+quoteArg(name),
			map[string]any{"key": name, "allowed": names},
			"use one of: "+strings.Join(names, ", "))
	}
	if strings.TrimSpace(value) == "" {
		return invalid("invalid_config_value", k.Name+" cannot be empty",
			map[string]any{"key": k.Name}, "")
	}
	if k.IsPath {
		value = a.abs(value)
	}
	path, err := a.configPath()
	if err != nil {
		return err
	}
	file, changed, err := config.Set(path, k, value)
	if err != nil {
		return configLoadError(err)
	}
	return a.emit(configSetResult{Key: k.Name, Value: value, Path: path, Changed: changed},
		configWarnings(file, path))
}

// setting is one effective value in config show; both fields are null when
// the setting is not configured anywhere.
type setting struct {
	Value  *string `json:"value"`
	Source *string `json:"source"`
}

func newSetting(v config.Value) setting {
	if !v.IsSet() {
		return setting{}
	}
	val, src := v.Value, string(v.Source)
	return setting{&val, &src}
}

// configShowResult has one field per config key, in display order, after
// config_path and pointer_path.
type configShowResult struct {
	ConfigPath  string
	PointerPath *string   // the nearest repo pointer, nil when there is none
	Settings    []setting // parallel to config.Keys
}

func (r configShowResult) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(`{"config_path":`)
	p, err := json.Marshal(r.ConfigPath)
	if err != nil {
		return nil, err
	}
	b.Write(p)
	b.WriteString(`,"pointer_path":`)
	if p, err = json.Marshal(r.PointerPath); err != nil {
		return nil, err
	}
	b.Write(p)
	for i, k := range config.Keys {
		v, err := json.Marshal(r.Settings[i])
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&b, ",%q:", k.Name)
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

func (r configShowResult) RenderHuman(w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "KEY\tVALUE\tSOURCE")
	for i, k := range config.Keys {
		val, src := "-", "unset"
		if s := r.Settings[i]; s.Value != nil {
			val, src = *s.Value, *s.Source
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", k.Name, val, src)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "\nconfig file: %s\n", r.ConfigPath); err != nil {
		return err
	}
	if r.PointerPath != nil {
		_, err := fmt.Fprintf(w, "repo pointer: %s\n", *r.PointerPath)
		return err
	}
	return nil
}

func (a *app) configShow() error {
	s, err := a.settings()
	if err != nil {
		return err
	}
	if _, err := s.project(); err != nil {
		return err
	}
	r := configShowResult{ConfigPath: s.ConfigPath}
	if s.PointerPath != "" {
		r.PointerPath = &s.PointerPath
	}
	for _, k := range config.Keys {
		r.Settings = append(r.Settings, newSetting(s.Get(k)))
	}
	return a.emit(r, s.Warnings)
}
