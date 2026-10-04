package cli

import (
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/talvor/otman/internal/frontmatter"
	"github.com/talvor/otman/internal/item"
	"github.com/talvor/otman/internal/output"
	"github.com/talvor/otman/internal/vault"
)

type editFlags struct {
	title         string
	kind          string
	body          string
	bodyFile      string
	clearBody     bool
	assignee      string
	clearAssignee bool
	addLabels     []string
	removeLabels  []string
	ifRev         string
}

func (a *app) newEditCmd() *cobra.Command {
	var f editFlags
	cmd := &cobra.Command{
		Use:   "edit REF [--title T] [--kind K] [--body T | --body-file P|- | --clear-body] [--assignee NAME|@me | --clear-assignee] [--add-label L]... [--remove-label L]... [--if-rev REV]",
		Short: "Change an Item's title, Kind, body, assignee or Labels",
		Args:  cobra.ExactArgs(1),
	}
	fl := cmd.Flags()
	fl.StringVar(&f.title, "title", "", "retitle the Item, renaming its file and rewriting every link to it")
	fl.StringVar(&f.kind, "kind", "", "change the Kind to issue, prd or spec, moving the file to its folder")
	fl.StringVar(&f.body, "body", "", "replace the body, keeping the comments")
	fl.StringVar(&f.bodyFile, "body-file", "", "replace the body with PATH, or with stdin for -")
	fl.BoolVar(&f.clearBody, "clear-body", false, "empty the body, keeping the comments")
	fl.StringVar(&f.assignee, "assignee", "", "assign the Item to NAME, or to the actor with @me")
	fl.BoolVar(&f.clearAssignee, "clear-assignee", false, "unassign the Item")
	fl.StringArrayVar(&f.addLabels, "add-label", nil, "add the Label L (repeatable)")
	fl.StringArrayVar(&f.removeLabels, "remove-label", nil, "remove the Label L (repeatable)")
	fl.StringVar(&f.ifRev, "if-rev", "", "fail with stale_item unless the Item's rev is REV")
	cmd.RunE = func(cmd *cobra.Command, args []string) error { return a.edit(cmd, args[0], f) }
	return cmd
}

// edit changes the Item ref names. The whole request is validated before
// anything is written, and an edit that changes nothing writes nothing.
func (a *app) edit(cmd *cobra.Command, ref string, f editFlags) error {
	fl := cmd.Flags()
	if err := exclusive(fl.Changed, "body", "body-file", "clear-body"); err != nil {
		return err
	}
	if err := exclusive(fl.Changed, "assignee", "clear-assignee"); err != nil {
		return err
	}
	// Every flag of edit's own but --if-rev is a change; an edit needs one.
	var changes []string
	given := false
	cmd.LocalNonPersistentFlags().VisitAll(func(fl *pflag.Flag) {
		if fl.Name != "if-rev" && fl.Name != "help" {
			changes = append(changes, "--"+fl.Name)
			given = given || fl.Changed
		}
	})
	if !given {
		return invalid("empty_edit", "edit needs something to change", nil,
			"pass "+strings.Join(changes, ", "))
	}
	var update item.Update
	if fl.Changed("title") {
		if err := checkTitle(f.title); err != nil {
			return err
		}
		update.Title = &f.title
	}
	if fl.Changed("kind") {
		kind, err := parseKind(f.kind)
		if err != nil {
			return err
		}
		update.Kind = &kind
	}
	switch {
	case fl.Changed("clear-body"):
		update.Body = new(string)
	case fl.Changed("body"), fl.Changed("body-file"):
		body, err := a.readText(cmd, "body", f.body, f.bodyFile)
		if err != nil {
			return err
		}
		update.Body = &body
	}
	var err error
	if update.AddLabels, err = parseLabels("--add-label", f.addLabels); err != nil {
		return err
	}
	if update.RemoveLabels, err = parseLabels("--remove-label", f.removeLabels); err != nil {
		return err
	}
	for _, l := range update.AddLabels {
		if slices.Contains(update.RemoveLabels, l) {
			return invalid("conflicting_labels", "--add-label and --remove-label both name "+quoteArg(l),
				map[string]any{"label": l, "flags": []string{"--add-label", "--remove-label"}},
				"either add or remove each Label, not both")
		}
	}
	if fl.Changed("if-rev") && strings.TrimSpace(f.ifRev) == "" {
		return invalid("invalid_arguments", "--if-rev cannot be empty",
			map[string]any{"flag": "--if-rev"}, "pass the rev a view or earlier edit reported")
	}
	s, err := a.settings()
	if err != nil {
		return err
	}
	switch {
	case fl.Changed("clear-assignee"):
		update.ClearAssignee = true
	case fl.Changed("assignee"):
		name, err := nameOrActor(s, "--assignee", f.assignee)
		if err != nil {
			return err
		}
		update.Assignee = &name
	}

	return a.withVault(s, func(v *vault.Vault, warnings []output.Problem) error {
		file, err := resolveRef(s, v, ref)
		if err != nil {
			return err
		}
		data, err := v.ReadItemFile(file)
		if err != nil {
			return ioError(err)
		}
		if rev := item.Rev(data); fl.Changed("if-rev") && rev != f.ifRev {
			return &Error{Exit: ExitConflict, Code: "stale_item",
				Message: file.ID() + " has changed: its rev is " + rev + ", not " + f.ifRev,
				Details: map[string]any{"path": file.Path, "expected_rev": f.ifRev, "rev": rev},
				Hint:    "view the Item again, then retry with its current rev"}
		}
		before := item.Parse(data)
		data, changed, err := item.Apply(data, update, a.opts.Now())
		if err != nil {
			return writeError(file, err)
		}
		if changed && len(update.AddLabels) > 0 {
			var added []string
			for _, l := range update.AddLabels {
				if !item.HasLabel(before.Labels, l) {
					added = append(added, l)
				}
			}
			inUse, err := labelsInUse(v, file.Key, file.Path)
			if err != nil {
				return ioError(err)
			}
			warnings = append(warnings, newLabelWarnings(file.Key, added, inUse)...)
		}
		// A retitle renames the file to the new title's projection, and a
		// Kind change moves it to the new Kind's folder. A rename goes
		// through the journal, rewriting every link to the Item.
		dir, name := path.Dir(file.Path), file.Name()
		if update.Kind != nil && (before.Kind == nil || *before.Kind != *update.Kind) {
			dir = path.Dir(file.KindPath(*update.Kind))
		}
		if update.Title != nil {
			name = item.Filename(file.Key, file.Number, *update.Title)
		}
		switch to := path.Join(dir, name); {
		case to != file.Path:
			operation := vault.MoveOperation
			if name != file.Name() {
				operation = vault.RetitleOperation
			}
			if file, data, err = v.RenameItem(file, to, data, operation); err != nil {
				return renameError(file, err)
			}
			changed = true
		case changed:
			if err := v.WriteItemFile(file, data); err != nil {
				return writeError(file, err)
			}
		}
		summary, ws, err := summarize(v, file, data)
		if err != nil {
			return err
		}
		warnings = append(warnings, ws...)
		human := fmt.Sprintf("Edited %s · %s\n%s\n", summary.ID, summary.Title, summary.Path)
		if !changed {
			human = fmt.Sprintf("%s · %s is unchanged\n", summary.ID, summary.Title)
		}
		return a.emit(mutationResult{summary, changed, human}, warnings)
	})
}

// renameError is the failure of renaming Item file f: unsafe_write naming
// a file whose links cannot be rewritten, move_target_exists naming the
// note a link to which the new path would make ambiguous, unsafe_write
// for a journal stopped by unexpected content, or whatever writeError makes of
// anything else.
func renameError(f vault.ItemFile, err error) error {
	var lr *vault.LinkRewriteError
	var unsafe *frontmatter.UnsafeError
	var c *vault.JournalConflictError
	var amb *vault.AmbiguousLinkError
	switch {
	case errors.As(err, &lr) && errors.As(err, &unsafe):
		e := unsafeWrite(lr.Path, unsafe.Reason,
			"make the frontmatter of "+lr.Path+" plain block-style YAML between --- lines, then retry")
		e.Message = "cannot rename " + f.ID() + ": refusing to rewrite " + lr.Path + ", which links to it: " + unsafe.Reason
		e.Details["item"] = f.ID()
		return e
	case errors.As(err, &amb):
		return &Error{Exit: ExitConflict, Code: "move_target_exists",
			Message: "cannot rename " + f.ID() + ": " + amb.Error(),
			Details: map[string]any{"item": f.ID(), "path": amb.Path, "link": amb.Target, "target": amb.Note},
			Hint:    "rename or move " + amb.Note + ", or name it by path in " + amb.Path + ", then retry"}
	case errors.As(err, &c):
		return journalError(c)
	default:
		return writeError(f, err)
	}
}

// checkTitle validates a new title: a single line, not blank. The title
// keeps the exact text; only the filename is a projection.
func checkTitle(title string) error {
	if strings.TrimSpace(title) == "" {
		return invalid("invalid_arguments", "--title cannot be empty",
			map[string]any{"flag": "--title"}, "pass the Item's new title with --title")
	}
	if !singleLine(title) {
		return invalid("invalid_arguments", "--title must be a single line of UTF-8 text",
			map[string]any{"flag": "--title"}, "")
	}
	return nil
}
