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

// newStatusCmd is close or reopen: the command use sets an Item's status
// to status.
func (a *app) newStatusCmd(use, short, status string) *cobra.Command {
	return &cobra.Command{
		Use:   use + " REF",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return a.setStatus(args[0], status)
		},
	}
}

// setStatus sets status and updated on the Item ref names. An Item that
// already has status is left alone and reported unchanged. Nothing else
// changes: the assignee is kept and children are untouched. A missing or
// invalid status is repaired.
func (a *app) setStatus(ref, status string) error {
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
		p := item.Parse(data)
		changed := p.Status == nil || *p.Status != status
		if changed {
			data, err = item.SetStatus(data, status, a.opts.Now())
			if err != nil {
				return writeError(f, err)
			}
			if err := v.WriteItemFile(f, data); err != nil {
				return ioError(err)
			}
			p = item.Parse(data)
		}
		files, err := v.ItemFiles(f.Key)
		if err != nil {
			return ioError(err)
		}
		summary := newItemSummary(f, p, data, newItemLinks(files))
		human := fmt.Sprintf("%s %s · %s\n", statusVerbs[status], summary.ID, summary.Title)
		if !changed {
			human = fmt.Sprintf("%s · %s is already %s\n", summary.ID, summary.Title, status)
		}
		return a.emit(mutationResult{summary, changed, human}, warnings)
	})
}

var statusVerbs = map[string]string{item.Closed: "Closed", item.Open: "Reopened"}

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
