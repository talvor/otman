package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/talvor/otman/internal/item"
	"github.com/talvor/otman/internal/output"
	"github.com/talvor/otman/internal/vault"
)

type editFlags struct {
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
		Use:   "edit REF [--kind K] [--body T | --body-file P|- | --clear-body] [--assignee NAME|@me | --clear-assignee] [--add-label L]... [--remove-label L]... [--if-rev REV]",
		Short: "Change an Item's Kind, body, assignee or Labels",
		Args:  cobra.ExactArgs(1),
	}
	fl := cmd.Flags()
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
		if changed {
			to := file.Path
			if update.Kind != nil && (before.Kind == nil || *before.Kind != *update.Kind) {
				to = file.KindPath(*update.Kind)
			}
			if file, err = v.MoveItemFile(file, to, data); err != nil {
				return writeError(file, err)
			}
		}
		files, err := v.ItemFiles(file.Key)
		if err != nil {
			return ioError(err)
		}
		summary := newItemSummary(file, item.Parse(data), data, newItemLinks(files))
		human := fmt.Sprintf("Edited %s · %s\n%s\n", summary.ID, summary.Title, summary.Path)
		if !changed {
			human = fmt.Sprintf("%s · %s is unchanged\n", summary.ID, summary.Title)
		}
		return a.emit(mutationResult{summary, changed, human}, warnings)
	})
}
