package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/talvor/otman/docs"
	"github.com/talvor/otman/internal/config"
	"github.com/talvor/otman/internal/fsutil"
)

// trackerDoc is where the repo keeps its Tracker template, relative to the
// repo root.
const trackerDoc = "docs/agents/issue-tracker.md"

func (a *app) newTrackerTemplateCmd() *cobra.Command {
	var write bool
	cmd := &cobra.Command{
		Use:   "tracker-template [--write]",
		Short: "Print the Tracker template for the selected Project",
		Long: "Print issue-tracker-otman.md, the Tracker template that tells the Matt Pocock\n" +
			"skills to use otman, with the selected Project's key filled in. Save it as the\n" +
			"repo's " + trackerDoc + ", or pass --write to save it there: at the git\n" +
			"root, or in the working directory outside git, replacing any existing file.",
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error { return a.trackerTemplate(write) },
	}
	cmd.Flags().BoolVar(&write, "write", false, "write the template to "+trackerDoc+" instead of printing it")
	return cmd
}

type trackerTemplateResult struct {
	Project  string `json:"project"`
	Template string `json:"template"`
}

func (r trackerTemplateResult) RenderHuman(w io.Writer) error {
	_, err := io.WriteString(w, r.Template)
	return err
}

// Document marks the template as a file to save, which AXI prints as it is.
func (trackerTemplateResult) Document() {}

type trackerTemplateWriteResult struct {
	Project string `json:"project"`
	Path    string `json:"path"`
}

func (r trackerTemplateWriteResult) RenderHuman(w io.Writer) error {
	_, err := fmt.Fprintf(w, "Wrote the Tracker template for Project %s to %s\n", r.Project, r.Path)
	return err
}

// trackerTemplate prints the Tracker template for the selected Project, or
// with write saves it as the repo's issue tracker doc. It needs only the
// Project key, so it never opens the Vault.
func (a *app) trackerTemplate(write bool) error {
	s, err := a.settings()
	if err != nil {
		return err
	}
	sel, err := selectedProject(s)
	if err != nil {
		return err
	}
	template := docs.TrackerTemplate(sel.Value)
	if !write {
		return a.emit(trackerTemplateResult{sel.Value, template}, s.Warnings)
	}
	dir, err := config.LinkDir(a.opts.Dir)
	if err != nil {
		return ioError(err)
	}
	path := filepath.Join(dir, filepath.FromSlash(trackerDoc))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return ioError(err)
	}
	if err := fsutil.WriteFile(path, []byte(template)); err != nil {
		return ioError(err)
	}
	return a.emit(trackerTemplateWriteResult{sel.Value, path}, s.Warnings)
}
