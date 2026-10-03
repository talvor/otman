// Command otman runs an Obsidian Vault as an issue tracker.
package main

import (
	"os"
	"time"

	"github.com/talvor/otman/internal/cli"
	"golang.org/x/term"
)

func main() {
	dir, err := os.Getwd()
	if err != nil {
		dir = "."
	}
	os.Exit(cli.Run(cli.Options{
		Args:   os.Args[1:],
		Env:    os.Environ(),
		Dir:    dir,
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Now:    time.Now,
		IsTTY:  isTerminal(os.Stdout),
	}))
}

// isTerminal reports whether f is a terminal, which is how otman decides
// between human and AXI output. A character device such as /dev/null is not.
func isTerminal(f *os.File) bool { return term.IsTerminal(int(f.Fd())) }
