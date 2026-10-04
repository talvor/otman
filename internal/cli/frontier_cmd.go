package cli

import (
	"github.com/spf13/cobra"
	"github.com/talvor/otman/internal/item"
	"github.com/talvor/otman/internal/vault"
)

// newFrontierCmd is list with fixed readiness rules: open, unassigned and
// no open blockers. It has no --state, --assignee, --unassigned or
// --blocked-by flags, so passing one fails as an unknown flag.
func (a *app) newFrontierCmd() *cobra.Command {
	f := listFlags{state: item.Open, unassigned: true, unblocked: true}
	cmd := &cobra.Command{
		Use:   "frontier [--kind K] [--label L]... [--parent REF] [--search TEXT] [--all-projects] [--limit N] [--offset N] [--all]",
		Short: "List the ready Items: open, unassigned and with no open blockers",
		Args:  cobra.NoArgs,
	}
	fl := cmd.Flags()
	fl.StringVar(&f.kind, "kind", "", "only Items of this Kind: issue, prd or spec")
	fl.StringArrayVar(&f.labels, "label", nil, "only Items with the Label L (repeatable; Items must have every one)")
	fl.StringVar(&f.parent, "parent", "", "only the direct children of REF, in REF's Project")
	fl.StringVar(&f.search, "search", "", "only Items whose title or body contains TEXT, ignoring case")
	fl.BoolVar(&f.allProjects, "all-projects", false, "list the ready Items of every Project (conflicts with --project)")
	f.paging = addPaging(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error { return a.list(cmd, f) }
	return cmd
}

// blockerStatus remembers, by path, whether each blocker read so far in
// one Project is open.
type blockerStatus map[string]bool

// blocked reports whether Item file f, parsed as p, has an open blocker.
// Every blocked_by link must resolve and every blocker must be readable,
// or frontier fails rather than guess: a blocker it cannot see is never
// taken as closed.
func (st blockerStatus) blocked(v *vault.Vault, links itemLinks, f vault.ItemFile, p item.Parsed) (bool, error) {
	blockers, err := resolveLinks(links, f, p, "blocked_by")
	if err != nil {
		return false, err
	}
	blocked := false
	for _, b := range blockers {
		open, err := st.open(v, f, b)
		if err != nil {
			return false, err
		}
		blocked = blocked || open
	}
	return blocked, nil
}

// open reports whether blocker b of Item file f is open: whether its
// status is anything but closed.
func (st blockerStatus) open(v *vault.Vault, f, b vault.ItemFile) (bool, error) {
	if open, ok := st[b.Path]; ok {
		return open, nil
	}
	data, err := v.ReadItemFile(b)
	if err != nil {
		return false, ioError(err)
	}
	p := item.Parse(data)
	if p.FrontmatterErr != nil {
		return false, &Error{Exit: ExitConflict, Code: "malformed_frontmatter",
			Message: "cannot read the status of " + b.ID() + ", a blocker of " + f.ID() + ": " + p.FrontmatterErr.Error(),
			Details: map[string]any{"id": b.ID(), "path": b.Path, "blocked": f.ID()},
			Hint:    "fix the YAML between the --- lines in " + b.Path + ", then retry"}
	}
	open := p.Status == nil || *p.Status != item.Closed
	st[b.Path] = open
	return open, nil
}
