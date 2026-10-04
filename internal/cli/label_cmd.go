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

func (a *app) newLabelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "label",
		Short: "Show the Labels in use",
	}
	var allProjects bool
	list := &cobra.Command{
		Use:   "list [--all-projects] [--limit N] [--offset N] [--all]",
		Short: "List the Labels the Items of the selected Project carry, with open and total counts",
		Args:  cobra.NoArgs,
	}
	list.Flags().BoolVar(&allProjects, "all-projects", false, "count the Items of every Project (conflicts with --project)")
	p := addPaging(list)
	list.RunE = func(*cobra.Command, []string) error { return a.labelList(allProjects, p) }
	cmd.AddCommand(list)
	return cmd
}

// labelCount is a Label in use: how many open Items carry it, and how
// many Items in all.
type labelCount struct {
	Name  string `json:"name"`
	Open  int    `json:"open"`
	Total int    `json:"total"`
}

type labelListResult struct {
	collection[labelCount]
}

func (r labelListResult) RenderHuman(w io.Writer) error {
	switch {
	case r.Total == 0:
		_, err := fmt.Fprintln(w, "No Labels in use")
		return err
	case r.Count == 0:
		_, err := fmt.Fprintf(w, "No Labels at --offset %d; %d in use\n", r.offset, r.Total)
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "LABEL\tOPEN\tTOTAL")
	for _, l := range r.Items {
		fmt.Fprintf(tw, "%s\t%d\t%d\n", l.Name, l.Open, l.Total)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	return r.renderMore(w)
}

// labelList counts the Labels of the Items in the selected Project, or in
// every Project, lowercased and sorted by name. An Item carrying a Label
// in two cases counts once.
func (a *app) labelList(allProjects bool, p *paging) error {
	if err := p.validate(); err != nil {
		return err
	}
	s, err := a.settings()
	if err != nil {
		return err
	}
	if allProjects && s.Project.Source == config.FromFlag {
		return conflictingFlags("--all-projects", "--project",
			"pass either --all-projects or --project, not both")
	}
	var sel config.Value
	if !allProjects {
		if sel, err = selectedProject(s, "--all-projects"); err != nil {
			return err
		}
	}
	return a.withVault(s, func(v *vault.Vault, warnings []output.Problem) error {
		keys, ws, err := listScope(v, sel)
		if err != nil {
			return err
		}
		counts := map[string]*labelCount{}
		for _, key := range keys {
			files, err := v.ItemFiles(key)
			if err != nil {
				return ioError(err)
			}
			for _, f := range files {
				data, err := v.ReadItemFile(f)
				if err != nil {
					return ioError(err)
				}
				it := item.Parse(data)
				if it.FrontmatterErr != nil {
					ws = append(ws, malformedFrontmatter(f, it.FrontmatterErr))
					continue
				}
				ws = append(ws, invalidLabels(f, it)...)
				open := it.Status != nil && *it.Status == item.Open
				for _, l := range item.NormalizeLabels(it.Labels) {
					c := counts[l]
					if c == nil {
						c = &labelCount{Name: l}
						counts[l] = c
					}
					c.Total++
					if open {
						c.Open++
					}
				}
			}
		}
		all := make([]labelCount, 0, len(counts))
		for _, c := range counts {
			all = append(all, *c)
		}
		slices.SortFunc(all, func(a, b labelCount) int { return strings.Compare(a.Name, b.Name) })
		return a.emit(labelListResult{page(p, all)}, append(warnings, ws...))
	})
}
