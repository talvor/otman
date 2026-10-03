package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/talvor/otman/internal/config"
	"github.com/talvor/otman/internal/item"
	"github.com/talvor/otman/internal/output"
	"github.com/talvor/otman/internal/vault"
)

type listFlags struct {
	allProjects bool
	paging      *paging
}

func (a *app) newListCmd() *cobra.Command {
	var f listFlags
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the Items of the selected Project (default: open ones)",
		Args:  cobra.NoArgs,
	}
	cmd.Flags().BoolVar(&f.allProjects, "all-projects", false, "list the Items of every Project (conflicts with --project)")
	f.paging = addPaging(cmd)
	cmd.RunE = func(*cobra.Command, []string) error { return a.list(f) }
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

func (a *app) list(f listFlags) error {
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
			found, err := listProject(v, key)
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

// listProject is the summaries of Project key's open Items, sorted by
// number, then path.
func listProject(v *vault.Vault, key string) ([]itemSummary, error) {
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
		parsed := item.Parse(data)
		if parsed.Status == nil || *parsed.Status != "open" {
			continue
		}
		found = append(found, newItemSummary(f, parsed, data, links))
	}
	return found, nil
}
