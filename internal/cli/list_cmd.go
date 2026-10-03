package cli

import (
	"fmt"
	"io"
	"slices"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/talvor/otman/internal/config"
	"github.com/talvor/otman/internal/item"
	"github.com/talvor/otman/internal/output"
	"github.com/talvor/otman/internal/vault"
)

type listFlags struct {
	state       string
	kind        string
	assignee    string
	unassigned  bool
	allProjects bool
	paging      *paging
}

// listStates are the values of list --state.
var listStates = []string{"open", "closed", "all"}

func (a *app) newListCmd() *cobra.Command {
	var f listFlags
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the Items of the selected Project (default: open ones)",
		Args:  cobra.NoArgs,
	}
	fl := cmd.Flags()
	fl.StringVar(&f.state, "state", "open", "open, closed or all")
	fl.StringVar(&f.kind, "kind", "", "only Items of this Kind: issue, prd or spec")
	fl.StringVar(&f.assignee, "assignee", "", "only Items assigned to NAME, or to the actor with @me")
	fl.BoolVar(&f.unassigned, "unassigned", false, "only Items with no assignee (conflicts with --assignee)")
	fl.BoolVar(&f.allProjects, "all-projects", false, "list the Items of every Project (conflicts with --project)")
	f.paging = addPaging(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error { return a.list(cmd, f) }
	return cmd
}

type listResult struct {
	collection[itemSummary]
}

func (r listResult) RenderHuman(w io.Writer) error {
	if r.Total == 0 {
		_, err := fmt.Fprintln(w, "No Items match")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tKIND\tSTATUS\tASSIGNEE\tTITLE")
	for _, it := range r.Items {
		kind := "-"
		if it.Kind != nil {
			kind = string(*it.Kind)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", it.ID, kind, orDash(it.Status), orDash(it.Assignee), it.Title)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	return r.renderMore(w)
}

// itemQuery is what list keeps of the Items in scope.
type itemQuery struct {
	state string     // open, closed or all
	kind  *item.Kind // nil for any Kind
	// assignee keeps only Items assigned to it; nil for any assignee.
	assignee   *string
	unassigned bool
}

// match reports whether the Item summarised by s is kept. An Item whose
// status is neither open nor closed is never kept.
func (q itemQuery) match(s itemSummary) bool {
	if s.Status == nil || (*s.Status != "open" && *s.Status != "closed") {
		return false
	}
	if q.state != "all" && *s.Status != q.state {
		return false
	}
	if q.kind != nil && (s.Kind == nil || *s.Kind != *q.kind) {
		return false
	}
	if q.assignee != nil && (s.Assignee == nil || *s.Assignee != *q.assignee) {
		return false
	}
	if q.unassigned && s.Assignee != nil {
		return false
	}
	return true
}

// query validates the filter flags. The caller resolves --assignee, which
// may name the actor.
func (f listFlags) query(cmd *cobra.Command) (itemQuery, error) {
	q := itemQuery{state: f.state, unassigned: f.unassigned}
	if !slices.Contains(listStates, f.state) {
		return itemQuery{}, invalid("invalid_state", "unknown state "+quoteArg(f.state),
			map[string]any{"state": f.state, "allowed": listStates}, "use --state open, closed or all")
	}
	if cmd.Flags().Changed("kind") {
		kind, err := parseKind(f.kind)
		if err != nil {
			return itemQuery{}, err
		}
		q.kind = &kind
	}
	if f.unassigned && cmd.Flags().Changed("assignee") {
		return itemQuery{}, invalid("conflicting_flags", "--assignee conflicts with --unassigned",
			map[string]any{"flags": []string{"--assignee", "--unassigned"}},
			"pass either --assignee or --unassigned, not both")
	}
	return q, nil
}

func (a *app) list(cmd *cobra.Command, f listFlags) error {
	q, err := f.query(cmd)
	if err != nil {
		return err
	}
	if err := f.paging.validate(); err != nil {
		return err
	}
	if f.allProjects && a.root.PersistentFlags().Changed(config.Project.Name) {
		return invalid("conflicting_flags", "--all-projects conflicts with --project",
			map[string]any{"flags": []string{"--all-projects", "--project"}},
			"pass either --all-projects or --project, not both")
	}
	s, err := a.settings()
	if err != nil {
		return err
	}
	if cmd.Flags().Changed("assignee") {
		name, err := nameOrActor(s, "--assignee", f.assignee)
		if err != nil {
			return err
		}
		q.assignee = &name
	}
	var sel config.Value
	if !f.allProjects {
		if sel, err = s.project(); err != nil {
			return err
		}
		if !sel.IsSet() {
			return noProject("--all-projects")
		}
	}
	return a.withVault(s, func(v *vault.Vault, warnings []output.Problem) error {
		keys, ws, err := listScope(v, sel)
		if err != nil {
			return err
		}
		all := []itemSummary{}
		for _, key := range keys {
			found, err := listProject(v, key, q)
			if err != nil {
				return ioError(err)
			}
			all = append(all, found...)
		}
		return a.emit(listResult{page(f.paging, all)}, append(warnings, ws...))
	})
}

// listScope is the keys of the Projects a list spans, in key order: the
// selected Project, which must exist, or every Project when sel is unset.
func listScope(v *vault.Vault, sel config.Value) ([]string, []output.Problem, error) {
	if sel.IsSet() {
		if err := requireProject(v, sel.Value, map[string]any{"source": string(sel.Source)}); err != nil {
			return nil, nil, err
		}
		return []string{sel.Value}, nil, nil
	}
	ps, ws, err := v.Projects()
	if err != nil {
		return nil, nil, ioError(err)
	}
	keys := make([]string, len(ps))
	for i, p := range ps {
		keys[i] = p.Key
	}
	return keys, ws, nil
}

// listProject is the summaries of the Items of Project key that q keeps,
// sorted by number, then path.
func listProject(v *vault.Vault, key string, q itemQuery) ([]itemSummary, error) {
	files, err := v.ItemFiles(key)
	if err != nil {
		return nil, err
	}
	links := newItemLinks(files)
	var found []itemSummary
	for _, f := range files {
		data, err := v.ReadItemFile(f)
		if err != nil {
			return nil, err
		}
		if s := newItemSummary(f, item.Parse(data), data, links); q.match(s) {
			found = append(found, s)
		}
	}
	return found, nil
}
