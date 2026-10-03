// Package cli is otman's in-process entry point (test seam 1). Everything a
// command can observe from the outside world arrives through Options, so
// tests drive the whole CLI exactly as a user or agent does.
package cli

import (
	"errors"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/talvor/otman/internal/config"
	"github.com/talvor/otman/internal/output"
)

// Version is the otman version, overridden at build time with
// -ldflags "-X github.com/talvor/otman/internal/cli.Version=...".
var Version = "dev"

// Options is everything one otman run sees.
type Options struct {
	Args   []string // command-line arguments, without the program name
	Env    []string // environment, as KEY=VALUE pairs
	Dir    string   // working directory, for relative paths
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Now    func() time.Time
	IsTTY  bool // whether Stdout is a terminal
}

// app is the state of one run, shared by every command.
type app struct {
	opts Options
	env  map[string]string

	// global flags
	vault, project, actor string
	format                string
	json                  bool

	root   *cobra.Command
	parsed bool // flags were parsed and validated
	out    output.Format
}

// Run executes one otman command and returns its exit code.
func Run(o Options) int {
	a := &app{opts: o, env: parseEnv(o.Env)}
	a.out = output.Default(o.IsTTY)
	a.root = a.newRoot()
	a.root.SetArgs(o.Args)
	a.root.SetIn(o.Stdin)
	a.root.SetOut(o.Stdout)
	a.root.SetErr(o.Stderr)

	err := a.root.Execute()
	if err == nil {
		return ExitOK
	}
	var e *Error
	if !errors.As(err, &e) {
		// Anything otman did not classify comes from cobra's own parsing.
		e = invalidArgs(err.Error(), "run 'otman --help' for usage")
	}
	if !a.parsed {
		a.out = a.prescanFormat()
	}
	if werr := output.Failure(a.out, o.Stderr, e.problem()); werr != nil {
		return ExitFailure
	}
	return e.Exit
}

func (a *app) newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "otman",
		Short:         "Run an Obsidian Vault as an issue tracker",
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			f, err := resolveFormat(a.json, a.format, cmd.Flags().Changed("format"), a.opts.IsTTY)
			if err != nil {
				return err
			}
			a.out, a.parsed = f, true
			return nil
		},
	}
	root.CompletionOptions.DisableDefaultCmd = true
	pf := root.PersistentFlags()
	pf.StringVar(&a.vault, "vault", "", "Vault path (overrides OTM_VAULT and config)")
	pf.StringVar(&a.project, "project", "", "Project key (overrides OTM_PROJECT and config)")
	pf.StringVar(&a.actor, "actor", "", "actor name (overrides OTM_ACTOR and config)")
	pf.StringVar(&a.format, "format", "", "output format: human|axi|json (default human on a terminal, axi otherwise)")
	pf.BoolVar(&a.json, "json", false, "shorthand for --format json")

	root.AddCommand(a.newConfigCmd())
	return root
}

// resolveFormat applies --format/--json over the TTY default.
func resolveFormat(jsonFlag bool, format string, formatGiven bool, isTTY bool) (output.Format, error) {
	var f output.Format
	if formatGiven {
		var ok bool
		if f, ok = output.ParseFormat(format); !ok {
			return "", &Error{
				Exit: ExitInvalid, Code: "invalid_format",
				Message: "unknown output format " + quoteArg(format),
				Details: map[string]any{"format": format, "allowed": []string{"human", "axi", "json"}},
				Hint:    "use --format human, axi or json",
			}
		}
	}
	if jsonFlag {
		if formatGiven && f != output.JSON {
			return "", &Error{
				Exit: ExitInvalid, Code: "conflicting_flags",
				Message: "--json conflicts with --format " + format,
				Details: map[string]any{"flags": []string{"--json", "--format"}},
				Hint:    "pass either --json or --format, not both",
			}
		}
		return output.JSON, nil
	}
	if formatGiven {
		return f, nil
	}
	return output.Default(isTTY), nil
}

// prescanFormat finds the requested format when cobra failed before parsing
// flags, so even a usage error is reported in the format asked for. An
// invalid or conflicting request falls back to the TTY default.
func (a *app) prescanFormat() output.Format {
	var jsonFlag, given bool
	var format string
	args := a.opts.Args
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; {
		case arg == "--":
			i = len(args)
		case arg == "--json":
			jsonFlag = true
		case arg == "--format" && i+1 < len(args):
			format, given = args[i+1], true
			i++
		case strings.HasPrefix(arg, "--format="):
			format, given = strings.TrimPrefix(arg, "--format="), true
		}
	}
	f, err := resolveFormat(jsonFlag, format, given, a.opts.IsTTY)
	if err != nil {
		return output.Default(a.opts.IsTTY)
	}
	return f
}

// emit writes a successful result in the selected format.
func (a *app) emit(data output.HumanRenderer, warnings []output.Problem) error {
	if err := output.Success(a.out, a.opts.Stdout, a.opts.Stderr, data, warnings); err != nil {
		return ioError(err)
	}
	return nil
}

func (a *app) getenv(k string) string { return a.env[k] }

// configPath locates the user config file.
func (a *app) configPath() (string, error) {
	p, err := config.Path(a.getenv)
	if err != nil {
		return "", &Error{
			Exit: ExitInvalid, Code: "invalid_config", Message: err.Error(),
			Hint: "set XDG_CONFIG_HOME or HOME",
		}
	}
	return p, nil
}

// settings resolves the effective Vault, Project and actor, and returns the
// config path it read.
func (a *app) settings() (config.Settings, string, error) {
	path, err := a.configPath()
	if err != nil {
		return config.Settings{}, "", err
	}
	file, err := config.Load(path)
	if err != nil {
		return config.Settings{}, "", configLoadError(err)
	}
	flags := map[string]string{}
	pf := a.root.PersistentFlags()
	for _, k := range config.Keys {
		if pf.Changed(k) {
			flags[k], _ = pf.GetString(k)
		}
	}
	s := config.Resolve(config.Inputs{Flags: flags, Env: a.getenv, File: file})
	if s.Vault.IsSet() {
		s.Vault.Value = a.abs(s.Vault.Value)
	}
	return s, path, nil
}

func configLoadError(err error) error {
	var pe *config.ParseError
	if errors.As(err, &pe) {
		return &Error{
			Exit: ExitInvalid, Code: "invalid_config",
			Message: "cannot read user config: " + pe.Error(),
			Details: map[string]any{"path": pe.Path},
			Hint:    "fix or remove " + pe.Path,
		}
	}
	return ioError(err)
}

// abs resolves p against the run's working directory.
func (a *app) abs(p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(a.opts.Dir, p)
}

func parseEnv(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			m[k] = v
		}
	}
	return m
}

func quoteArg(s string) string { return strconv.Quote(s) }
