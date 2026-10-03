package cli

import (
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

func (a *app) configSet(key, value string) error {
	if !config.IsKey(key) {
		return &Error{
			Exit: ExitInvalid, Code: "invalid_config_key",
			Message: fmt.Sprintf("unknown config key %s", quoteArg(key)),
			Details: map[string]any{"key": key, "allowed": config.Keys},
			Hint:    "use one of: " + strings.Join(config.Keys, ", "),
		}
	}
	if strings.TrimSpace(value) == "" {
		return &Error{
			Exit: ExitInvalid, Code: "invalid_config_value",
			Message: fmt.Sprintf("%s cannot be empty", key),
			Details: map[string]any{"key": key},
		}
	}
	if key == "vault" {
		value = a.abs(value)
	}
	path, err := a.configPath()
	if err != nil {
		return err
	}
	changed, err := config.Set(path, key, value)
	if err != nil {
		return configLoadError(err)
	}
	return a.emit(configSetResult{Key: key, Value: value, Path: path, Changed: changed}, nil)
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

type configShowResult struct {
	ConfigPath string  `json:"config_path"`
	Vault      setting `json:"vault"`
	Actor      setting `json:"actor"`
	Project    setting `json:"project"`
}

func (r configShowResult) RenderHuman(w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "KEY\tVALUE\tSOURCE")
	for _, row := range []struct {
		key string
		s   setting
	}{{"vault", r.Vault}, {"actor", r.Actor}, {"project", r.Project}} {
		val, src := "-", "unset"
		if row.s.Value != nil {
			val, src = *row.s.Value, *row.s.Source
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", row.key, val, src)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "\nconfig file: %s\n", r.ConfigPath)
	return err
}

func (a *app) configShow() error {
	s, path, err := a.settings()
	if err != nil {
		return err
	}
	return a.emit(configShowResult{
		ConfigPath: path,
		Vault:      newSetting(s.Vault),
		Actor:      newSetting(s.Actor),
		Project:    newSetting(s.Project),
	}, nil)
}
