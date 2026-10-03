package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/talvor/otman/internal/frontmatter"
	"github.com/talvor/otman/internal/item"
	"github.com/talvor/otman/internal/output"
	"github.com/talvor/otman/internal/vault"
)

// statusCommand is a command that sets an Item's status: close or reopen.
type statusCommand struct {
	use, short, status string
	done               string // what a change reports, such as "Closed"
}

var statusCommands = []statusCommand{
	{"close", "Close an Item", item.Closed, "Closed"},
	{"reopen", "Reopen a closed Item", item.Open, "Reopened"},
}

func (a *app) newStatusCmd(c statusCommand) *cobra.Command {
	return &cobra.Command{
		Use:   c.use + " REF",
		Short: c.short,
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return a.setStatus(c, args[0])
		},
	}
}

// setStatus sets c.status, and updated, on the Item ref names. An Item
// that already has it is left alone and reported unchanged. Nothing else
// changes: the assignee is kept and children are untouched. A missing or
// invalid status is repaired.
func (a *app) setStatus(c statusCommand, ref string) error {
	s, err := a.settings()
	if err != nil {
		return err
	}
	return a.withVault(s, func(v *vault.Vault, warnings []output.Problem) error {
		f, err := resolveRef(s, v, ref)
		if err != nil {
			return err
		}
		data, err := v.ReadItemFile(f)
		if err != nil {
			return ioError(err)
		}
		data, changed, err := item.SetStatus(data, c.status, a.opts.Now())
		if err != nil {
			return writeError(f, err)
		}
		if changed {
			if err := v.WriteItemFile(f, data); err != nil {
				return ioError(err)
			}
		}
		files, err := v.ItemFiles(f.Key)
		if err != nil {
			return ioError(err)
		}
		summary := newItemSummary(f, item.Parse(data), data, newItemLinks(files))
		human := fmt.Sprintf("%s %s · %s\n", c.done, summary.ID, summary.Title)
		if !changed {
			human = fmt.Sprintf("%s · %s is already %s\n", summary.ID, summary.Title, c.status)
		}
		return a.emit(mutationResult{summary, changed, human}, warnings)
	})
}

// writeError reports a failed rewrite of Item file f: unsafe_write when
// otman refused to splice it, an I/O error otherwise.
func writeError(f vault.ItemFile, err error) error {
	var unsafe *frontmatter.UnsafeError
	if !errors.As(err, &unsafe) {
		return ioError(err)
	}
	return &Error{Exit: ExitConflict, Code: "unsafe_write",
		Message: "refusing to rewrite " + f.Path + ": " + unsafe.Reason,
		Details: map[string]any{"path": f.Path, "reason": unsafe.Reason},
		Hint:    "make the frontmatter plain block-style YAML between --- lines, then retry"}
}
