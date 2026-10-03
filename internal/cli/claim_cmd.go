package cli

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"github.com/talvor/otman/internal/config"
	"github.com/talvor/otman/internal/item"
	"github.com/talvor/otman/internal/output"
	"github.com/talvor/otman/internal/vault"
)

// claimCommand is a command that sets or clears the actor's Claim on an
// Item: claim or release.
type claimCommand struct {
	use, short string
	claim      bool
}

var claimCommands = []claimCommand{
	{"claim", "Assign an open Item to the actor", true},
	{"release", "Unassign an Item the actor holds", false},
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

// setClaim claims or releases the Item ref names for the actor. A claim
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
	update := item.Update{Assignee: &actor}
	if !c.claim {
		update = item.Update{ClearAssignee: true}
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
			return claimConflict(c, f, *before.Assignee, actor)
		}
		if !c.claim && before.Assignee == nil {
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
		var human string
		switch {
		case changed && c.claim:
			human = fmt.Sprintf("Claimed %s · %s\n", summary.ID, summary.Title)
		case changed:
			human = fmt.Sprintf("Released %s · %s\n", summary.ID, summary.Title)
		case c.claim:
			human = fmt.Sprintf("%s · %s is already claimed by %s\n", summary.ID, summary.Title, actor)
		default:
			human = fmt.Sprintf("%s · %s is already unassigned\n", summary.ID, summary.Title)
		}
		return a.emit(mutationResult{summary, changed, human}, warnings)
	})
}

// requireActor is the actor a command that records an identity acts as,
// failing with no_actor when none is configured.
func requireActor(s resolved, command string) (string, error) {
	if !s.Actor.IsSet() {
		return "", invalid("no_actor", command+" needs an actor, and none is configured", nil,
			"pass --actor NAME, set "+config.Actor.EnvVar+", or run 'otman config set actor NAME'")
	}
	actor := s.Actor.Value
	if !utf8.ValidString(actor) || strings.ContainsAny(actor, "\r\n") {
		return "", invalid("invalid_arguments", "the actor must be a single-line UTF-8 name",
			map[string]any{"source": string(s.Actor.Source)}, "")
	}
	return actor, nil
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

// claimConflict refuses to claim or release Item file f, which assignee
// holds rather than actor.
func claimConflict(c claimCommand, f vault.ItemFile, assignee, actor string) error {
	hint := "pick another Item, or reassign this one deliberately with 'otman edit " + f.ID() + " --assignee @me'"
	if !c.claim {
		hint = "leave the release to " + assignee + ", or unassign deliberately with 'otman edit " + f.ID() + " --clear-assignee'"
	}
	return &Error{Exit: ExitConflict, Code: "claim_conflict",
		Message: "cannot " + c.use + " " + f.ID() + ": it is claimed by " + assignee + ", not " + actor,
		Details: map[string]any{"path": f.Path, "assignee": assignee, "actor": actor}, Hint: hint}
}
