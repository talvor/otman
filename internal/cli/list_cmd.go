package cli

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/talvor/otman/internal/config"
	"github.com/talvor/otman/internal/item"
	"github.com/talvor/otman/internal/output"
	"github.com/talvor/otman/internal/vault"
)

type listFlags struct {
	state         string
	kind          string
	assignee      string
	unassigned    bool
	search        string
	labels        []string
	withoutLabels []string
	unlabeled     bool
	allProjects   bool
	parent        string
	blockedBy     string
	// unblocked is set by frontier, which has no flag for it.
	unblocked bool
	paging    *paging
}

// listStates are the values of list --state.
var listStates = []string{item.Open, item.Closed, "all"}

func (a *app) newListCmd() *cobra.Command {
	var f listFlags
	cmd := &cobra.Command{
		Use:   "list [--state open|closed|all] [--kind K] [--label L]... [--without-label L]... [--unlabeled] [--assignee NAME|@me | --unassigned] [--parent REF] [--blocked-by REF] [--search TEXT] [--all-projects] [--limit N] [--offset N] [--all]",
		Short: "List the Items of the selected Project (default: open ones)",
		Args:  cobra.NoArgs,
	}
	fl := cmd.Flags()
	fl.StringVar(&f.state, "state", "open", "open, closed or all")
	fl.StringVar(&f.kind, "kind", "", "only Items of this Kind: issue, prd or spec")
	fl.StringArrayVar(&f.labels, "label", nil, "only Items with the Label L (repeatable; Items must have every one)")
	fl.StringArrayVar(&f.withoutLabels, "without-label", nil, "only Items without the Label L (repeatable)")
	fl.BoolVar(&f.unlabeled, "unlabeled", false, "only Items with no Labels (conflicts with --label)")
	fl.StringVar(&f.assignee, "assignee", "", "only Items assigned to NAME, or to the actor with @me")
	fl.BoolVar(&f.unassigned, "unassigned", false, "only Items with no assignee (conflicts with --assignee)")
	fl.StringVar(&f.parent, "parent", "", "only the direct children of REF, in REF's Project")
	fl.StringVar(&f.blockedBy, "blocked-by", "", "only the Items REF blocks, in REF's Project")
	fl.StringVar(&f.search, "search", "", "only Items whose title or body contains TEXT, ignoring case")
	fl.BoolVar(&f.allProjects, "all-projects", false, "list the Items of every Project (conflicts with --project)")
	f.paging = addPaging(cmd)
	cmd.RunE = func(cmd *cobra.Command, _ []string) error { return a.list(cmd, f) }
	return cmd
}

type listResult struct {
	collection[itemSummary]
}

func (r listResult) RenderHuman(w io.Writer) error {
	switch {
	case r.Total == 0:
		_, err := fmt.Fprintln(w, "No Items match")
		return err
	case r.Count == 0:
		_, err := fmt.Fprintf(w, "No Items at --offset %d; %d match\n", r.offset, r.Total)
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tKIND\tSTATUS\tASSIGNEE\tTITLE")
	for _, it := range r.Items {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", it.ID, kindOrDash(it.Kind), orDash(it.Status), orDash(it.Assignee), it.Title)
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
	// search is the lowercased text the title or body must contain; ""
	// for no search.
	search string
	// labels are Labels an Item must all have, withoutLabels those it
	// must not have, both lowercase; unlabeled keeps only Items with no
	// Labels.
	labels, withoutLabels []string
	unlabeled             bool
	// parent keeps only the direct children of an Item, and blockedBy
	// only the Items it blocks; nil for no such filter.
	parent, blockedBy *vault.ItemFile
	// unblocked keeps only Items with no open blockers. Every blocked_by
	// link of an Item it would keep must resolve.
	unblocked bool
}

// match reports whether the Item summarised by s, with body, passes every
// filter but --state.
func (q itemQuery) match(s itemSummary, body string) bool {
	if q.kind != nil && (s.Kind == nil || *s.Kind != *q.kind) {
		return false
	}
	if q.assignee != nil && (s.Assignee == nil || *s.Assignee != *q.assignee) {
		return false
	}
	if q.unassigned && s.Assignee != nil {
		return false
	}
	if q.unlabeled && len(s.Labels) > 0 {
		return false
	}
	for _, l := range q.labels {
		if !item.HasLabel(s.Labels, l) {
			return false
		}
	}
	for _, l := range q.withoutLabels {
		if item.HasLabel(s.Labels, l) {
			return false
		}
	}
	if q.search != "" && !strings.Contains(strings.ToLower(s.Title), q.search) &&
		!strings.Contains(strings.ToLower(body), q.search) {
		return false
	}
	return true
}

// query validates the filter flags. The caller resolves --assignee, which
// may name the actor.
func (f listFlags) query(cmd *cobra.Command) (itemQuery, error) {
	q := itemQuery{state: f.state, unassigned: f.unassigned, unlabeled: f.unlabeled,
		search: strings.ToLower(f.search), unblocked: f.unblocked}
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
		return itemQuery{}, conflictingFlags("--assignee", "--unassigned",
			"pass either --assignee or --unassigned, not both")
	}
	if f.unlabeled && cmd.Flags().Changed("label") {
		return itemQuery{}, conflictingFlags("--label", "--unlabeled",
			"pass either --label or --unlabeled, not both")
	}
	var err error
	if q.labels, err = parseLabels("--label", f.labels); err != nil {
		return itemQuery{}, err
	}
	if q.withoutLabels, err = parseLabels("--without-label", f.withoutLabels); err != nil {
		return itemQuery{}, err
	}
	for _, flag := range []string{"parent", "blocked-by"} {
		if v, _ := cmd.Flags().GetString(flag); cmd.Flags().Changed(flag) && v == "" {
			return itemQuery{}, invalid("invalid_arguments", "--"+flag+" cannot be empty",
				map[string]any{"flag": "--" + flag}, "pass an Item, or omit --"+flag)
		}
	}
	if cmd.Flags().Changed("search") && f.search == "" {
		return itemQuery{}, invalid("invalid_arguments", "--search cannot be empty",
			map[string]any{"flag": "--search"}, "pass the text to search for, or omit --search")
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
	s, err := a.settings()
	if err != nil {
		return err
	}
	// Only an explicit --project conflicts; a default Project does not.
	if f.allProjects && s.Project.Source == config.FromFlag {
		return conflictingFlags("--all-projects", "--project",
			"pass either --all-projects or --project, not both")
	}
	if cmd.Flags().Changed("assignee") {
		name, err := nameOrActor(s, "--assignee", f.assignee)
		if err != nil {
			return err
		}
		q.assignee = &name
	}
	selectors := f.parent != "" || f.blockedBy != ""
	// A relation selector sets the Project scope, so it excludes
	// --all-projects as --project does.
	if f.allProjects && selectors {
		other := "--parent"
		if f.parent == "" {
			other = "--blocked-by"
		}
		return conflictingFlags("--all-projects", other,
			"drop --all-projects; "+other+" lists within its Item's Project")
	}
	var sel config.Value
	if !f.allProjects && !selectors {
		if sel, err = selectedProject(s, "--all-projects"); err != nil {
			return err
		}
	}
	return a.withVault(s, func(v *vault.Vault, warnings []output.Problem) error {
		if selectors {
			// A relation selector names the Project to list.
			if sel, err = q.resolveSelectors(s, v, f); err != nil {
				return err
			}
		}
		keys, ws, err := listScope(v, sel)
		if err != nil {
			return err
		}
		all := []itemSummary{}
		inUse := labelSet{}
		for _, key := range keys {
			found, pws, err := listProject(v, key, q, inUse)
			if err != nil {
				return err
			}
			all = append(all, found...)
			ws = append(ws, pws...)
		}
		ws = append(ws, unknownLabelWarnings("--label", q.labels, inUse)...)
		ws = append(ws, unknownLabelWarnings("--without-label", q.withoutLabels, inUse)...)
		return a.emit(listResult{page(f.paging, all)}, append(warnings, ws...))
	})
}

// resolveSelectors resolves --parent and --blocked-by into q and returns
// the Project they set as the scope. Relations stay within one Project,
// so selectors in two Projects conflict.
func (q *itemQuery) resolveSelectors(s resolved, v *vault.Vault, f listFlags) (config.Value, error) {
	var sel config.Value
	var first string
	for _, x := range []struct {
		flag, ref string
		into      **vault.ItemFile
	}{{"--parent", f.parent, &q.parent}, {"--blocked-by", f.blockedBy, &q.blockedBy}} {
		if x.ref == "" {
			continue
		}
		t, err := resolveRef(s, v, x.ref)
		if err != nil {
			return config.Value{}, err
		}
		if sel.IsSet() && sel.Value != t.Key {
			return config.Value{}, invalid("project_mismatch",
				first+" is in Project "+sel.Value+", but "+x.flag+" "+x.ref+" is in Project "+t.Key,
				map[string]any{"flags": []string{"--parent", "--blocked-by"}, "projects": []string{sel.Value, t.Key}},
				"name Items of one Project; relations stay within a Project")
		}
		*x.into = &t
		sel = config.Value{Value: t.Key, Source: config.FromFlag}
		first = x.flag + " " + x.ref
	}
	return sel, nil
}

// related reports whether the Item file f, parsed as p, passes --parent
// and --blocked-by. A relation link ambiguous between the selector and
// other Items fails rather than give a partial answer.
func (q itemQuery) related(f vault.ItemFile, p item.Parsed, links itemLinks) (bool, error) {
	for _, x := range []struct {
		key    string
		target *vault.ItemFile
	}{{"parent", q.parent}, {"blocked_by", q.blockedBy}} {
		if x.target == nil {
			continue
		}
		found, err := matchingLinks(f, x.key, relationLinks(p, x.key), links, *x.target)
		if err != nil || len(found) == 0 {
			return false, err
		}
	}
	return true, nil
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
// sorted by number, then path, and a warning for each Item left out
// because it drifted: its frontmatter cannot be read, or only its status,
// missing or neither open nor closed, kept it from --state open or closed.
// --state all keeps any status. It adds the Labels of every Item it reads,
// kept or not, to inUse. With q.unblocked it fails on a blocker of an Item
// it would otherwise keep that it cannot resolve or read.
func listProject(v *vault.Vault, key string, q itemQuery, inUse labelSet) ([]itemSummary, []output.Problem, error) {
	files, err := v.ItemFiles(key)
	if err != nil {
		return nil, nil, ioError(err)
	}
	links := newItemLinks(files)
	blockers := blockerStatus{}
	var found []itemSummary
	var warnings []output.Problem
	for _, f := range files {
		data, err := v.ReadItemFile(f)
		if err != nil {
			return nil, nil, ioError(err)
		}
		p := item.Parse(data)
		if p.FrontmatterErr != nil {
			warnings = append(warnings, malformedFrontmatter(f, p.FrontmatterErr))
			continue
		}
		inUse.add(p.Labels)
		s := newItemSummary(f, p, data, links)
		if !q.match(s, p.Body) {
			continue
		}
		if ok, err := q.related(f, p, links); err != nil {
			return nil, nil, err
		} else if !ok {
			continue
		}
		if q.state == "all" || (s.Status != nil && *s.Status == q.state) {
			if q.unblocked {
				if blocked, err := blockers.blocked(v, links, f, p); err != nil {
					return nil, nil, err
				} else if blocked {
					continue
				}
			}
			found = append(found, s)
			continue
		}
		if s.Status == nil || (*s.Status != item.Open && *s.Status != item.Closed) {
			warnings = append(warnings, invalidStatus(s))
		}
	}
	return found, warnings, nil
}

// malformedFrontmatter warns that the Item file f was left out because
// its frontmatter cannot be read.
func malformedFrontmatter(f vault.ItemFile, err error) output.Problem {
	return output.Warning("malformed_frontmatter",
		"cannot read the frontmatter of "+f.Path+": "+err.Error(),
		map[string]any{"path": f.Path},
		"fix the YAML between the --- lines in "+f.Path)
}

// invalidStatus warns that the Item summarised by s was left out of a
// --state open or closed list because its status is neither.
func invalidStatus(s itemSummary) output.Problem {
	msg := s.ID + " has no status"
	if s.Status != nil {
		msg = s.ID + " has status " + quoteArg(*s.Status)
	}
	return output.Warning("invalid_status", msg+", not open or closed, so it is not listed",
		map[string]any{"id": s.ID, "path": s.Path, "status": s.Status},
		"set status: open or closed in "+s.Path+", or pass --state all")
}
