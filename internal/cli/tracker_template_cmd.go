package cli

import (
	"io"

	"github.com/spf13/cobra"
	"github.com/talvor/otman/docs"
)

func (a *app) newTrackerTemplateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tracker-template",
		Short: "Print the Tracker template for the selected Project",
		Long: "Print issue-tracker-otman.md, the Tracker template that tells the Matt Pocock\n" +
			"skills to use otman, with the selected Project's key filled in. Save it as the\n" +
			"repo's docs/agents/issue-tracker.md.",
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error { return a.trackerTemplate() },
	}
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

// trackerTemplate prints the Tracker template for the selected Project. It
// needs only the Project key, so it never opens the Vault.
func (a *app) trackerTemplate() error {
	s, err := a.settings()
	if err != nil {
		return err
	}
	sel, err := selectedProject(s)
	if err != nil {
		return err
	}
	r := trackerTemplateResult{sel.Value, docs.TrackerTemplate(sel.Value)}
	return a.emit(r, s.Warnings)
}
