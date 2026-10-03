// Package cli is otman's in-process entry point (test seam 1). Everything a
// command can observe from the outside world arrives through Options.
package cli

import (
	"io"
	"time"

	"github.com/spf13/cobra"
)

// Version is the otman version, overridden at build time with
// -ldflags "-X github.com/talvor/otman/internal/cli.Version=...".
var Version = "dev"

// Options is everything one otman run sees.
type Options struct {
	Args   []string  // command-line arguments, without the program name
	Env    []string  // environment, as KEY=VALUE pairs
	Dir    string    // working directory
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Now    func() time.Time
	IsTTY  bool // whether Stdout is a terminal
}

// Run executes one otman command and returns its exit code.
func Run(o Options) int {
	root := &cobra.Command{
		Use:           "otman",
		Short:         "Run an Obsidian Vault as an issue tracker",
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetArgs(o.Args)
	root.SetIn(o.Stdin)
	root.SetOut(o.Stdout)
	root.SetErr(o.Stderr)
	if err := root.Execute(); err != nil {
		return 1
	}
	return 0
}
