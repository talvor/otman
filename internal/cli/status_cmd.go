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
	comment            bool   // whether it takes --comment and --comment-file
}

var statusCommands = []statusCommand{
	{"close", "Close an Item", item.Closed, "Closed", true},
	{"reopen", "Reopen a closed Item", item.Open, "Reopened", false},
}

func (a *app) newStatusCmd(c statusCommand) *cobra.Command {
	var comment, commentFile string
	cmd := &cobra.Command{
		Use:   c.use + " REF",
		Short: c.short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.setStatus(cmd, c, args[0], comment, commentFile)
		},
	}
	if c.comment {
		cmd.Use += " [--comment TEXT | --comment-file PATH|-]"
		cmd.Flags().StringVar(&comment, "comment", "", "append a comment, as Markdown, in the same write")
		cmd.Flags().StringVar(&commentFile, "comment-file", "", "append a comment read from PATH, or from stdin with -")
	}
	return cmd
}

// setStatus sets c.status, and updated, on the Item ref names, appending a
// comment by the actor in the same write when --comment or --comment-file
// is given. An Item that already has the status is left alone and
// reported unchanged, unless a comment is appended to it. Nothing else
// changes: the assignee is kept and children are untouched. A missing or
// invalid status is repaired.
func (a *app) setStatus(cmd *cobra.Command, c statusCommand, ref, commentText, commentFile string) error {
	fl := cmd.Flags()
	// Only close registers the comment flags, so reopen never has one.
	withComment := fl.Changed("comment") || fl.Changed("comment-file")
	if withComment {
		var err error
		if commentText, err = a.readComment(cmd, "comment", commentText, commentFile); err != nil {
			return err
		}
	}
	s, err := a.settings()
	if err != nil {
		return err
	}
	var comment *item.Comment
	if withComment {
		cm, err := a.newComment(s, c.use+" --comment", commentText)
		if err != nil {
			return err
		}
		comment = &cm
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
		before := item.Parse(data)
		data, changed, err := item.SetStatus(data, f.Derived(), c.status, comment, a.opts.Now())
		if err != nil {
			return writeError(f, err)
		}
		if changed {
			if err := v.WriteItemFile(f, data); err != nil {
				return ioError(err)
			}
		}
		summary, ws, err := summarize(v, f, data)
		if err != nil {
			return err
		}
		warnings = append(warnings, ws...)
		already := before.Status != nil && *before.Status == c.status
		var human string
		switch {
		case !already && comment != nil:
			human = fmt.Sprintf("%s %s · %s, with a comment\n", c.done, summary.ID, summary.Title)
		case !already:
			human = fmt.Sprintf("%s %s · %s\n", c.done, summary.ID, summary.Title)
		case comment != nil:
			human = fmt.Sprintf("%s · %s is already %s; commented\n", summary.ID, summary.Title, c.status)
		default:
			human = fmt.Sprintf("%s · %s is already %s\n", summary.ID, summary.Title, c.status)
		}
		return a.emit(mutationResult{summary, changed, human}, warnings)
	})
}

// writeError reports a failed rewrite of Item file f: unsafe_write when
// otman refused to splice it or to guess where its body ends,
// move_target_exists when a move would replace another file, an I/O
// error otherwise.
func writeError(f vault.ItemFile, err error) error {
	var unsafe *frontmatter.UnsafeError
	var exists *vault.TargetExistsError
	switch {
	case errors.As(err, &unsafe):
		return unsafeWrite(f.Path, unsafe.Reason,
			"make the frontmatter plain block-style YAML between --- lines, then retry")
	case errors.Is(err, item.ErrBlockersNotList):
		return unsafeWrite(f.Path, err.Error(),
			`make blocked_by in `+f.Path+` a list of quoted wikilinks, such as blocked_by: ["[[`+f.Key+`-1 Title]]"], then retry`)
	case errors.Is(err, item.ErrNoMarker):
		return unsafeWrite(f.Path, err.Error(), "put the line "+item.CommentsMarker+
			" back just before "+item.CommentsHeading+", or run 'otman comment "+f.ID()+"', which restores it, then retry")
	case errors.Is(err, item.ErrStrayText):
		return unsafeWrite(f.Path, err.Error(), "put the line "+item.CommentsMarker+
			" back just before the "+item.CommentsHeading+" heading where the comments start, then retry")
	case errors.Is(err, item.ErrDuplicateMarkers):
		return unsafeWrite(f.Path, err.Error(), "keep only the "+item.CommentsMarker+
			" line just before the comments' "+item.CommentsHeading+" heading, then retry")
	case errors.As(err, &exists):
		return &Error{Exit: ExitConflict, Code: "move_target_exists",
			Message: "cannot move " + f.Path + ": " + exists.Error(),
			Details: map[string]any{"path": f.Path, "target": exists.Target},
			Hint:    "move or rename the file in the way, then retry"}
	default:
		return ioError(err)
	}
}
