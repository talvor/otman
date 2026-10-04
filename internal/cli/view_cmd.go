package cli

import (
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/talvor/otman/internal/config"
	"github.com/talvor/otman/internal/item"
	"github.com/talvor/otman/internal/output"
	"github.com/talvor/otman/internal/vault"
)

// textLimit is how many Unicode characters of a body or comment human and
// AXI output show without --full.
const textLimit = 2000

func (a *app) newViewCmd() *cobra.Command {
	var comments, full bool
	cmd := &cobra.Command{
		Use:   "view REF",
		Short: "Show an Item by ID, number, full filename or Vault-relative path",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return a.view(args[0], comments, full)
		},
	}
	cmd.Flags().BoolVar(&comments, "comments", false, "include comments in human and AXI output")
	cmd.Flags().BoolVar(&full, "full", false, "show bodies and comments without the 2,000-character limit")
	return cmd
}

// viewResult is the JSON view: always the complete Item.
type viewResult struct {
	Item itemFull `json:"item"`
}

func (r viewResult) RenderHuman(w io.Writer) error {
	return newViewDisplay(r.Item, r.Item.ID, true, true).RenderHuman(w)
}

// viewDisplay is the human and AXI view: Item with its body possibly cut
// and its comments only when asked for. When text is cut or comments are
// left out, ReadAll is the command that shows everything.
type viewDisplay struct {
	Item            itemFull `json:"item"`
	Truncated       bool     `json:"truncated"`
	CommentsOmitted int      `json:"comments_omitted,omitempty"`
	ReadAll         *string  `json:"read_all,omitempty"`

	bodyCut     bool
	commentsCut []bool
}

func (a *app) view(ref string, comments, full bool) error {
	s, err := a.settings()
	if err != nil {
		return err
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
		files, err := v.ItemFiles(f.Key)
		if err != nil {
			return ioError(err)
		}
		links := newItemLinks(files)
		r, err := relatedTo(v, f, files, links)
		if err != nil {
			return ioError(err)
		}
		p := item.Parse(data)
		warnings = append(warnings, relationProblems(f, p, links)...)
		it := newItemFull(f, p, data, links, r)
		if a.out == output.JSON {
			return a.emit(viewResult{it}, warnings)
		}
		return a.emit(newViewDisplay(it, commandRef(f, files), comments, full), warnings)
	})
}

// newViewDisplay is it as the display shows it: with comments or without,
// and with text cut unless full. ref names it in the ReadAll command.
func newViewDisplay(it itemFull, ref string, comments, full bool) viewDisplay {
	all := *it.Comments
	d := viewDisplay{Item: it}
	if !full {
		d.Item.Body, d.bodyCut = item.TruncateText(it.Body, textLimit)
	}
	if comments {
		shown := make([]commentJSON, len(all))
		d.commentsCut = make([]bool, len(all))
		for i, c := range all {
			if !full {
				c.Body, d.commentsCut[i] = item.TruncateText(c.Body, textLimit)
			}
			shown[i] = c
		}
		d.Item.Comments = &shown
	} else {
		d.Item.Comments = nil
		d.CommentsOmitted = len(all)
	}
	d.Truncated = d.bodyCut
	for _, cut := range d.commentsCut {
		d.Truncated = d.Truncated || cut
	}
	if d.Truncated || d.CommentsOmitted > 0 {
		cmd := "otman view " + ref
		if len(all) > 0 {
			cmd += " --comments"
		}
		cmd += " --full"
		d.ReadAll = &cmd
	}
	return d
}

func (d viewDisplay) RenderHuman(w io.Writer) error {
	it := d.Item
	var b strings.Builder
	fmt.Fprintf(&b, "%s · %s\n", it.ID, it.Title)
	assignment := "unassigned"
	if it.Assignee != nil {
		assignment = "assigned to " + *it.Assignee
	}
	fmt.Fprintf(&b, "%s · %s · %s\n", kindOrDash(it.Kind), orDash(it.Status), assignment)
	if len(it.Labels) > 0 {
		fmt.Fprintf(&b, "Labels: %s\n", strings.Join(it.Labels, ", "))
	}
	fmt.Fprintf(&b, "Author: %s · created %s · updated %s\n", orDash(it.Author), orDash(it.Created), orDash(it.Updated))
	if it.Parent != nil {
		fmt.Fprintf(&b, "Parent: %s\n", refList([]refJSON{*it.Parent}))
	}
	for _, rel := range []struct {
		label string
		refs  []refJSON
	}{{"Blocked by", it.BlockedBy}, {"Children", it.Children}, {"Blocks", it.Blocks}} {
		if len(rel.refs) > 0 {
			fmt.Fprintf(&b, "%s: %s\n", rel.label, refList(rel.refs))
		}
	}
	fmt.Fprintf(&b, "Path: %s\n\n", it.Path)
	if it.Body == "" {
		b.WriteString("(no body)\n")
	} else {
		b.WriteString(withNewline(it.Body))
	}
	if d.bodyCut {
		fmt.Fprintf(&b, "… body truncated after %s characters.\n", humanCount(textLimit))
	}
	if it.Comments != nil {
		fmt.Fprintf(&b, "\nComments (%d)\n", len(*it.Comments))
		for i, c := range *it.Comments {
			fmt.Fprintf(&b, "\n### %s · %s\n%s", c.Created, c.Author, withNewline(c.Body))
			if d.commentsCut[i] {
				fmt.Fprintf(&b, "… comment truncated after %s characters.\n", humanCount(textLimit))
			}
		}
	}
	if d.CommentsOmitted > 0 {
		fmt.Fprintf(&b, "\nComments omitted (%d).\n", d.CommentsOmitted)
	} else if d.ReadAll != nil {
		b.WriteString("\n")
	}
	if d.ReadAll != nil {
		fmt.Fprintf(&b, "Read everything: %s\n", *d.ReadAll)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// refList names refs for a human: the ID, or the raw link when it does not
// resolve.
func refList(refs []refJSON) string {
	names := make([]string, len(refs))
	for i, r := range refs {
		if r.Resolved {
			names[i] = *r.ID
		} else {
			names[i] = *r.Link + " (unresolved)"
		}
	}
	return strings.Join(names, ", ")
}

func orDash(s *string) string {
	if s == nil {
		return "-"
	}
	return *s
}

// kindOrDash names an Item's Kind for a human, or "-" when it has none.
func kindOrDash(k *item.Kind) string {
	if k == nil {
		return "-"
	}
	return string(*k)
}

func withNewline(s string) string {
	if strings.HasSuffix(s, "\n") {
		return s
	}
	return s + "\n"
}

// humanCount writes n with thousands separators, such as 2,000.
func humanCount(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// commandRef is how a follow-up command should name f, one of its
// Project's Item files: its ID, or its path when the ID is ambiguous.
func commandRef(f vault.ItemFile, files []vault.ItemFile) string {
	if len(matchNumber(files, f.Number)) > 1 {
		return shellQuote(f.Path)
	}
	return f.ID()
}

func shellQuote(s string) string {
	if !strings.ContainsAny(s, " '\"\\$`!*?[]#&;|<>(){}~\t") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

var numberRef = regexp.MustCompile(`^[1-9][0-9]*$`)

// resolveRef finds the one Item ref names: a qualified ID, a bare number
// in the selected Project, a unique full filename (with its ".md") or an
// exact Vault-relative path. There is no title or fuzzy matching. A ref
// that names its Project overrides the implicit Project, but an explicit
// --project that disagrees fails.
func resolveRef(s resolved, v *vault.Vault, ref string) (vault.ItemFile, error) {
	var key string
	var details map[string]any // for project_not_found
	var match func([]vault.ItemFile) []vault.ItemFile
	idKey, idNumber, isID := item.ParseID(ref)
	switch {
	case isID:
		key = idKey
		match = func(fs []vault.ItemFile) []vault.ItemFile { return matchNumber(fs, idNumber) }
	case numberRef.MatchString(ref):
		sel, err := selectedProject(s)
		if err != nil {
			return vault.ItemFile{}, err
		}
		n, err := strconv.Atoi(ref)
		if err != nil {
			return vault.ItemFile{}, itemNotFound(ref, sel.Value)
		}
		key, details = sel.Value, map[string]any{"source": string(sel.Source)}
		match = func(fs []vault.ItemFile) []vault.ItemFile { return matchNumber(fs, n) }
	case strings.Contains(ref, "/"):
		f, ok, err := v.ItemAt(ref)
		if err != nil {
			return vault.ItemFile{}, ioError(err)
		}
		if !ok {
			return vault.ItemFile{}, itemNotFound(ref, "")
		}
		if err := checkRefProject(s, ref, f.Key); err != nil {
			return vault.ItemFile{}, err
		}
		return f, nil
	default:
		k, _, _, ok := item.ParseFilename(ref)
		if !ok {
			return vault.ItemFile{}, itemNotFound(ref, "")
		}
		key = k
		match = func(fs []vault.ItemFile) []vault.ItemFile { return matchName(fs, ref) }
	}
	if err := checkRefProject(s, ref, key); err != nil {
		return vault.ItemFile{}, err
	}
	if err := requireProject(v, key, details); err != nil {
		return vault.ItemFile{}, err
	}
	return uniqueItem(v, ref, key, match)
}

// checkRefProject fails when ref names Project key but --project names
// another. Implicit Project defaults never conflict.
func checkRefProject(s resolved, ref, key string) error {
	if s.Project.Source != config.FromFlag || s.Project.Value == key {
		return nil
	}
	return invalid("project_mismatch",
		ref+" is in Project "+key+", but --project is "+s.Project.Value,
		map[string]any{"ref": ref, "project": key, "flag": s.Project.Value},
		"drop --project, or pass --project "+key)
}

// uniqueItem is the one Item of Project key that match selects.
func uniqueItem(v *vault.Vault, ref, key string, match func([]vault.ItemFile) []vault.ItemFile) (vault.ItemFile, error) {
	files, err := v.ItemFiles(key)
	if err != nil {
		return vault.ItemFile{}, ioError(err)
	}
	found := match(files)
	switch len(found) {
	case 0:
		return vault.ItemFile{}, itemNotFound(ref, key)
	case 1:
		return found[0], nil
	}
	paths := make([]string, len(found))
	for i, f := range found {
		paths[i] = f.Path
	}
	return vault.ItemFile{}, &Error{Exit: ExitConflict, Code: "ambiguous_reference",
		Message: fmt.Sprintf("%s matches %d Items: %s", ref, len(found), strings.Join(paths, ", ")),
		Details: map[string]any{"ref": ref, "paths": paths},
		Hint:    "name one by its Vault-relative path, or run 'otman doctor --fix' to renumber the duplicate"}
}

func matchNumber(files []vault.ItemFile, n int) []vault.ItemFile {
	var out []vault.ItemFile
	for _, f := range files {
		if f.Number == n {
			out = append(out, f)
		}
	}
	return out
}

// matchName is the files whose full filename is name.
func matchName(files []vault.ItemFile, name string) []vault.ItemFile {
	var out []vault.ItemFile
	for _, f := range files {
		if f.Name() == name {
			out = append(out, f)
		}
	}
	return out
}

func itemNotFound(ref, key string) error {
	details := map[string]any{"ref": ref}
	msg := "no Item matches " + quoteArg(ref)
	if key != "" {
		details["project"] = key
		msg += " in Project " + key
	}
	return &Error{Exit: ExitNotFound, Code: "item_not_found", Message: msg, Details: details,
		Hint: "name an Item by its ID (OTM-12), its number, its full filename or its Vault-relative path; titles do not match"}
}
