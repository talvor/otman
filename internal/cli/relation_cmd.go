package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/talvor/otman/internal/item"
	"github.com/talvor/otman/internal/output"
	"github.com/talvor/otman/internal/vault"
)

func (a *app) newParentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "parent",
		Short: "Set or clear an Item's parent",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "set CHILD PARENT",
		Short: "Make PARENT the parent of CHILD, replacing any parent it has",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			return a.changeRelations(args[0], func(v *vault.Vault, f vault.ItemFile, p item.Parsed, links itemLinks) (relationChange, error) {
				return parentSet(v, f, p, links, args[1])
			})
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "clear CHILD",
		Short: "Clear the parent of CHILD",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return a.changeRelations(args[0], parentClear)
		},
	})
	return cmd
}

// blockCommand is a command that adds or removes a blocker: block or
// unblock.
type blockCommand struct {
	use, short string
	add        bool
}

var blockCommands = []blockCommand{
	{"block", "Record that BLOCKER blocks an Item", true},
	{"unblock", "Remove the record that BLOCKER blocks an Item", false},
}

func (a *app) newBlockCmd(c blockCommand) *cobra.Command {
	var by string
	cmd := &cobra.Command{
		Use:   c.use + " REF --by BLOCKER",
		Short: c.short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("by") || by == "" {
				return invalid("invalid_arguments", c.use+" needs --by BLOCKER",
					map[string]any{"flag": "--by"}, "pass the blocking Item with --by, such as --by OTM-3")
			}
			plan := unblock
			if c.add {
				plan = block
			}
			return a.changeRelations(args[0], func(v *vault.Vault, f vault.ItemFile, p item.Parsed, links itemLinks) (relationChange, error) {
				return plan(v, f, p, links, by)
			})
		},
	}
	cmd.Flags().StringVar(&by, "by", "", "the blocking Item: an ID, a number in REF's Project, a full filename or a Vault-relative path")
	return cmd
}

// relationChange is what a relation command does to an Item: update, or
// nothing when changed is false, and what a terminal shows.
type relationChange struct {
	update  item.Update
	changed bool
	human   string
}

// changeRelations resolves ref, plans a change to its relations, and
// writes it. The plan and the write happen under the Vault lock, and the
// whole operation is validated before anything is written.
func (a *app) changeRelations(ref string, plan func(*vault.Vault, vault.ItemFile, item.Parsed, itemLinks) (relationChange, error)) error {
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
		// Refuse a file otman could not rewrite, whatever its relations
		// seem to say, even when the change turns out to be a no-op.
		if _, _, err := item.Apply(data, item.Update{}, a.opts.Now()); err != nil {
			return writeError(f, err)
		}
		files, err := v.ItemFiles(f.Key)
		if err != nil {
			return ioError(err)
		}
		links := newItemLinks(files)
		c, err := plan(v, f, item.Parse(data), links)
		if err != nil {
			return err
		}
		out, changed := data, false
		if c.changed {
			if out, changed, err = item.Apply(data, c.update, a.opts.Now()); err != nil {
				return writeError(f, err)
			}
		}
		if changed {
			if err := v.WriteItemFile(f, out); err != nil {
				return ioError(err)
			}
		}
		p := item.Parse(out)
		warnings = append(warnings, relationProblems(f, p, links)...)
		return a.emit(mutationResult{newItemSummary(f, p, out, links), changed, c.human}, warnings)
	})
}

// named is how a human message names Item file f, parsed as p.
func named(f vault.ItemFile, p item.Parsed) string { return f.ID() + " · " + itemTitle(f, p) }

func parentSet(v *vault.Vault, f vault.ItemFile, p item.Parsed, links itemLinks, ref string) (relationChange, error) {
	parent, err := resolveTarget(v, f.Key, ref, "parent")
	if err != nil {
		return relationChange{}, err
	}
	if parent == f {
		return relationChange{}, selfEdge(f, "parent")
	}
	if p.Parent != nil {
		if t, ok := links.target(*p.Parent); ok && t == parent {
			return relationChange{human: fmt.Sprintf("%s already has the parent %s\n", named(f, p), parent.ID())}, nil
		}
	}
	if err := checkParentCycle(v, links, f, parent); err != nil {
		return relationChange{}, err
	}
	link := item.Link(parent.Name())
	return relationChange{update: item.Update{Parent: &link}, changed: true,
		human: fmt.Sprintf("Set the parent of %s to %s\n", named(f, p), parent.ID())}, nil
}

func parentClear(_ *vault.Vault, f vault.ItemFile, p item.Parsed, _ itemLinks) (relationChange, error) {
	hasBad := false
	for _, bad := range p.BadRelations {
		hasBad = hasBad || bad.Key == "parent"
	}
	if p.Parent == nil && !hasBad {
		return relationChange{human: fmt.Sprintf("%s has no parent\n", named(f, p))}, nil
	}
	return relationChange{update: item.Update{ClearParent: true}, changed: true,
		human: fmt.Sprintf("Cleared the parent of %s\n", named(f, p))}, nil
}

func block(v *vault.Vault, f vault.ItemFile, p item.Parsed, links itemLinks, ref string) (relationChange, error) {
	blocker, err := resolveTarget(v, f.Key, ref, "blocker")
	if err != nil {
		return relationChange{}, err
	}
	if blocker == f {
		return relationChange{}, selfEdge(f, "blocker")
	}
	existing, err := matchingLinks(f, "blocked_by", p.BlockedBy, links, blocker)
	if err != nil {
		return relationChange{}, err
	}
	if len(existing) > 0 {
		return relationChange{human: fmt.Sprintf("%s is already blocked by %s\n", named(f, p), blocker.ID())}, nil
	}
	if err := checkBlockingCycle(v, links, f, blocker); err != nil {
		return relationChange{}, err
	}
	return relationChange{update: item.Update{AddBlockers: []string{item.Link(blocker.Name())}}, changed: true,
		human: fmt.Sprintf("%s is now blocked by %s\n", named(f, p), blocker.ID())}, nil
}

func unblock(v *vault.Vault, f vault.ItemFile, p item.Parsed, links itemLinks, ref string) (relationChange, error) {
	blocker, err := resolveTarget(v, f.Key, ref, "blocker")
	if err != nil {
		return relationChange{}, err
	}
	existing, err := matchingLinks(f, "blocked_by", p.BlockedBy, links, blocker)
	if err != nil {
		return relationChange{}, err
	}
	if len(existing) == 0 {
		return relationChange{human: fmt.Sprintf("%s is not blocked by %s\n", named(f, p), blocker.ID())}, nil
	}
	return relationChange{update: item.Update{RemoveBlockers: existing}, changed: true,
		human: fmt.Sprintf("%s is no longer blocked by %s\n", named(f, p), blocker.ID())}, nil
}
