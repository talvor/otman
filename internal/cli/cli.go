// Package cli is otman's in-process entry point (test seam 1). Everything a
// command can observe from the outside world arrives through Options, so
// tests drive the whole CLI exactly as a user or agent does.
package cli

import (
	"errors"
	"fmt"
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

	// LockTimeout bounds the wait for .otman/lock; zero means
	// vault.DefaultLockTimeout. Tests shorten it.
	LockTimeout time.Duration
}

// app is the state of one run, shared by every command.
type app struct {
	opts Options
	env  map[string]string

	// global flags; the config key flags are read back from cobra
	format   string
	jsonFlag bool
	version  bool

	root           *cobra.Command
	formatResolved bool // flags were parsed and the format validated
	out            output.Format
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
	if !a.formatResolved {
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
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			f, err := formatRequest{
				json:        a.jsonFlag,
				format:      a.format,
				formatGiven: cmd.Flags().Changed("format"),
				isTTY:       a.opts.IsTTY,
			}.resolve()
			if err != nil {
				return err
			}
			a.out, a.formatResolved = f, true
			return a.checkKeyFlags()
		},
		// otman handles --version itself, rather than through cobra, so
		// that it honours the output format.
		RunE: func(cmd *cobra.Command, _ []string) error {
			if a.version {
				return a.emit(versionResult{Version}, nil)
			}
			return cmd.Help()
		},
	}
	root.CompletionOptions.DisableDefaultCmd = true
	pf := root.PersistentFlags()
	for _, k := range config.Keys {
		pf.String(k.Name, "", k.Usage)
	}
	pf.StringVar(&a.format, "format", "", "output format: human|axi|json (default human on a terminal, axi otherwise)")
	pf.BoolVar(&a.jsonFlag, "json", false, "shorthand for --format json")
	root.Flags().BoolVarP(&a.version, "version", "v", false, "version for otman")

	root.AddCommand(a.newCreateCmd())
	root.AddCommand(a.newViewCmd())
	root.AddCommand(a.newListCmd())
	root.AddCommand(a.newEditCmd())
	for _, c := range statusCommands {
		root.AddCommand(a.newStatusCmd(c))
	}
	root.AddCommand(a.newConfigCmd())
	root.AddCommand(a.newProjectCmd())
	return root
}

// checkKeyFlags rejects an explicit empty --vault, --actor or --project.
// Passing one is a mistake (often an unset shell variable), so it fails
// rather than silently falling through to the environment or config.
func (a *app) checkKeyFlags() error {
	pf := a.root.PersistentFlags()
	for _, k := range config.Keys {
		if v, _ := pf.GetString(k.Name); pf.Changed(k.Name) && strings.TrimSpace(v) == "" {
			flag := "--" + k.Name
			return invalid("invalid_arguments", flag+" cannot be empty",
				map[string]any{"flag": flag},
				"pass a value, or omit "+flag+" to use "+k.EnvVar+" or config")
		}
	}
	return nil
}

type versionResult struct {
	Version string `json:"version"`
}

func (r versionResult) RenderHuman(w io.Writer) error {
	_, err := fmt.Fprintf(w, "otman version %s\n", r.Version)
	return err
}

// formatRequest is what a run asked for: --json, --format (and whether it
// was given at all) and whether stdout is a terminal.
type formatRequest struct {
	json        bool
	format      string
	formatGiven bool
	isTTY       bool
}

// resolve applies --format/--json over the TTY default.
func (r formatRequest) resolve() (output.Format, error) {
	var f output.Format
	if r.formatGiven {
		var ok bool
		if f, ok = output.ParseFormat(r.format); !ok {
			return "", invalid("invalid_format",
				"unknown output format "+quoteArg(r.format),
				map[string]any{"format": r.format, "allowed": []string{"human", "axi", "json"}},
				"use --format human, axi or json")
		}
	}
	if r.json {
		if r.formatGiven && f != output.JSON {
			e := conflictingFlags("--json", "--format", "pass either --json or --format, not both")
			e.Message += " " + r.format // name the format --json disagrees with
			return "", e
		}
		return output.JSON, nil
	}
	if r.formatGiven {
		return f, nil
	}
	return output.Default(r.isTTY), nil
}

// prescanFormat finds the requested format when cobra failed before parsing
// flags, so even a usage error is reported in the format asked for. An
// invalid or conflicting request falls back to the TTY default.
func (a *app) prescanFormat() output.Format {
	r := formatRequest{isTTY: a.opts.IsTTY}
	args := a.opts.Args
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; {
		case arg == "--":
			i = len(args)
		case arg == "--json":
			r.json = true
		case strings.HasPrefix(arg, "--json="):
			if v, err := strconv.ParseBool(strings.TrimPrefix(arg, "--json=")); err == nil {
				r.json = v
			}
		case arg == "--format" && i+1 < len(args):
			r.format, r.formatGiven = args[i+1], true
			i++
		case strings.HasPrefix(arg, "--format="):
			r.format, r.formatGiven = strings.TrimPrefix(arg, "--format="), true
		}
	}
	f, err := r.resolve()
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

// configPath locates the user config file.
func (a *app) configPath() (string, error) {
	p, err := config.Path(a.env)
	if err != nil {
		return "", invalid("invalid_config", err.Error(), nil, "set XDG_CONFIG_HOME or HOME")
	}
	return p, nil
}

// resolved is the effective configuration for one run.
type resolved struct {
	config.Settings
	ConfigPath  string // the user config file
	PointerPath string // the nearest repo pointer; "" when there is none
	Warnings    []output.Problem

	pointerErr error // the repo pointer the Project depends on is unreadable
}

// project returns the selected Project, failing when it depends on a repo
// pointer otman cannot read or names a malformed key.
func (r resolved) project() (config.Value, error) {
	if r.pointerErr != nil {
		return config.Value{}, r.pointerErr
	}
	if r.Project.IsSet() {
		if err := checkKey(r.Project.Value); err != nil {
			err.(*Error).Details["source"] = string(r.Project.Source)
			return config.Value{}, err
		}
	}
	return r.Project, nil
}

// settings resolves the effective Vault, Project and actor. The repo
// pointer is read only when no flag or environment variable already
// selects the Project, so a broken pointer never blocks an override, and
// it fails only the commands that use the Project.
func (a *app) settings() (resolved, error) {
	path, err := a.configPath()
	if err != nil {
		return resolved{}, err
	}
	file, err := config.Load(path)
	if err != nil {
		return resolved{}, configLoadError(err)
	}
	flags := map[string]string{}
	pf := a.root.PersistentFlags()
	for _, k := range config.Keys {
		if pf.Changed(k.Name) {
			flags[k.Name], _ = pf.GetString(k.Name)
		}
	}
	ptrPath, err := config.FindPointer(a.opts.Dir)
	if err != nil {
		return resolved{}, ioError(err)
	}
	var ptr string
	var ptrErr error
	if ptrPath != "" && flags[config.Project.Name] == "" && a.env[config.Project.EnvVar] == "" {
		if ptr, err = config.ReadPointer(ptrPath); err != nil {
			ptrErr = pointerError(err, "")
		}
	}
	s := config.Resolve(config.Inputs{Flags: flags, Env: a.env, Pointer: ptr, File: file, Abs: a.abs})
	return resolved{s, path, ptrPath, configWarnings(file, path), ptrErr}, nil
}

// pointerError reports a repo pointer otman cannot read.
func pointerError(err error, hint string) error {
	var pe *config.ParseError
	if !errors.As(err, &pe) {
		return ioError(err)
	}
	if hint == "" {
		hint = "fix " + pe.Path + " to hold project = \"KEY\", or run 'otman project link KEY --force'"
	}
	return invalid("invalid_pointer", "cannot read repo pointer: "+pe.Error(),
		map[string]any{"path": pe.Path}, hint)
}

// configWarnings flags keys in the config file that otman ignores.
func configWarnings(f config.File, path string) []output.Problem {
	var ws []output.Problem
	for _, k := range f.Unknown {
		ws = append(ws, output.Warning("unknown_config_key",
			"ignoring unknown config key "+quoteArg(k),
			map[string]any{"key": k, "path": path},
			"otman reads only "+strings.Join(config.KeyNames(), ", ")+"; remove or rename it in "+path))
	}
	return ws
}

func configLoadError(err error) error {
	var pe *config.ParseError
	if errors.As(err, &pe) {
		return invalid("invalid_config", "cannot read user config: "+pe.Error(),
			map[string]any{"path": pe.Path}, "fix or remove "+pe.Path)
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
