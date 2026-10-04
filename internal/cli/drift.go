package cli

import (
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/talvor/otman/internal/item"
	"github.com/talvor/otman/internal/output"
	"github.com/talvor/otman/internal/vault"
)

// Drift (ADR 0005) is read leniently: an Item that drifted still reads,
// and each disagreement is warned about. Writes heal only lossless Drift
// (see item.Derived) and refuse with unsafe_write rather than guess.

// itemProblems are the warnings a read of Item file f, parsed as p,
// carries: its Drift, then each relation link that does not resolve.
func itemProblems(f vault.ItemFile, p item.Parsed, links itemLinks) []output.Problem {
	return append(driftProblems(f, p), relationProblems(f, p, links)...)
}

// driftProblems warn about each way Item file f, parsed as p, drifted.
// Frontmatter that cannot be read warns only that.
func driftProblems(f vault.ItemFile, p item.Parsed) []output.Problem {
	if p.FrontmatterErr != nil {
		return []output.Problem{malformedFrontmatter(f, p.FrontmatterErr)}
	}
	d := f.Derived()
	var ws []output.Problem
	if w, ok := idMismatch(f, p); ok {
		ws = append(ws, w)
	}
	var title, kind *string
	if d.Title != "" {
		title = &d.Title
	}
	if d.Kind != nil {
		k := string(*d.Kind)
		kind = &k
	}
	for _, m := range []struct {
		key     string
		absent  bool
		derived *string // nil when the file says nothing
		from    string
	}{
		{"id", p.ID == nil, &d.ID, "filename"},
		{"title", p.Title == nil, title, "filename"},
		{"kind", p.Kind == nil, kind, "folder"},
	} {
		if !m.absent || hasBadValue(p, m.key) {
			continue
		}
		msg := f.ID() + " has no " + m.key
		hint := "add " + m.key + " to " + f.Path
		if m.derived != nil {
			msg += ", so it reads as " + quoteArg(*m.derived) + " from its " + m.from
			hint = "the next otman write to " + f.ID() + " adds " + m.key + " from its " + m.from
		}
		ws = append(ws, output.Warning("missing_key", msg,
			map[string]any{"id": f.ID(), "path": f.Path, "key": m.key, "derived": m.derived}, hint))
	}
	if p.Title != nil && item.FilenameTitle(*p.Title) != d.Title {
		ws = append(ws, output.Warning("title_drift",
			f.ID()+" has title "+quoteArg(*p.Title)+", but its filename says "+quoteArg(d.Title),
			map[string]any{"id": f.ID(), "path": f.Path, "title": *p.Title, "filename_title": d.Title},
			"otman shows the frontmatter title; 'otman doctor --fix --prefer frontmatter|file' settles which is right"))
	}
	folder := path.Dir(f.Path)
	switch {
	case d.Kind == nil:
		details := map[string]any{"id": f.ID(), "path": f.Path, "folder": folder}
		hint := "move it into " + kindFolders(f.Key)
		if p.Kind != nil {
			details["expected"] = f.KindPath(*p.Kind)
			hint = "move it to " + f.KindPath(*p.Kind) + ", or run 'otman doctor --fix'"
		}
		ws = append(ws, output.Warning("unexpected_folder",
			f.ID()+" is in "+folder+", not in a Kind folder of Project "+f.Key, details, hint))
	case p.Kind != nil && *p.Kind != *d.Kind:
		ws = append(ws, output.Warning("kind_drift",
			f.ID()+" has kind "+string(*p.Kind)+", but it is in "+folder+", the folder for "+string(*d.Kind),
			map[string]any{"id": f.ID(), "path": f.Path, "kind": *p.Kind, "folder_kind": *d.Kind},
			"otman shows the frontmatter Kind; 'otman doctor --fix --prefer frontmatter|file' settles which is right"))
	}
	if p.Status == nil || *p.Status != item.Open && *p.Status != item.Closed {
		ws = append(ws, output.Warning("invalid_status", statusMessage(f.ID(), p.Status),
			map[string]any{"id": f.ID(), "path": f.Path, "status": p.Status},
			"run 'otman reopen "+f.ID()+"' or 'otman close "+f.ID()+"' to set it"))
	}
	for _, bad := range p.BadValues {
		if bad.Key != "id" {
			ws = append(ws, badValue(f, bad))
		}
	}
	ws = append(ws, invalidLabels(f, p)...)
	details := map[string]any{"id": f.ID(), "path": f.Path}
	switch {
	case p.Markers == 0:
		ws = append(ws, output.Warning("missing_comments_marker",
			f.ID()+" has no "+item.CommentsMarker+" line, so its comments are read from the last "+item.CommentsHeading+" heading",
			details, "'otman comment "+f.ID()+"' restores the marker; until then body edits are refused"))
	case p.Markers > 1:
		ws = append(ws, output.Warning("duplicate_comments_markers",
			f.ID()+" has "+strconv.Itoa(p.Markers)+" "+item.CommentsMarker+" lines, so its comments are read from the first",
			details, "keep only the marker just before the comments' "+item.CommentsHeading+" heading; until then body edits and comments are refused"))
	}
	return ws
}

// invalidLabels warn about each Label of Item file f, parsed as p, that
// is not a valid Label even lowercased. Reads show it as written, and
// writes keep it.
func invalidLabels(f vault.ItemFile, p item.Parsed) []output.Problem {
	var ws []output.Problem
	seen := labelSet{}
	for _, l := range p.Labels {
		if _, ok := item.ParseLabel(l); ok || seen[strings.ToLower(l)] {
			continue
		}
		seen.add([]string{l})
		ws = append(ws, output.Warning("invalid_label",
			f.ID()+" has the label "+quoteArg(l)+", which is not a valid Label even lowercased",
			map[string]any{"id": f.ID(), "path": f.Path, "label": l, "pattern": item.LabelPattern, "max_length": item.LabelLimit},
			"otman keeps it; rename it in "+f.Path+" to lowercase letters, digits and . _ : / -, starting with a letter or digit, at most 64 characters"))
	}
	return ws
}

// idMismatch warns when the frontmatter id of Item file f, parsed as p,
// is present but is not its filename prefix.
func idMismatch(f vault.ItemFile, p item.Parsed) (output.Problem, bool) {
	var written string
	switch {
	case p.ID != nil && *p.ID != f.ID():
		written = quoteArg(*p.ID)
	case hasBadValue(p, "id"):
		written = "that is a list or mapping"
	default:
		return output.Problem{}, false
	}
	return output.Warning("id_mismatch",
		f.ID()+" has frontmatter id "+written+"; its identity is its filename prefix "+f.ID(),
		map[string]any{"id": f.ID(), "path": f.Path, "frontmatter_id": p.ID},
		"the next otman write to "+f.ID()+" resets id to "+f.ID()), true
}

// kindFolders names the Kind folders of Project key.
func kindFolders(key string) string {
	names := make([]string, len(item.Kinds))
	for i, k := range item.Kinds {
		names[i] = path.Join(vault.ProjectsDir, key, k.Folder()) + "/"
	}
	return strings.Join(names, ", ")
}

// badWant is what each owned key's value should be.
var badWant = map[string]string{
	"title": "text", "kind": "issue, prd or spec", "author": "a name", "assignee": "a name",
	"created": "a timestamp", "updated": "a timestamp", "labels": "a list of Labels",
}

// badValue warns that bad, an owned value of Item file f, has the wrong
// shape, so it reads as absent.
func badValue(f vault.ItemFile, bad item.BadValue) output.Problem {
	what := "a list or mapping"
	if bad.Value != nil {
		what = quoteArg(*bad.Value)
	}
	return output.Warning("malformed_value",
		"the "+bad.Key+" value of "+f.ID()+" is "+what+", not "+badWant[bad.Key]+", so it reads as absent",
		map[string]any{"id": f.ID(), "path": f.Path, "key": bad.Key, "value": bad.Value},
		"fix "+bad.Key+" in "+f.Path+"; otman keeps it as written until a command sets "+bad.Key)
}

// malformedFrontmatter warns that the Item file f was left out because
// its frontmatter cannot be read.
func malformedFrontmatter(f vault.ItemFile, err error) output.Problem {
	return output.Warning("malformed_frontmatter",
		"cannot read "+f.Path+": "+err.Error(),
		map[string]any{"path": f.Path},
		"fix the YAML between the --- lines in "+f.Path)
}

// hasBadValue reports whether the value of key, parsed as p, has the
// wrong shape.
func hasBadValue(p item.Parsed, key string) bool {
	return slices.ContainsFunc(p.BadValues, func(b item.BadValue) bool { return b.Key == key })
}
