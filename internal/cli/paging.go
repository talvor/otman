package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// defaultLimit bounds every collection unless --limit or --all says
// otherwise.
const defaultLimit = 50

// paging is the --limit, --offset and --all flags every collection takes.
type paging struct {
	cmd    *cobra.Command
	limit  int
	offset int
	all    bool
}

func addPaging(cmd *cobra.Command) *paging {
	p := &paging{cmd: cmd}
	f := cmd.Flags()
	f.IntVar(&p.limit, "limit", defaultLimit, "return at most N results")
	f.IntVar(&p.offset, "offset", 0, "skip the first N results")
	f.BoolVar(&p.all, "all", false, "return every result (conflicts with --limit and --offset)")
	return p
}

func (p *paging) validate() error {
	f := p.cmd.Flags()
	if p.all {
		for _, name := range []string{"limit", "offset"} {
			if f.Changed(name) {
				return conflictingFlags("--all", "--"+name, "pass either --all or --limit/--offset")
			}
		}
	}
	if p.limit < 1 {
		return invalid("invalid_arguments", "--limit must be a positive integer",
			map[string]any{"flag": "--limit", "value": p.limit}, "")
	}
	if p.offset < 0 {
		return invalid("invalid_arguments", "--offset must be zero or more",
			map[string]any{"flag": "--offset", "value": p.offset}, "")
	}
	return nil
}

// collection is the shape every list result shares: one page of items
// plus the total, the page size and whether more follow.
type collection[T any] struct {
	Items   []T  `json:"items"`
	Total   int  `json:"total"`
	Count   int  `json:"count"`
	HasMore bool `json:"has_more"`

	offset int
}

// page cuts one page out of all, which is already sorted.
func page[T any](p *paging, all []T) collection[T] {
	c := collection[T]{Items: []T{}, Total: len(all), offset: p.offset}
	if p.all {
		c.Items = append(c.Items, all...)
		c.offset = 0
	} else if p.offset < len(all) {
		end := p.offset + min(p.limit, len(all)-p.offset)
		c.Items = append(c.Items, all[p.offset:end]...)
		c.HasMore = end < len(all)
	}
	c.Count = len(c.Items)
	return c
}

// renderMore tells a human reader that the page is incomplete and how to
// see the rest.
func (c collection[T]) renderMore(w io.Writer) error {
	if !c.HasMore {
		return nil
	}
	next := c.offset + c.Count
	_, err := fmt.Fprintf(w, "\nShowing %d-%d of %d; pass --offset %d for more, or --all\n",
		c.offset+1, next, c.Total, next)
	return err
}
