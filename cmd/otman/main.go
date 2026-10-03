// Command otman runs an Obsidian Vault as an issue tracker.
package main

import (
	"os"
	"time"

	"github.com/talvor/otman/internal/cli"
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

// isTerminal reports whether f is a character device, which is how otman
// decides between human and AXI output.
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
