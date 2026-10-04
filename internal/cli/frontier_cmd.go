package cli

import (
	"github.com/spf13/cobra"
	"github.com/talvor/otman/internal/item"
	"github.com/talvor/otman/internal/vault"
)

// newFrontierCmd lists the Frontier: the open, unclaimed Items with no
// open blockers. It is list with those rules fixed, so it has no --state,
// --assignee, --unassigned or --blocked-by flags, and passing one fails
// as an unknown flag.
func (a *app) newFrontierCmd() *cobra.Command {
	f := listFlags{state: item.Open, unassigned: true, unblocked: true}
	cmd := &cobra.Command{
		Use:   "frontier [--kind K] [--label L]... [--parent REF] [--search TEXT] [--all-projects] [--limit N] [--offset N] [--all]",
		Short: "List the Frontier: the open, unclaimed Items with no open blockers",
		Args:  cobra.NoArgs,
	}
	fl := cmd.Flags()
	fl.StringVar(&f.kind, "kind", "", "only Items of this Kind: issue, prd or spec")
	fl.StringArrayVar(&f.labels, "label", nil, "only Items with the Label L (repeatable; Items must have every one)")
	fl.StringVar(&f.parent, "parent", "", "the Frontier of REF: only its direct children, in REF's Project")
	fl.StringVar(&f.search, "search", "", "only Items whose title or body contains TEXT, ignoring case")
	fl.BoolVar(&f.allProjects, "all-projects", false, "list the Frontier of every Project (conflicts with --project)")
	f.paging = addPaging(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error { return a.list(cmd, f) }
	return cmd
}

// openByPath caches, by path, whether each blocker read so far in one
// Project is open.
type openByPath map[string]bool

// isOpen reports whether blocker b of Item file f is open: whether its
// status is anything but closed. A blocker it cannot read fails rather
// than be taken as closed.
func (o openByPath) isOpen(v *vault.Vault, f, b vault.ItemFile) (bool, error) {
	if open, ok := o[b.Path]; ok {
		return open, nil
	}
	data, err := v.ReadItemFile(b)
	if err != nil {
		return false, ioError(err)
	}
	p := item.Parse(data)
	if p.FrontmatterErr != nil {
		e := unreadableTarget(b, "the status of "+b.ID()+", a blocker of "+f.ID(), p.FrontmatterErr)
		e.Details["blocked"] = f.ID()
		return false, e
	}
	open := p.Status == nil || *p.Status != item.Closed
	o[b.Path] = open
	return open, nil
}
