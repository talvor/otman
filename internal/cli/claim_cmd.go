package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/talvor/otman/internal/item"
	"github.com/talvor/otman/internal/output"
	"github.com/talvor/otman/internal/vault"
)

// claimCommand is a command that takes or gives up the Actor's Claim on
// an Item: claim or release.
type claimCommand struct {
	use, short string
	claim      bool   // take the Claim rather than give it up
	done       string // what a change reports, such as "Claimed"
	// unchanged is what a no-op reports after the Item, given the Actor.
	unchanged func(actor string) string
	// conflictHint is the hint of claim_conflict, given the Item's ID and
	// the assignee.
	conflictHint func(id, assignee string) string
}

var claimCommands = []claimCommand{
	{"claim", "Claim an open Item for the actor", true, "Claimed",
		func(actor string) string { return "is already claimed by " + actor },
		func(id, _ string) string {
			return "pick another Item, or reassign this one deliberately with 'otman edit " + id + " --assignee @me'"
		}},
	{"release", "Release the actor's Claim on an Item", false, "Released",
		func(string) string { return "is already unclaimed" },
		func(id, assignee string) string {
			return "leave the release to " + assignee + ", or unassign deliberately with 'otman edit " + id + " --clear-assignee'"
		}},
}

func (a *app) newClaimCmd(c claimCommand) *cobra.Command {
	return &cobra.Command{
		Use:   c.use + " REF",
		Short: c.short,
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return a.setClaim(c, args[0])
		},
	}
}

// setClaim claims or releases the Item ref names for the Actor. A claim
// needs an open Item, blocked or not; a release works at either status.
// Neither takes an Item from someone else. The check and the write happen
// under the Vault lock, so two claims on one device never both win.
func (a *app) setClaim(c claimCommand, ref string) error {
	s, err := a.settings()
	if err != nil {
		return err
	}
	actor, err := requireActor(s, c.use)
	if err != nil {
		return err
	}
	update := item.Update{ClearAssignee: true}
	if c.claim {
		update = item.Update{Assignee: &actor}
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
		// Apply first, so a file otman could not rewrite is refused with
		// unsafe_write whatever its frontmatter seems to say.
		out, changed, err := item.Apply(data, update, a.opts.Now())
		if err != nil {
			return writeError(f, err)
		}
		before := item.Parse(data)
		if c.claim && (before.Status == nil || *before.Status != item.Open) {
			return notOpen(f, before.Status)
		}
		if before.Assignee != nil && *before.Assignee != actor {
			return &Error{Exit: ExitConflict, Code: "claim_conflict",
				Message: "cannot " + c.use + " " + f.ID() + ": it is claimed by " + *before.Assignee + ", not " + actor,
				Details: map[string]any{"path": f.Path, "assignee": *before.Assignee, "actor": actor},
				Hint:    c.conflictHint(f.ID(), *before.Assignee)}
		}
		if !c.claim && before.Assignee == nil {
			// Already unclaimed: clearing would only add an assignee: null
			// line to a file that has no assignee key.
			out, changed = data, false
		}
		if changed {
			if err := v.WriteItemFile(f, out); err != nil {
				return ioError(err)
			}
		}
		files, err := v.ItemFiles(f.Key)
		if err != nil {
			return ioError(err)
		}
		summary := newItemSummary(f, item.Parse(out), out, newItemLinks(files))
		human := fmt.Sprintf("%s %s · %s\n", c.done, summary.ID, summary.Title)
		if !changed {
			human = fmt.Sprintf("%s · %s %s\n", summary.ID, summary.Title, c.unchanged(actor))
		}
		return a.emit(mutationResult{summary, changed, human}, warnings)
	})
}

// notOpen refuses a claim on Item file f, whose status is closed, invalid
// or missing.
func notOpen(f vault.ItemFile, status *string) error {
	msg := "cannot claim " + f.ID() + ": it has no status"
	hint := "set status: open in " + f.Path + ", then retry"
	if status != nil {
		msg = "cannot claim " + f.ID() + ": it is " + quoteArg(*status) + ", not open"
	}
	if status != nil && *status == item.Closed {
		msg = "cannot claim " + f.ID() + ": it is closed"
		hint = "run 'otman reopen " + f.ID() + "' first if the work is not done"
	}
	return &Error{Exit: ExitConflict, Code: "item_not_open", Message: msg,
		Details: map[string]any{"path": f.Path, "status": status}, Hint: hint}
}
