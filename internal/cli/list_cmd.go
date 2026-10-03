package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/talvor/otman/internal/item"
	"github.com/talvor/otman/internal/output"
	"github.com/talvor/otman/internal/vault"
)

func (a *app) newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the Items of the selected Project (default: open ones)",
		Args:  cobra.NoArgs,
	}
	p := addPaging(cmd)
	cmd.RunE = func(*cobra.Command, []string) error { return a.list(p) }
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

func (a *app) list(p *paging) error {
	if err := p.validate(); err != nil {
		return err
	}
	s, err := a.settings()
	if err != nil {
		return err
	}
	sel, err := selectedProject(s)
	if err != nil {
		return err
	}
	return a.withVault(s, func(v *vault.Vault, warnings []output.Problem) error {
		if err := requireProject(v, sel.Value, map[string]any{"source": string(sel.Source)}); err != nil {
			return err
		}
		files, err := v.ItemFiles(sel.Value)
		if err != nil {
			return ioError(err)
		}
		links := newItemLinks(files)
		all := []itemSummary{}
		for _, f := range files {
			data, err := v.ReadItemFile(f)
			if err != nil {
				return ioError(err)
			}
			parsed := item.Parse(data)
			if parsed.Status == nil || *parsed.Status != "open" {
				continue
			}
			all = append(all, newItemSummary(f, parsed, data, links))
		}
		return a.emit(listResult{page(p, all)}, warnings)
	})
}
